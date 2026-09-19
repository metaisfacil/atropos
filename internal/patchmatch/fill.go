// Package patchmatch provides context-aware image inpainting using PatchMatch.
package patchmatch

import (
	"context"
	"encoding/binary"
	"errors"
	"image"
	"image/draw"
	"math"
	"runtime"
	"sync"
)

// Fill replaces pixels covered by mask with patches sampled from elsewhere in
// src. Performance-sensitive callers should prefer FillROI or FillBounds with a known
// dirty rectangle.
func Fill(ctx context.Context, src *image.NRGBA, mask *image.Alpha, patchSize, iterations int) (*image.NRGBA, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if src == nil {
		return nil, errors.New("PatchMatch: nil source image")
	}
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	if w == 0 || h == 0 || mask == nil {
		return normalizeNRGBA(src), nil
	}
	localSource := normalizeNRGBA(src)
	localMask := normalizeAlpha(mask, w, h)
	if maskBounds(localMask).Empty() {
		return localSource, nil
	}
	return patchMatchFillLocal(ctx, localSource, localMask, patchSize, iterations)
}

// FillBounds is the full-image result API with an optional
// dirty rectangle. dirtyBounds is expressed relative to src.Bounds().Min and
// must contain every non-zero mask pixel. Passing an empty rectangle scans mask
// to discover it.
func FillBounds(ctx context.Context, src *image.NRGBA, mask *image.Alpha, dirtyBounds image.Rectangle, patchSize, iterations int) (*image.NRGBA, error) {
	local, workBounds, err := FillROI(ctx, src, mask, dirtyBounds, patchSize, iterations)
	if err != nil {
		return nil, err
	}
	if src == nil {
		return nil, errors.New("PatchMatch: nil source image")
	}
	out := normalizeNRGBA(src)
	if local != nil && !workBounds.Empty() {
		draw.Draw(out, workBounds, local, image.Point{}, draw.Src)
	}
	return out, nil
}

// FillROI is the lowest-latency integration API. It returns only the
// local working image and the rectangle where it belongs in the source image.
// An editor that already owns a mutable/tiled document buffer can composite
// this ROI itself and avoid the final full-document copy performed by
// Fill/FillBounds.
//
// dirtyBounds follows the same convention as FillBounds. If there is
// nothing to fill, local is nil and workBounds is empty.
func FillROI(ctx context.Context, src *image.NRGBA, mask *image.Alpha, dirtyBounds image.Rectangle, patchSize, iterations int) (local *image.NRGBA, workBounds image.Rectangle, err error) {
	if err := ctx.Err(); err != nil {
		return nil, image.Rectangle{}, err
	}
	if src == nil {
		return nil, image.Rectangle{}, errors.New("PatchMatch: nil source image")
	}
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	if w == 0 || h == 0 || mask == nil {
		return nil, image.Rectangle{}, nil
	}
	patchSize = normalizePatchSize(patchSize, w, h)
	if iterations < 1 {
		iterations = 1
	}

	imageBounds := image.Rect(0, 0, w, h)
	maskBoundsFull := dirtyBounds.Intersect(imageBounds)
	if maskBoundsFull.Empty() {
		maskBoundsFull = maskBoundsInImage(mask, w, h)
	}
	if maskBoundsFull.Empty() {
		return nil, image.Rectangle{}, nil
	}

	workBounds = pmWorkingROI(maskBoundsFull, imageBounds, patchSize)
	localSource := cropNRGBA(src, workBounds)
	localMask := cropAlpha(mask, workBounds, w, h)
	if maskBounds(localMask).Empty() {
		return nil, image.Rectangle{}, nil
	}
	local, err = patchMatchFillLocal(ctx, localSource, localMask, patchSize, iterations)
	if err != nil {
		return nil, image.Rectangle{}, err
	}
	return local, workBounds, nil
}

const (
	synthesisPatchSize = 7
	synthesisPatchHalf = synthesisPatchSize / 2
	synthesisMinSide   = 35

	// Fine-level random-search radius of the first propagation dispatch and
	// the fixed radius of the incumbent/restart merge dispatch.
	synthesisFineRadius      = 3
	synthesisSecondaryRadius = 1
)

// synthesisPlaneSigma is the Gaussian of every step of the plane chain
// (probed: the synthesis's target planes are a smooth chain, sigma about 0.55
// per 0.7 step).
const synthesisPlaneSigma = 0.55

type pmPoint struct {
	x int32
	y int32
}

type synthesisLevel struct {
	src           *image.NRGBA // plain box-reduced source sampled by costs and votes
	plane         *image.NRGBA // opaque form of src used by the patch-cost kernel
	seed          *image.NRGBA // masked, normalised target image that begins the E/M loop
	mask          *image.Alpha // point-sampled hole: pixels replaced by the vote
	targetMask    *image.Alpha // conservative coverage: target/source classification
	w, h          int
	painted       image.Rectangle
	targetPainted image.Rectangle
	active        image.Rectangle
	fieldStride   int
	fieldOffset   int
	searchWindow  int   // scalar fallback proposal bound; zero means the full source region
	window        []int // per-pixel proposal bound (the engine's window plane)
	target        []bool
	valid         []bool
	sources       []pmPoint
	nnf           []pmPoint
	cost          []uint32
	restartNNF    []pmPoint
	restartCost   []uint32
	coherence     []float32
}

type synthesisSolution struct {
	level   *synthesisLevel
	working *image.NRGBA
	nnf     []pmPoint
}

// synthesisChainPlane is a colour plane with a validity weight per pixel.
type synthesisChainPlane struct {
	w, h   int
	values []float32 // RGB, premultiplied by the weight
	weight []float32
}

// synthesisRNG reproduces the counter-mode generator used by the synthesis
// translation solver. Its AES round-key table is intentionally not a normal
// expanded AES key: the native code reads the fixed table one byte off its
// conventional alignment. Keeping the actual round keys is therefore both
// simpler and exact.
type synthesisRNG struct {
	counter [4]uint32
	words   [4]uint32
	used    int
}

// synthesisDebugLevel, when set, receives every solved level (tests only).
var synthesisDebugLevel func(levelIndex int, level *synthesisLevel, working *image.NRGBA)

// synthesisDebugPeel, when set, receives the pre-healed peel image (tests only).
var synthesisDebugPeel func(img *image.NRGBA, mask *image.Alpha)

// synthesisDebugRound, when set, is called after every round (tests only).
var synthesisDebugRound func(levelIndex, round int, phase string, level *synthesisLevel, working *image.NRGBA)

func patchMatchFillLocal(ctx context.Context, source *image.NRGBA, targetMask *image.Alpha, _, _ int) (*image.NRGBA, error) {
	// Holes that touch the image border are pre-healed ring by ring at a
	// coarse power-of-two scale before the ordinary pyramid runs; that scale
	// also becomes the coarsest pyramid level.
	w, h := source.Bounds().Dx(), source.Bounds().Dy()
	peel := maskTouchesBorder(targetMask)
	maxDist := synthesisMaxL1Distance(targetMask)
	minCoarse := synthesisMinSide
	var peelScale float32
	if peel {
		// The peel scale sets the coarsest pyramid level (min side of the peel
		// image, but never more than half the image).
		peelScale = synthesisPeelScale(w, h, maxDist)
		peelW, peelH := int(float32(w)*peelScale+0.5), int(float32(h)*peelScale+0.5)
		minCoarse = minInt(minInt(peelW, peelH), minInt(w, h)/2)
	}
	levels := synthesisPyramid(source, targetMask, minCoarse)
	active := make([]int, 0, len(levels))
	for i, level := range levels {
		if len(level.sources) != 0 && synthesisLevelIsUsable(level, i == len(levels)-1) {
			active = append(active, i)
		}
	}
	if len(active) == 0 {
		return cloneNRGBA(source), nil
	}

	var parent *synthesisSolution
	if peel {
		peelSrc, peelMask := synthesisBlackHole(source, targetMask), targetMask
		peelW, peelH := int(float32(w)*peelScale+0.5), int(float32(h)*peelScale+0.5)
		if peelW != w || peelH != h {
			if levels[0].w == peelW && levels[0].h == peelH && levels[0].src != nil && levels[0].targetMask != nil {
				peelSrc, peelMask = levels[0].src, levels[0].targetMask
			} else {
				peelSrc = synthesisMaskedResize(synthesisBlackHole(source, targetMask), nil, peelW, peelH)
				peelMask = synthesisResizeAlpha(targetMask, peelW, peelH)
			}
		}
		depth := int(float32(maxDist)*peelScale + 0.5)
		healed, field, err := synthesisPeel(ctx, peelSrc, peelMask, depth)
		if err != nil {
			return nil, err
		}
		if synthesisDebugPeel != nil {
			synthesisDebugPeel(healed, peelMask)
		}
		// The pre-healed image seeds the first active level's target; its
		// field starts from random sources like any first level (probed: the
		// first main dispatch of a top band starts from a uniformly random
		// field even though the peel solved the same rows).
		_ = field
		parent = &synthesisSolution{level: &synthesisLevel{w: peelW, h: peelH}, working: healed}
	}

	// The initializer's half-window is derived once from the full-resolution
	// hole geometry. It is not a generic global draw: live traces show the
	// same value at the first active pyramid level (for example 17 at scale
	// 0.7 for a 10 px dab).
	initWindow := synthesisSearchWindow(targetMask, maxDist, peel)
	for _, levelIndex := range active {
		level := levels[levelIndex]
		level.window = synthesisWindowPlane(level, initWindow, w)
	}

	finest := active[len(active)-1]
	ordinaryIndex := 0
	ordinaryCount := len(active) - 1
	for _, levelIndex := range active {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		level := levels[levelIndex]
		primaryRadius := synthesisFineRadius
		if ordinaryIndex < maxInt(2, len(active)-3) {
			primaryRadius = maxInt(level.w, level.h)
		}
		working := synthesisSeedWorking(level, parent)
		inherited := parent != nil && parent.nnf != nil
		if inherited {
			synthesisUpscaleNNF(level, parent, levelIndex)
			// The level's first target is the vote of the inherited field on
			// this level's plane rather than the blurry upsampled image, so
			// the inherited entries start as exact copies and only a fresh
			// entry that is really cheaper can displace them (probed: the
			// synthesis's first round leaves the inherited block structure
			// unchanged).
			synthesisRefreshCosts(level, working)
			voted, err := synthesisVote(ctx, level, working)
			if err != nil {
				return nil, err
			}
			working = voted
		}

		if levelIndex == finest && inherited {
			// The finest level runs a single round without proposals: one
			// propagation dispatch on the inherited field, then a vote.
			synthesisRefreshCosts(level, working)
			if synthesisDebugRound != nil {
				synthesisDebugRound(levelIndex, 0, "proposed", level, working)
			}
			synthesisSearchDispatch(level, working, 0, levelIndex, 0, 0, nil, nil)
			if synthesisDebugRound != nil {
				synthesisDebugRound(levelIndex, 0, "searched", level, working)
			}
			var err error
			working, err = synthesisVote(ctx, level, working)
			if err != nil {
				return nil, err
			}
			parent = &synthesisSolution{level: level, working: working, nnf: append([]pmPoint(nil), level.nnf...)}
			if synthesisDebugLevel != nil {
				synthesisDebugLevel(levelIndex, level, working)
			}
			continue
		}

		rounds := 25
		if ordinaryIndex == 0 || ordinaryIndex == ordinaryCount-1 || ordinaryCount <= 1 {
			rounds = 30
		}
		// Probed schedule: the primary dispatch spans the level on the early
		// active levels and uses radius 3 on the last three; the merge dispatch
		// always uses radius 1.
		// The last ordinary levels freeze the interiors of coherent blocks
		// of the incumbent field (probed: a three-state map appears on the
		// last three active levels only).
		freezing := ordinaryIndex >= ordinaryCount-2
		for round := 0; round < rounds; round++ {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			// The field persists across rounds. Every round after the first
			// solves a fresh restart field - uniform random sources refined
			// by the primary dispatch - and the second dispatch merges it into
			// the incumbent pixel by pixel (probed on the synthesis engine: the
			// entry buffer is re-randomised, searched, and the prior field
			// returns in the first pass of the second dispatch with only the
			// cheaper restart entries kept). Only the very first round of a
			// level without an inherited field starts from scratch; an
			// inherited field is never random-searched with the primary
			// radius itself (probed: a level's first round takes the fresh
			// field from 38 offsets per 7x7 footprint to the inherited 6 and
			// the rounds keep it there, whereas searching the inherited field
			// directly against the blurry upsampled target fragments it).
			var restart []pmPoint
			var frozen []bool
			if round == 0 && !inherited {
				synthesisRandomInitialize(level, working, levelIndex, round, initWindow, false, false)
			} else {
				synthesisRefreshCosts(level, working)
				if freezing && round != 0 {
					frozen = synthesisFrozen(level)
				}
				restart = synthesisRestartField(level, working, levelIndex, round, initWindow, primaryRadius, frozen)
			}
			if synthesisDebugRound != nil {
				synthesisDebugRound(levelIndex, round, "proposed", level, working)
			}
			if restart == nil {
				synthesisSearchDispatch(level, working, primaryRadius, levelIndex, round, 0, nil, frozen)
			}
			if ordinaryIndex != 0 || round != 0 {
				synthesisSearchDispatch(level, working, synthesisSecondaryRadius, levelIndex, round, 1, restart, frozen)
			}
			if synthesisDebugRound != nil {
				synthesisDebugRound(levelIndex, round, "searched", level, working)
			}
			var err error
			working, err = synthesisVote(ctx, level, working)
			if err != nil {
				return nil, err
			}
		}
		parent = &synthesisSolution{level: level, working: working, nnf: append([]pmPoint(nil), level.nnf...)}
		// Restart buffers are level-local workspace. Release them once this
		// level has been collapsed into the parent solution so coarse levels do
		// not retain a second full-resolution field for the rest of the fill.
		level.restartNNF = nil
		level.restartCost = nil
		ordinaryIndex++
		if synthesisDebugLevel != nil {
			synthesisDebugLevel(levelIndex, level, working)
		}
	}

	if parent == nil {
		return cloneNRGBA(source), nil
	}
	return parent.working, nil
}

// synthesisPyramid builds the coarse-to-fine levels: scales step by 0.7 from
// full resolution while the shorter side stays at least minCoarse, and a
// final level clamped to exactly minCoarse is added when the next step would
// fall below it.
func synthesisPyramid(source *image.NRGBA, targetMask *image.Alpha, minCoarse int) []*synthesisLevel {
	w, h := source.Bounds().Dx(), source.Bounds().Dy()
	minSide := minInt(w, h)
	scales := []float32{1}
	for minSide > minCoarse {
		last := scales[len(scales)-1]
		next := last * 0.7
		if int(float32(minSide)*next+0.5) < minCoarse {
			next = float32(minCoarse) / float32(minSide)
		}
		lastW, lastH := int(float32(w)*last+0.5), int(float32(h)*last+0.5)
		nextW, nextH := int(float32(w)*next+0.5), int(float32(h)*next+0.5)
		if nextW == lastW && nextH == lastH {
			break
		}
		scales = append(scales, next)
		if next == float32(minCoarse)/float32(minSide) {
			break
		}
	}

	// The source and target pyramids are distinct. Candidate patches and
	// reconstruction votes use a plain box reduction of the black-hole source.
	// The evolving target starts from a masked, normalised Gaussian chain that
	// extrapolates known colour a little way into the hole. Fine to coarse,
	// then reversed.
	blackSource := synthesisBlackHole(source, targetMask)
	chain := newSynthesisChainPlane(source, targetMask)
	fine := make([]*synthesisLevel, 0, len(scales))
	for i, scale := range scales {
		lw := maxInt(synthesisPatchSize, int(float32(w)*scale+0.5))
		lh := maxInt(synthesisPatchSize, int(float32(h)*scale+0.5))
		if i == 0 {
			level := newSynthesisLevel(source, targetMask, nil)
			level.plane = blackSource
			level.seed = blackSource
			fine = append(fine, level)
			continue
		}
		chain = chain.reduce(lw, lh, synthesisPlaneSigma)
		pointMask := synthesisPointMask(targetMask, lw, lh)
		pointPainted := maskBounds(pointMask)
		if pointPainted.Dx() < synthesisPatchSize || pointPainted.Dy() < synthesisPatchSize {
			// The schedule skips levels whose downscaled hole cannot contain a
			// complete patch. Keep only their geometry: the Gaussian chain must
			// still advance through this scale, but source planes, classification
			// maps and NNF storage are never observed.
			fine = append(fine, &synthesisLevel{
				mask: pointMask, w: lw, h: lh, painted: pointPainted, targetPainted: pointPainted,
			})
			continue
		}
		sourcePlane := synthesisMaskedResize(blackSource, nil, lw, lh)
		coverageMask := synthesisResizeAlpha(targetMask, lw, lh)
		targetLevelMask := synthesisAreaMask(targetMask, lw, lh)
		level := newSynthesisLevelWithPainted(sourcePlane, targetLevelMask, coverageMask, pointPainted)
		// The native target view uses the point-sampled mask's bounds but a
		// filtered mask for per-centre activity. Source patches are rejected
		// against every coarse pixel touched by the original hole.
		level.mask = pointMask
		level.painted = pointPainted
		level.targetMask = coverageMask
		level.seed = chain.image()
		fine = append(fine, level)
	}
	levels := make([]*synthesisLevel, 0, len(fine))
	for i := len(fine) - 1; i >= 0; i-- {
		levels = append(levels, fine[i])
	}
	return levels
}

func newSynthesisChainPlane(src *image.NRGBA, mask *image.Alpha) *synthesisChainPlane {
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	p := &synthesisChainPlane{w: w, h: h, values: make([]float32, w*h*3), weight: make([]float32, w*h)}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if mask.Pix[y*mask.Stride+x] != 0 {
				continue
			}
			i := y*w + x
			p.weight[i] = 1
			for c := 0; c < 3; c++ {
				p.values[i*3+c] = float32(src.Pix[y*src.Stride+x*4+c])
			}
		}
	}
	return p
}

// reduce area-averages the plane to w x h (weights and premultiplied values
// alike) and then blurs both with a Gaussian, so the normalised result is the
// known content's average where known pixels reach and black elsewhere.
func (p *synthesisChainPlane) reduce(w, h int, sigma float64) *synthesisChainPlane {
	box := &synthesisChainPlane{w: w, h: h, values: make([]float32, w*h*3), weight: make([]float32, w*h)}
	sx, sy := float64(w)/float64(p.w), float64(h)/float64(p.h)
	// Gather each output cell from its overlapping source cells. This visits a
	// cell's contributors in the same y/x order as the former scatter loop, so
	// its floating-point sums are bit-for-bit identical, but independent output
	// rows can run concurrently without atomics.
	_ = parallelRowsSized(context.Background(), 0, h, p.w, func(oy int) {
		sourceY0 := maxInt(0, int(float64(oy)/sy))
		sourceY1 := minInt(p.h, int(math.Ceil(float64(oy+1)/sy)))
		for ox := 0; ox < w; ox++ {
			sourceX0 := maxInt(0, int(float64(ox)/sx))
			sourceX1 := minInt(p.w, int(math.Ceil(float64(ox+1)/sx)))
			oi := oy*w + ox
			for y := sourceY0; y < sourceY1; y++ {
				y0, y1 := float64(y)*sy, float64(y+1)*sy
				cy := minFloat64(y1, float64(oy+1)) - maxFloat64(y0, float64(oy))
				if cy <= 0 {
					continue
				}
				for x := sourceX0; x < sourceX1; x++ {
					i := y*p.w + x
					if p.weight[i] == 0 {
						continue
					}
					x0, x1 := float64(x)*sx, float64(x+1)*sx
					cx := minFloat64(x1, float64(ox+1)) - maxFloat64(x0, float64(ox))
					if cx <= 0 {
						continue
					}
					wgt := float32(cx * cy)
					for c := 0; c < 3; c++ {
						box.values[oi*3+c] += wgt * p.values[i*3+c]
					}
					box.weight[oi] += wgt * p.weight[i]
				}
			}
		}
	})
	radius := int(math.Ceil(sigma * 2))
	kernel := make([]float32, 2*radius+1)
	for i := range kernel {
		d := float64(i - radius)
		kernel[i] = float32(math.Exp(-d * d / (2 * sigma * sigma)))
	}
	out := &synthesisChainPlane{w: w, h: h, values: make([]float32, w*h*3), weight: make([]float32, w*h)}
	tmp := &synthesisChainPlane{w: w, h: h, values: make([]float32, w*h*3), weight: make([]float32, w*h)}
	_ = parallelRowsSized(context.Background(), 0, h, w*(2*radius+1), func(y int) {
		for x := 0; x < w; x++ {
			oi := y*w + x
			for k := -radius; k <= radius; k++ {
				nx := x + k
				if nx < 0 || nx >= w {
					continue
				}
				ni := y*w + nx
				g := kernel[k+radius]
				tmp.weight[oi] += g * box.weight[ni]
				for c := 0; c < 3; c++ {
					tmp.values[oi*3+c] += g * box.values[ni*3+c]
				}
			}
		}
	})
	_ = parallelRowsSized(context.Background(), 0, h, w*(2*radius+1), func(y int) {
		for x := 0; x < w; x++ {
			oi := y*w + x
			for k := -radius; k <= radius; k++ {
				ny := y + k
				if ny < 0 || ny >= h {
					continue
				}
				ni := ny*w + x
				g := kernel[k+radius]
				out.weight[oi] += g * tmp.weight[ni]
				for c := 0; c < 3; c++ {
					out.values[oi*3+c] += g * tmp.values[ni*3+c]
				}
			}
		}
	})
	return out
}

func (p *synthesisChainPlane) image() *image.NRGBA {
	out := image.NewNRGBA(image.Rect(0, 0, p.w, p.h))
	for i := 0; i < p.w*p.h; i++ {
		di := i * 4
		if p.weight[i] > 1e-6 {
			for c := 0; c < 3; c++ {
				out.Pix[di+c] = byte(clampInt(int(p.values[i*3+c]/p.weight[i]+0.5), 0, 255))
			}
		}
		out.Pix[di+3] = 255
	}
	return out
}

// newSynthesisLevel prepares the target/source classification of one level.
// Centres whose 7x7 patch overlaps mask are targets; centres whose patch
// overlaps neither mask nor exclude are valid sources. exclude may be nil.
func newSynthesisLevel(src *image.NRGBA, mask *image.Alpha, exclude *image.Alpha) *synthesisLevel {
	return newSynthesisLevelWithPainted(src, mask, exclude, maskBounds(mask))
}

func newSynthesisLevelWithPainted(src *image.NRGBA, mask *image.Alpha, exclude *image.Alpha, targetPainted image.Rectangle) *synthesisLevel {
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	level := &synthesisLevel{src: src, plane: src, seed: src, mask: mask, targetMask: mask, w: w, h: h}
	if !src.Opaque() {
		// The patch cost reads RGB only; an opaque copy lets the SIMD
		// kernel score transparent (outpaint) sources too.
		level.plane = cloneNRGBA(src)
		for i := 3; i < len(level.plane.Pix); i += 4 {
			level.plane.Pix[i] = 255
		}
	}
	level.painted = maskBounds(mask)
	level.targetPainted = targetPainted
	if level.targetPainted.Empty() || w < synthesisPatchSize || h < synthesisPatchSize {
		return level
	}
	level.active = image.Rect(
		maxInt(synthesisPatchHalf, level.targetPainted.Min.X-synthesisPatchHalf),
		maxInt(synthesisPatchHalf, level.targetPainted.Min.Y-synthesisPatchHalf),
		minInt(w-synthesisPatchHalf, level.targetPainted.Max.X+synthesisPatchHalf),
		minInt(h-synthesisPatchHalf, level.targetPainted.Max.Y+synthesisPatchHalf),
	)
	level.fieldStride = level.active.Dx()
	level.fieldOffset = -level.active.Min.Y*level.fieldStride - level.active.Min.X

	maskIntegral := maskedIntegral(mask)
	var excludeIntegral []int
	if exclude != nil {
		excludeIntegral = maskedIntegral(exclude)
	}
	level.valid = make([]bool, w*h)
	fieldSize := level.active.Dx() * level.active.Dy()
	level.target = make([]bool, fieldSize)
	level.nnf = make([]pmPoint, fieldSize)
	level.cost = make([]uint32, fieldSize)
	for i := range level.nnf {
		level.nnf[i] = pmPoint{x: -1, y: -1}
		level.cost[i] = math.MaxUint32
	}
	for y := synthesisPatchHalf; y < h-synthesisPatchHalf; y++ {
		for x := synthesisPatchHalf; x < w-synthesisPatchHalf; x++ {
			x0, y0 := x-synthesisPatchHalf, y-synthesisPatchHalf
			x1, y1 := x+synthesisPatchHalf+1, y+synthesisPatchHalf+1
			covered := integralRectSum(maskIntegral, w+1, x0, y0, x1, y1) != 0
			excluded := excludeIntegral != nil && integralRectSum(excludeIntegral, w+1, x0, y0, x1, y1) != 0
			switch {
			case covered:
				if synthesisFieldContains(level, x, y) {
					level.target[synthesisFieldID(level, x, y)] = true
				}
			case !covered && !excluded:
				level.valid[y*w+x] = true
				level.sources = append(level.sources, pmPoint{x: int32(x), y: int32(y)})
			}
		}
	}
	return level
}

func synthesisFieldContains(level *synthesisLevel, x, y int) bool {
	return x >= level.active.Min.X && x < level.active.Max.X && y >= level.active.Min.Y && y < level.active.Max.Y
}

func synthesisFieldID(level *synthesisLevel, x, y int) int {
	return y*level.fieldStride + x + level.fieldOffset
}

func synthesisTargetAt(level *synthesisLevel, x, y int) bool {
	return synthesisFieldContains(level, x, y) && level.target[synthesisFieldID(level, x, y)]
}

func synthesisLevelIsUsable(level *synthesisLevel, finest bool) bool {
	if level.targetPainted.Empty() || level.active.Empty() || len(level.sources) == 0 {
		return false
	}
	if finest {
		return true
	}
	return level.targetPainted.Dx() >= synthesisPatchSize && level.targetPainted.Dy() >= synthesisPatchSize
}

func synthesisSeedWorking(level *synthesisLevel, parent *synthesisSolution) *image.NRGBA {
	if parent == nil {
		return cloneNRGBA(level.seed)
	}
	out := cloneNRGBA(level.seed)
	for y := level.painted.Min.Y; y < level.painted.Max.Y; y++ {
		for x := level.painted.Min.X; x < level.painted.Max.X; x++ {
			if level.mask.Pix[y*level.mask.Stride+x] == 0 {
				continue
			}
			sample := pmBilinearParent(parent.working, level.w, level.h, x, y)
			i := y*out.Stride + x*4
			for c := 0; c < 4; c++ {
				out.Pix[i+c] = sample[c]
			}
		}
	}
	return out
}

// synthesisWindowAt returns the proposal bound for a target centre. The engine
// keeps a per-pixel window plane (EM state+0x528); the scalar is only the
// fallback the initializer uses when that plane is absent.
func synthesisWindowAt(level *synthesisLevel, x, y int) int {
	if level.window != nil {
		return level.window[synthesisFieldID(level, x, y)]
	}
	return level.searchWindow
}

// synthesisWindowPlane reproduces the engine's window plane: the
// full-resolution half-window scaled into this level, plus each pixel's
// Manhattan distance into the hole, so centres deep inside the hole search
// farther than the boundary. Verified against native planes: C = int(hswHalf *
// scale) matched every probed level exactly, and the ramp is
// PM2_ManhattanDistanceTransform of the level's hole.
func synthesisWindowPlane(level *synthesisLevel, hswHalf, fullWidth int) []int {
	if hswHalf <= 0 || fullWidth <= 0 {
		return nil
	}
	c := int(float64(hswHalf) * float64(level.w) / float64(fullWidth))
	if c < 1 {
		c = 1
	}
	dist := synthesisHoleDistance(level.targetMask, level.w, level.h)
	out := make([]int, level.active.Dx()*level.active.Dy())
	for y := level.active.Min.Y; y < level.active.Max.Y; y++ {
		for x := level.active.Min.X; x < level.active.Max.X; x++ {
			out[synthesisFieldID(level, x, y)] = c + dist[y*level.w+x]
		}
	}
	return out
}

// synthesisHoleDistance is the Manhattan distance of every hole pixel to the
// nearest pixel outside the hole (zero outside), by the usual two-pass chamfer.
func synthesisHoleDistance(mask *image.Alpha, w, h int) []int {
	const far = 1 << 20
	d := make([]int, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if mask.Pix[y*mask.Stride+x] != 0 {
				d[y*w+x] = far
			}
		}
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := y*w + x
			if d[i] == 0 {
				continue
			}
			if x > 0 && d[i-1]+1 < d[i] {
				d[i] = d[i-1] + 1
			}
			if y > 0 && d[i-w]+1 < d[i] {
				d[i] = d[i-w] + 1
			}
		}
	}
	for y := h - 1; y >= 0; y-- {
		for x := w - 1; x >= 0; x-- {
			i := y*w + x
			if d[i] == 0 {
				continue
			}
			if x < w-1 && d[i+1]+1 < d[i] {
				d[i] = d[i+1] + 1
			}
			if y < h-1 && d[i+w]+1 < d[i] {
				d[i] = d[i+w] + 1
			}
		}
	}
	return d
}

// synthesisRandomInitialize draws one random source per target centre. With
// propose set, the candidate replaces the incumbent only when its cost is
// lower; otherwise the field is (re)initialised. Candidates are drawn within
// the hole search window around the current match when the field was
// inherited from a coarser level, else around the target pixel.
func synthesisRandomInitialize(level *synthesisLevel, working *image.NRGBA, levelIndex, round, window int, aroundMatch, propose bool) {
	_ = parallelRowsSized(context.Background(), level.active.Min.Y, level.active.Max.Y, level.active.Dx()*synthesisPatchSize, func(y int) {
		rng := newSynthesisRNG(level.active.Min.X-synthesisPatchHalf, y-synthesisPatchHalf, 0, synthesisPatchHalf)
		for x := level.active.Min.X; x < level.active.Max.X; x++ {
			id := synthesisFieldID(level, x, y)
			if !level.target[id] {
				continue
			}
			cx, cy := x, y
			if aroundMatch && synthesisValid(level, level.nnf[id]) {
				cx, cy = int(level.nnf[id].x), int(level.nnf[id].y)
			}
			candidate, ok := synthesisRandomSource(level, &rng, cx, cy, synthesisWindowAt(level, x, y))
			if !ok {
				continue
			}
			if propose {
				if candidate == level.nnf[id] {
					continue
				}
				cost := synthesisPatchCost(level, working, x, y, candidate, level.cost[id])
				if cost < level.cost[id] {
					level.nnf[id], level.cost[id] = candidate, cost
				}
				continue
			}
			level.nnf[id] = candidate
			level.cost[id] = synthesisPatchCost(level, working, x, y, candidate, math.MaxUint32)
		}
	})
}

// synthesisRandomSource draws a valid source centre inside the window around
// (cx, cy), falling back to any valid source after a few misses.
func synthesisRandomSource(level *synthesisLevel, rng *synthesisRNG, cx, cy, window int) (pmPoint, bool) {
	rng.beginCandidate()
	// The native field and its sampling window are expressed in patch top-left
	// coordinates. The Go solver stores patch centres, so translate both the
	// incumbent and every generated candidate at this boundary.
	cx -= synthesisPatchHalf
	cy -= synthesisPatchHalf
	minX, maxX := 0, level.w-synthesisPatchSize+1
	minY, maxY := 0, level.h-synthesisPatchSize+1
	if window > 0 {
		minX, maxX = maxInt(minX, cx-window), minInt(maxX, cx+window)
		minY, maxY = maxInt(minY, cy-window), minInt(maxY, cy+window)
	}
	if maxX > minX && maxY > minY {
		for attempt := 0; attempt < 32; attempt++ {
			word := rng.next()
			lo, hi := uint32(uint16(word)), uint32(uint16(word>>16))
			x := minX + int((lo+hi)%uint32(maxX-minX))
			y := minY + int((lo-hi)%uint32(maxY-minY))
			p := pmPoint{x: int32(x + synthesisPatchHalf), y: int32(y + synthesisPatchHalf)}
			if synthesisValid(level, p) {
				return p, true
			}
		}
	}
	if len(level.sources) == 0 {
		return pmPoint{}, false
	}
	return synthesisDrawPoint(level, rng)
}

func synthesisDrawPoint(level *synthesisLevel, rng *synthesisRNG) (pmPoint, bool) {
	minX, maxX := synthesisPatchHalf, level.w-synthesisPatchHalf
	minY, maxY := synthesisPatchHalf, level.h-synthesisPatchHalf
	for rowAttempt := 0; rowAttempt < 32; rowAttempt++ {
		y := minY + int(rng.nextBlockFirst()%uint32(maxY-minY))
		for xAttempt := 0; xAttempt < 16; xAttempt++ {
			if xAttempt&3 == 0 {
				rng.beginCandidate()
			}
			x := minX + int(rng.next()%uint32(maxX-minX))
			p := pmPoint{x: int32(x), y: int32(y)}
			if synthesisValid(level, p) {
				return p, true
			}
		}
	}
	for y := minY; y < maxY; y++ {
		for x := minX; x < maxX; x++ {
			p := pmPoint{x: int32(x), y: int32(y)}
			if synthesisValid(level, p) {
				return p, true
			}
		}
	}
	return pmPoint{}, false
}

func synthesisUpscaleNNF(level *synthesisLevel, parent *synthesisSolution, _ int) {
	pw, ph := parent.level.w, parent.level.h
	repaired := make([]bool, len(level.nnf))
	for y := level.active.Min.Y; y < level.active.Max.Y; y++ {
		for x := level.active.Min.X; x < level.active.Max.X; x++ {
			id := synthesisFieldID(level, x, y)
			if !level.target[id] {
				continue
			}
			px := clampInt(int((float64(x)+0.5)*float64(pw)/float64(level.w)), 0, pw-1)
			py := clampInt(int((float64(y)+0.5)*float64(ph)/float64(level.h)), 0, ph-1)
			q := pmPoint{x: -1, y: -1}
			if synthesisFieldContains(parent.level, px, py) {
				q = parent.nnf[synthesisFieldID(parent.level, px, py)]
			}
			if q.x >= 0 && q.y >= 0 {
				dx, dy := float64(q.x)-float64(px), float64(q.y)-float64(py)
				candidate := pmPoint{
					x: int32(math.Round(float64(x) + dx*float64(level.w)/float64(pw))),
					y: int32(math.Round(float64(y) + dy*float64(level.h)/float64(ph))),
				}
				if synthesisValid(level, candidate) {
					level.nnf[id] = candidate
					repaired[id] = true
					continue
				}
			}
		}
	}

	// The native upsampler marks a correspondence invalid when its scaled
	// source falls outside the legal source mask. It then runs the same repair
	// operation twice: take a nearby valid field entry, continue that entry's
	// translation to this target, and use it when the translated source is
	// legal. In particular, it does not put an unrelated random source at an
	// invalid inherited entry. Search expanding square rings here; the native
	// implementation has fast paths for the adjacent ring before doing the
	// same outward search.
	for pass := 0; pass < 2; pass++ {
		for y := level.active.Min.Y; y < level.active.Max.Y; y++ {
			for x := level.active.Min.X; x < level.active.Max.X; x++ {
				id := synthesisFieldID(level, x, y)
				if !level.target[id] || repaired[id] {
					continue
				}
				if candidate, ok := synthesisRepairInherited(level, repaired, x, y); ok {
					level.nnf[id] = candidate
					repaired[id] = true
				}
			}
		}
	}

	// Degenerate masks can leave an island with no inherited neighbour to
	// repair from. The native routine ultimately falls back to its default
	// valid correspondence in this case; choose the first legal source rather
	// than introducing another random stream into field upsampling.
	if len(level.sources) != 0 {
		fallback := level.sources[0]
		for y := level.active.Min.Y; y < level.active.Max.Y; y++ {
			for x := level.active.Min.X; x < level.active.Max.X; x++ {
				id := synthesisFieldID(level, x, y)
				if level.target[id] && !repaired[id] {
					level.nnf[id] = fallback
				}
			}
		}
	}
}

func synthesisRepairInherited(level *synthesisLevel, repaired []bool, x, y int) (pmPoint, bool) {
	maxRadius := maxInt(level.active.Dx(), level.active.Dy())
	for radius := 1; radius <= maxRadius; radius++ {
		minX := maxInt(level.active.Min.X, x-radius)
		maxX := minInt(level.active.Max.X-1, x+radius)
		minY := maxInt(level.active.Min.Y, y-radius)
		maxY := minInt(level.active.Max.Y-1, y+radius)
		for ny := minY; ny <= maxY; ny++ {
			for nx := minX; nx <= maxX; nx++ {
				if nx != minX && nx != maxX && ny != minY && ny != maxY {
					continue
				}
				nid := synthesisFieldID(level, nx, ny)
				if !repaired[nid] {
					continue
				}
				q := level.nnf[nid]
				candidate := pmPoint{x: q.x + int32(x-nx), y: q.y + int32(y-ny)}
				if synthesisValid(level, candidate) {
					return candidate, true
				}
				// Some native repair paths copy the neighbour unchanged if
				// continuing its translation crosses the source-region mask.
				if synthesisValid(level, q) {
					return q, true
				}
			}
		}
	}
	return pmPoint{}, false
}

func synthesisRefreshCosts(level *synthesisLevel, working *image.NRGBA) {
	_ = parallelRowsSized(context.Background(), level.active.Min.Y, level.active.Max.Y, level.active.Dx()*synthesisPatchSize, func(y int) {
		for x := level.active.Min.X; x < level.active.Max.X; x++ {
			id := synthesisFieldID(level, x, y)
			if !level.target[id] || !synthesisValid(level, level.nnf[id]) {
				level.cost[id] = math.MaxUint32
				continue
			}
			level.cost[id] = synthesisPatchCost(level, working, x, y, level.nnf[id], math.MaxUint32)
		}
	})
}

// synthesisRestartField solves a fresh field for the level: every target
// centre that is not frozen draws a uniformly random source (within the hole
// search window around the target), the field is refined by one primary
// dispatch, and the result is returned as per-pixel candidates for the
// incumbent. Frozen centres keep the incumbent's entry. The level's own field
// is left untouched.
func synthesisRestartField(level *synthesisLevel, working *image.NRGBA, levelIndex, round, window, primaryRadius int, frozen []bool) []pmPoint {
	nnf, cost := level.nnf, level.cost
	if cap(level.restartNNF) < len(nnf) {
		level.restartNNF = make([]pmPoint, len(nnf))
	} else {
		level.restartNNF = level.restartNNF[:len(nnf)]
	}
	if cap(level.restartCost) < len(cost) {
		level.restartCost = make([]uint32, len(cost))
	} else {
		level.restartCost = level.restartCost[:len(cost)]
	}
	copy(level.restartNNF, nnf)
	copy(level.restartCost, cost)
	level.nnf, level.cost = level.restartNNF, level.restartCost
	_ = parallelRowsSized(context.Background(), level.active.Min.Y, level.active.Max.Y, level.active.Dx()*synthesisPatchSize, func(y int) {
		rng := newSynthesisRNG(level.active.Min.X-synthesisPatchHalf, y-synthesisPatchHalf, 0, synthesisPatchHalf)
		for x := level.active.Min.X; x < level.active.Max.X; x++ {
			id := synthesisFieldID(level, x, y)
			if !level.target[id] || (frozen != nil && frozen[id]) {
				continue
			}
			cx, cy := x, y
			if synthesisValid(level, nnf[id]) {
				cx, cy = int(nnf[id].x), int(nnf[id].y)
			}
			candidate, ok := synthesisRandomSource(level, &rng, cx, cy, synthesisWindowAt(level, x, y))
			if !ok {
				continue
			}
			level.nnf[id] = candidate
			level.cost[id] = synthesisPatchCost(level, working, x, y, candidate, math.MaxUint32)
		}
	})
	synthesisSearchDispatch(level, working, primaryRadius, levelIndex, round, 0, nil, frozen)
	restart := level.nnf
	level.restartNNF, level.restartCost = level.nnf, level.cost
	level.nnf, level.cost = nnf, cost
	return restart
}

// synthesisFrozen marks the target centres whose entry the restart field keeps:
// the interiors of coherent blocks of the incumbent. A centre is a seed when
// one side of its 3x3 neighbourhood (the left, right, top or bottom triple)
// holds no neighbour with the same offset; seeds and non-target centres are
// dilated by a 3 px square and everything else is frozen. This reproduces the
// observed post-classifier active map, including its morphology.
func synthesisFrozen(level *synthesisLevel) []bool {
	same := func(x, y int, dx, dy int32) bool {
		nx, ny := x+int(dx), y+int(dy)
		if !synthesisTargetAt(level, nx, ny) {
			return false
		}
		p := level.nnf[synthesisFieldID(level, x, y)]
		q := level.nnf[synthesisFieldID(level, nx, ny)]
		return q.x-p.x == dx && q.y-p.y == dy
	}
	seed := make([]bool, len(level.nnf))
	for y := level.active.Min.Y; y < level.active.Max.Y; y++ {
		for x := level.active.Min.X; x < level.active.Max.X; x++ {
			id := synthesisFieldID(level, x, y)
			if !level.target[id] {
				seed[id] = true
				continue
			}
			// A side is coherent when any of its three neighbours continues
			// the match.
			side := func(dx, dy, ax, ay int32) bool {
				for k := int32(-1); k <= 1; k++ {
					nx, ny := dx+ax*k, dy+ay*k
					if same(x, y, nx, ny) {
						return true
					}
				}
				return false
			}
			if !side(-1, 0, 0, 1) || !side(1, 0, 0, 1) || !side(0, -1, 1, 0) || !side(0, 1, 1, 0) {
				seed[id] = true
			}
		}
	}
	frozen := make([]bool, len(level.nnf))
	for y := level.active.Min.Y; y < level.active.Max.Y; y++ {
		for x := level.active.Min.X; x < level.active.Max.X; x++ {
			id := synthesisFieldID(level, x, y)
			if !level.target[id] {
				continue
			}
			keep := true
			for ny := y - 3; ny <= y+3 && keep; ny++ {
				for nx := x - 3; nx <= x+3; nx++ {
					if !synthesisTargetAt(level, nx, ny) || seed[synthesisFieldID(level, nx, ny)] {
						keep = false
						break
					}
				}
			}
			frozen[id] = keep
		}
	}
	return frozen
}

func synthesisSearchDispatch(level *synthesisLevel, working *image.NRGBA, radius, levelIndex, round, dispatch int, restart []pmPoint, skip []bool) {
	synthesisSearchSweep(level, working, radius, levelIndex, round, dispatch, false, restart, skip)
	synthesisSearchSweep(level, working, radius, levelIndex, round, dispatch, true, restart, skip)
}

func synthesisSearchSweep(level *synthesisLevel, working *image.NRGBA, radius, levelIndex, round, dispatch int, reverse bool, restart []pmPoint, skip []bool) {
	y0, yStep := level.active.Min.Y, 1
	x0, xStep := level.active.Min.X, 1
	if reverse {
		y0, yStep = level.active.Max.Y-1, -1
		x0, xStep = level.active.Max.X-1, -1
	}
	// A pixel reads only the two predecessors of this pass, so the dependency
	// graph admits a wavefront: tile the active rect and walk the anti-diagonals
	// of the TILE grid. A tile's only predecessors are the tiles to its left and
	// above, which both sit on the previous tile diagonal, so tiles on one
	// diagonal are independent while each tile is still swept in scan order
	// internally. That keeps the 7x7 compare windows overlapping column-wise
	// inside a tile - a per-pixel diagonal would stride memory by w-1 and lose
	// that reuse - and costs one barrier per tile diagonal instead of one per
	// pixel diagonal. This is the engine's dependency-scheduled block graph, and
	// the result is identical to a plain scan either way.
	width, height := level.active.Dx(), level.active.Dy()
	pixel := func(px, py int) {
		synthesisSearchPixel(level, working, x0+px*xStep, y0+py*yStep, radius, levelIndex, round, xStep, yStep, reverse, restart, skip)
	}
	const synthesisSweepTile = 16
	workers := minInt(maxInt(1, runtime.GOMAXPROCS(0)-1), 8)
	tilesX := (width + synthesisSweepTile - 1) / synthesisSweepTile
	tilesY := (height + synthesisSweepTile - 1) / synthesisSweepTile
	if workers < 2 || minInt(tilesX, tilesY) < 2 {
		for py := 0; py < height; py++ {
			for px := 0; px < width; px++ {
				pixel(px, py)
			}
		}
		return
	}
	sweepTile := func(bx, by int) {
		xEnd := minInt(width, (bx+1)*synthesisSweepTile)
		yEnd := minInt(height, (by+1)*synthesisSweepTile)
		for py := by * synthesisSweepTile; py < yEnd; py++ {
			for px := bx * synthesisSweepTile; px < xEnd; px++ {
				pixel(px, py)
			}
		}
	}
	type tileJob struct{ x, y int }
	jobs := make(chan tileJob, workers-1)
	var workerWG sync.WaitGroup
	var diagonalWG sync.WaitGroup
	workerWG.Add(workers - 1)
	for i := 0; i < workers-1; i++ {
		go func() {
			defer workerWG.Done()
			for job := range jobs {
				sweepTile(job.x, job.y)
				diagonalWG.Done()
			}
		}()
	}
	for d := 0; d < tilesX+tilesY-1; d++ {
		lo := maxInt(0, d-(tilesX-1))
		hi := minInt(d, tilesY-1)
		if lo == hi {
			sweepTile(d-lo, lo)
			continue
		}
		diagonalWG.Add(hi - lo)
		for by := lo + 1; by <= hi; by++ {
			jobs <- tileJob{x: d - by, y: by}
		}
		sweepTile(d-lo, lo)
		diagonalWG.Wait()
	}
	close(jobs)
	workerWG.Wait()
}

// synthesisSearchPixel is one invocation of the engine's search kernel: the two
// axial predecessors of the pass, then random search around the current best
// with the radius halving to zero.
func synthesisSearchPixel(level *synthesisLevel, working *image.NRGBA, x, y, radius, levelIndex, round, xStep, yStep int, reverse bool, restart []pmPoint, skip []bool) {
	id := synthesisFieldID(level, x, y)
	if !level.target[id] || skip != nil && skip[id] || !synthesisValid(level, level.nnf[id]) {
		return
	}
	if restart != nil {
		synthesisTry(level, working, x, y, restart[id], id)
	}
	if nx := x - xStep; nx >= level.active.Min.X && nx < level.active.Max.X {
		q := level.nnf[synthesisFieldID(level, nx, y)]
		synthesisTry(level, working, x, y, pmPoint{x: q.x + int32(xStep), y: q.y}, id)
	}
	if ny := y - yStep; ny >= level.active.Min.Y && ny < level.active.Max.Y {
		q := level.nnf[synthesisFieldID(level, x, ny)]
		synthesisTry(level, working, x, y, pmPoint{x: q.x, y: q.y + int32(yStep)}, id)
	}
	rng := newSynthesisSearchRNG(x-level.active.Min.X, y-level.active.Min.Y, levelIndex, round, reverse)
	for r := radius; r >= 1; r /= 2 {
		best := level.nnf[id]
		minX := maxInt(synthesisPatchHalf, int(best.x)-r)
		maxX := minInt(level.w-synthesisPatchHalf-1, int(best.x)+r)
		minY := maxInt(synthesisPatchHalf, int(best.y)-r)
		maxY := minInt(level.h-synthesisPatchHalf-1, int(best.y)+r)
		// The kernel draws inside the valid rect only: the window is
		// enforced as a rejection in synthesisTry, not by narrowing the
		// draw range (narrowing it would change the modulo and so the
		// drawn coordinate). Only the initializer clips its draw range.
		if minX > maxX || minY > maxY {
			continue
		}
		word := rng.next()
		lo, hi := uint32(uint16(word)), uint32(uint16(word>>16))
		sx := minX + int((lo+hi)%uint32(maxX-minX+1))
		sy := minY + int((lo-hi)%uint32(maxY-minY+1))
		synthesisTry(level, working, x, y, pmPoint{x: int32(sx), y: int32(sy)}, id)
	}
}

func synthesisTry(level *synthesisLevel, working *image.NRGBA, tx, ty int, candidate pmPoint, id int) {
	if win := synthesisWindowAt(level, tx, ty); win > 0 &&
		(absInt(int(candidate.x)-tx) >= win || absInt(int(candidate.y)-ty) >= win) {
		return
	}
	if !synthesisValid(level, candidate) || candidate == level.nnf[id] {
		return
	}
	cost := synthesisPatchCost(level, working, tx, ty, candidate, level.cost[id])
	if cost < level.cost[id] {
		level.nnf[id], level.cost[id] = candidate, cost
	}
}

func synthesisPatchCost(level *synthesisLevel, target *image.NRGBA, tx, ty int, source pmPoint, limit uint32) uint32 {
	sx, sy := int(source.x), int(source.y)
	if pmOpaqueKernelAvailable() {
		args := pmOpaqueKernelArgs{
			target:       &target.Pix[(ty-synthesisPatchHalf)*target.Stride+(tx-synthesisPatchHalf)*4],
			source:       &level.plane.Pix[(sy-synthesisPatchHalf)*level.plane.Stride+(sx-synthesisPatchHalf)*4],
			targetStride: target.Stride,
			sourceStride: level.plane.Stride,
			limit:        limit,
		}
		return pmRunSynthesisOpaqueKernel(&args)
	}
	var sum uint32
	for py := -synthesisPatchHalf; py <= synthesisPatchHalf; py++ {
		ti := (ty+py)*target.Stride + (tx-synthesisPatchHalf)*4
		si := (sy+py)*level.plane.Stride + (sx-synthesisPatchHalf)*4
		for px := 0; px < synthesisPatchSize; px++ {
			for c := 0; c < 3; c++ {
				d := int32(target.Pix[ti+px*4+c]) - int32(level.plane.Pix[si+px*4+c])
				sum += uint32(d * d)
			}
		}
		if sum >= limit {
			return sum
		}
	}
	return sum
}

func synthesisVote(ctx context.Context, level *synthesisLevel, working *image.NRGBA) (*image.NRGBA, error) {
	synthesisCoherence(level)
	out := cloneNRGBA(working)
	err := parallelRowsSized(ctx, level.painted.Min.Y, level.painted.Max.Y, level.painted.Dx()*synthesisPatchSize, func(y int) {
		for x := level.painted.Min.X; x < level.painted.Max.X; x++ {
			if level.mask.Pix[y*level.mask.Stride+x] == 0 {
				continue
			}
			var sum [4]float64
			var total float64
			for cy := maxInt(level.active.Min.Y, y-synthesisPatchHalf); cy <= minInt(level.active.Max.Y-1, y+synthesisPatchHalf); cy++ {
				fieldRow := (cy-level.active.Min.Y)*level.fieldStride - level.active.Min.X
				deltaY := y - cy
				for cx := maxInt(level.active.Min.X, x-synthesisPatchHalf); cx <= minInt(level.active.Max.X-1, x+synthesisPatchHalf); cx++ {
					id := fieldRow + cx
					weight := float64(level.coherence[id])
					if weight == 0 {
						continue
					}
					sx := int(level.nnf[id].x) + x - cx
					sy := int(level.nnf[id].y) + deltaY
					si := sy*level.src.Stride + sx*4
					for c := 0; c < 4; c++ {
						sum[c] += weight * float64(level.src.Pix[si+c])
					}
					total += weight
				}
			}
			if total == 0 {
				continue
			}
			// Every covered pixel is replaced outright (the synthesis treats
			// any mask coverage as hole and never blends the hole's own
			// pixels back in, so a feathered brush edge cannot leave a rim
			// of the removed content).
			di := y*out.Stride + x*4
			for c := 0; c < 4; c++ {
				out.Pix[di+c] = byte(clampInt(int(sum[c]/total+0.5), 0, 255))
			}
		}
	})
	return out, err
}

func synthesisCoherence(level *synthesisLevel) {
	fieldSize := level.active.Dx() * level.active.Dy()
	if cap(level.coherence) < fieldSize {
		level.coherence = make([]float32, fieldSize)
	} else {
		level.coherence = level.coherence[:fieldSize]
		clear(level.coherence)
	}
	table := [...]float32{0.1, 0.2, 0.3, 0.6, 1}
	diagonals := [...]image.Point{{X: -1, Y: -1}, {X: 1, Y: -1}, {X: -1, Y: 1}, {X: 1, Y: 1}}
	for y := level.active.Min.Y; y < level.active.Max.Y; y++ {
		for x := level.active.Min.X; x < level.active.Max.X; x++ {
			id := synthesisFieldID(level, x, y)
			match := level.nnf[id]
			if !level.target[id] || !synthesisValid(level, match) {
				continue
			}
			coherent, available := 0, 0
			for _, d := range diagonals {
				nx, ny := x+d.X, y+d.Y
				if nx < level.active.Min.X || ny < level.active.Min.Y || nx >= level.active.Max.X || ny >= level.active.Max.Y {
					continue
				}
				nid := synthesisFieldID(level, nx, ny)
				neighbor := level.nnf[nid]
				if !level.target[nid] || !synthesisValid(level, neighbor) {
					continue
				}
				available++
				if neighbor.x == match.x+int32(d.X) && neighbor.y == match.y+int32(d.Y) {
					coherent++
				}
			}
			if available == 0 {
				level.coherence[id] = table[0]
				continue
			}
			position := float32(4*coherent) / float32(available)
			lo := clampInt(int(position), 0, 4)
			hi := minInt(4, lo+1)
			fraction := position - float32(lo)
			level.coherence[id] = table[lo] + (table[hi]-table[lo])*fraction
		}
	}
}

func synthesisValid(level *synthesisLevel, p pmPoint) bool {
	x, y := int(p.x), int(p.y)
	return x >= 0 && y >= 0 && x < level.w && y < level.h && level.valid[y*level.w+x]
}

// pmWorkingROI chooses the part of the source image to work in. Its size comes
// from the mask's area rather than its longest side, so a long thin scratch
// gets a roughly square region of nearby material instead of a wide one
// reaching far away. The region always covers the mask plus full patch and
// filter support, and shifts inward at the image edges rather than being
// clipped short.
func pmWorkingROI(maskBounds, imageBounds image.Rectangle, patchSize int) image.Rectangle {
	span := int(math.Ceil(4 * math.Sqrt(float64(maxInt(50, maskBounds.Dx()))*float64(maxInt(50, maskBounds.Dy())))))
	halo := patchSize + 8
	w := minInt(imageBounds.Dx(), maxInt(span, maskBounds.Dx()+2*halo))
	h := minInt(imageBounds.Dy(), maxInt(span, maskBounds.Dy()+2*halo))
	x := clampInt(maskBounds.Min.X+(maskBounds.Dx()-w)/2, imageBounds.Min.X, imageBounds.Max.X-w)
	y := clampInt(maskBounds.Min.Y+(maskBounds.Dy()-h)/2, imageBounds.Min.Y, imageBounds.Max.Y-h)
	return image.Rect(x, y, x+w, y+h)
}

func cropNRGBA(src *image.NRGBA, bounds image.Rectangle) *image.NRGBA {
	out := image.NewNRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	point := src.Bounds().Min.Add(bounds.Min)
	draw.Draw(out, out.Bounds(), src, point, draw.Src)
	return out
}

func cropAlpha(src *image.Alpha, bounds image.Rectangle, fullW, fullH int) *image.Alpha {
	out := image.NewAlpha(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	if src == nil {
		return out
	}
	// Preserve the original API convention: mask's top-left corresponds to the
	// source image's top-left regardless of either rectangle's absolute Min.
	point := src.Bounds().Min.Add(bounds.Min)
	draw.Draw(out, out.Bounds(), src, point, draw.Src)
	return out
}

func maskBoundsInImage(mask *image.Alpha, w, h int) image.Rectangle {
	if mask == nil || w <= 0 || h <= 0 {
		return image.Rectangle{}
	}
	mw, mh := minInt(w, mask.Bounds().Dx()), minInt(h, mask.Bounds().Dy())
	minX, minY, maxX, maxY := mw, mh, 0, 0
	for y := 0; y < mh; y++ {
		row := y * mask.Stride
		for x := 0; x < mw; x++ {
			if mask.Pix[row+x] == 0 {
				continue
			}
			minX = minInt(minX, x)
			minY = minInt(minY, y)
			maxX = maxInt(maxX, x+1)
			maxY = maxInt(maxY, y+1)
		}
	}
	if minX == mw {
		return image.Rectangle{}
	}
	return image.Rect(minX, minY, maxX, maxY)
}

func normalizePatchSize(patchSize, w, h int) int {
	if patchSize < 3 {
		patchSize = 3
	}
	if patchSize > 15 {
		patchSize = 15
	}
	if patchSize&1 == 0 {
		patchSize++
	}
	maxPatch := minInt(w, h)
	if maxPatch&1 == 0 {
		maxPatch--
	}
	if maxPatch < 1 {
		return 1
	}
	if patchSize > maxPatch {
		patchSize = maxPatch
	}
	return patchSize
}

func pmBilinearParent(src *image.NRGBA, dstW, dstH, x, y int) [4]byte {
	sw, sh := src.Bounds().Dx(), src.Bounds().Dy()
	fx := (float64(x)+0.5)*float64(sw)/float64(dstW) - 0.5
	fy := (float64(y)+0.5)*float64(sh)/float64(dstH) - 0.5
	x0 := clampInt(int(math.Floor(fx)), 0, sw-1)
	y0 := clampInt(int(math.Floor(fy)), 0, sh-1)
	x1 := minInt(sw-1, x0+1)
	y1 := minInt(sh-1, y0+1)
	tx := float32(fx - math.Floor(fx))
	ty := float32(fy - math.Floor(fy))
	var out [4]byte
	for c := 0; c < 4; c++ {
		v00 := float32(src.Pix[y0*src.Stride+x0*4+c])
		v10 := float32(src.Pix[y0*src.Stride+x1*4+c])
		v01 := float32(src.Pix[y1*src.Stride+x0*4+c])
		v11 := float32(src.Pix[y1*src.Stride+x1*4+c])
		v0 := v00 + (v10-v00)*tx
		v1 := v01 + (v11-v01)*tx
		out[c] = byte(clampFloat32(v0 + (v1-v0)*ty))
	}
	return out
}

func maskedIntegral(mask *image.Alpha) []int {
	w, h := mask.Bounds().Dx(), mask.Bounds().Dy()
	integral := make([]int, (w+1)*(h+1))
	for y := 0; y < h; y++ {
		row := 0
		for x := 0; x < w; x++ {
			if mask.Pix[y*mask.Stride+x] != 0 {
				row++
			}
			integral[(y+1)*(w+1)+x+1] = integral[y*(w+1)+x+1] + row
		}
	}
	return integral
}

func integralRectSum(integral []int, stride, x0, y0, x1, y1 int) int {
	return integral[y1*stride+x1] - integral[y0*stride+x1] - integral[y1*stride+x0] + integral[y0*stride+x0]
}

func maskBounds(mask *image.Alpha) image.Rectangle {
	w, h := mask.Bounds().Dx(), mask.Bounds().Dy()
	minX, minY, maxX, maxY := w, h, 0, 0
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if mask.Pix[y*mask.Stride+x] == 0 {
				continue
			}
			minX = minInt(minX, x)
			minY = minInt(minY, y)
			maxX = maxInt(maxX, x+1)
			maxY = maxInt(maxY, y+1)
		}
	}
	if minX == w {
		return image.Rectangle{}
	}
	return image.Rect(minX, minY, maxX, maxY)
}

func normalizeNRGBA(src *image.NRGBA) *image.NRGBA {
	out := image.NewNRGBA(image.Rect(0, 0, src.Bounds().Dx(), src.Bounds().Dy()))
	draw.Draw(out, out.Bounds(), src, src.Bounds().Min, draw.Src)
	return out
}

func normalizeAlpha(src *image.Alpha, w, h int) *image.Alpha {
	out := image.NewAlpha(image.Rect(0, 0, w, h))
	if src != nil {
		draw.Draw(out, out.Bounds(), src, src.Bounds().Min, draw.Src)
	}
	return out
}

func cloneNRGBA(src *image.NRGBA) *image.NRGBA {
	out := image.NewNRGBA(src.Bounds())
	copy(out.Pix, src.Pix)
	return out
}

// synthesisBlackHole returns an opaque copy of src with every masked pixel
// black.
func synthesisBlackHole(src *image.NRGBA, mask *image.Alpha) *image.NRGBA {
	out := cloneNRGBA(src)
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := y*out.Stride + x*4
			if mask.Pix[y*mask.Stride+x] != 0 {
				out.Pix[i], out.Pix[i+1], out.Pix[i+2] = 0, 0, 0
			}
			out.Pix[i+3] = 255
		}
	}
	return out
}

// synthesisMaskedResize area-averages src. With a non-nil mask it averages
// known pixels only and renormalises their weights; with a nil mask every
// pixel participates, including black pixels in an already-cleared hole.
// An output pixel with no contributor is black. The result is opaque.
func synthesisMaskedResize(src *image.NRGBA, mask *image.Alpha, w, h int) *image.NRGBA {
	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	sw, sh := src.Bounds().Dx(), src.Bounds().Dy()
	sx, sy := float64(w)/float64(sw), float64(h)/float64(sh)
	// Gather rather than scatter: each output pixel sums the source pixels its
	// cell overlaps. The contributing pixels are visited in the same source scan
	// order as the old scatter, so the accumulation is bit-identical, but the
	// writes are private to one output row and the rows can run in parallel.
	// (Precomputing the per-axis overlap weights was tried and is slower: the
	// indirection costs more than the min/max it removes.)
	invX, invY := float64(sw)/float64(w), float64(sh)/float64(h)
	_ = parallelRowsSized(context.Background(), 0, h, w, func(oy int) {
		yLo := maxInt(0, int(float64(oy)*invY)-1)
		yHi := minInt(sh-1, int(float64(oy+1)*invY)+1)
		for ox := 0; ox < w; ox++ {
			xLo := maxInt(0, int(float64(ox)*invX)-1)
			xHi := minInt(sw-1, int(float64(ox+1)*invX)+1)
			var sum [3]float64
			var weight float64
			for y := yLo; y <= yHi; y++ {
				y0, y1 := float64(y)*sy, float64(y+1)*sy
				cy := minFloat64(y1, float64(oy+1)) - maxFloat64(y0, float64(oy))
				if cy <= 0 {
					continue
				}
				row := y * src.Stride
				for x := xLo; x <= xHi; x++ {
					if mask != nil && mask.Pix[y*mask.Stride+x] != 0 {
						continue
					}
					x0, x1 := float64(x)*sx, float64(x+1)*sx
					cx := minFloat64(x1, float64(ox+1)) - maxFloat64(x0, float64(ox))
					if cx <= 0 {
						continue
					}
					wgt := cx * cy
					si := row + x*4
					for c := 0; c < 3; c++ {
						sum[c] += wgt * float64(src.Pix[si+c])
					}
					weight += wgt
				}
			}
			di := oy*out.Stride + ox*4
			if weight > 0 {
				for c := 0; c < 3; c++ {
					out.Pix[di+c] = byte(clampInt(int(sum[c]/weight+0.5), 0, 255))
				}
			}
			out.Pix[di+3] = 255
		}
	})
	return out
}

func synthesisResizeAlpha(src *image.Alpha, w, h int) *image.Alpha {
	out := image.NewAlpha(image.Rect(0, 0, w, h))
	sw, sh := src.Bounds().Dx(), src.Bounds().Dy()
	sx, sy := float64(w)/float64(sw), float64(h)/float64(sh)
	for y := 0; y < sh; y++ {
		y0, y1 := float64(y)*sy, float64(y+1)*sy
		for x := 0; x < sw; x++ {
			if src.Pix[y*src.Stride+x] == 0 {
				continue
			}
			x0, x1 := float64(x)*sx, float64(x+1)*sx
			for oy := int(y0); oy < h && float64(oy) < y1; oy++ {
				for ox := int(x0); ox < w && float64(ox) < x1; ox++ {
					out.Pix[oy*out.Stride+ox] = 255
				}
			}
		}
	}
	return out
}

// synthesisAreaMask reduces the hole by exact footprint coverage. A small
// non-zero threshold reproduces the filtered target activity observed in the
// native coarse-level view without expanding to the conservative source mask.
func synthesisAreaMask(src *image.Alpha, w, h int) *image.Alpha {
	out := image.NewAlpha(image.Rect(0, 0, w, h))
	sw, sh := src.Bounds().Dx(), src.Bounds().Dy()
	coverage := make([]float64, w*h)
	sx, sy := float64(w)/float64(sw), float64(h)/float64(sh)
	for y := 0; y < sh; y++ {
		y0, y1 := float64(y)*sy, float64(y+1)*sy
		for x := 0; x < sw; x++ {
			alpha := float64(src.Pix[y*src.Stride+x]) / 255
			if alpha == 0 {
				continue
			}
			x0, x1 := float64(x)*sx, float64(x+1)*sx
			for oy := int(y0); oy < h && float64(oy) < y1; oy++ {
				cy := minFloat64(y1, float64(oy+1)) - maxFloat64(y0, float64(oy))
				for ox := int(x0); ox < w && float64(ox) < x1; ox++ {
					cx := minFloat64(x1, float64(ox+1)) - maxFloat64(x0, float64(ox))
					if cx > 0 && cy > 0 {
						coverage[oy*w+ox] += alpha * cx * cy
					}
				}
			}
		}
	}
	for i, value := range coverage {
		if value >= 0.1 {
			out.Pix[i] = 255
		}
	}
	return out
}

// synthesisPointMask samples the full-resolution hole at each coarse pixel
// centre. It is intentionally narrower than synthesisResizeAlpha: the latter
// conservatively classifies every coarse pixel touched by the hole, while this
// mask controls which pixels the vote actually replaces.
func synthesisPointMask(src *image.Alpha, w, h int) *image.Alpha {
	out := image.NewAlpha(image.Rect(0, 0, w, h))
	sw, sh := src.Bounds().Dx(), src.Bounds().Dy()
	for y := 0; y < h; y++ {
		sy := clampInt(int((float64(y)+0.5)*float64(sh)/float64(h)), 0, sh-1)
		for x := 0; x < w; x++ {
			sx := clampInt(int((float64(x)+0.5)*float64(sw)/float64(w)), 0, sw-1)
			if src.Pix[sy*src.Stride+sx] != 0 {
				out.Pix[y*out.Stride+x] = 255
			}
		}
	}
	return out
}

func newSynthesisRNG(x, y, level, parameter int) synthesisRNG {
	return synthesisRNG{
		counter: [4]uint32{
			uint32(x) + 0xbeefdead,
			uint32(y) + 0xcafebead,
			0x56781234 + uint32(level),
			0xcdef90ab + uint32(parameter),
		},
		used: 4,
	}
}

func newSynthesisSearchRNG(x, y, level, round int, reverse bool) synthesisRNG {
	pass := uint32(0)
	if reverse {
		pass = 1
	}
	return synthesisRNG{
		counter: [4]uint32{
			uint32(x) + 0xdeadaeef,
			uint32(y) + 0xbeadcafe,
			0x12345678 + uint32(level)*0x10000 + pass,
			0x90abcdef + uint32(level)*0x10000 + uint32(round),
		},
		used: 4,
	}
}

func (r *synthesisRNG) next() uint32 {
	if r.used == len(r.words) {
		var block [16]byte
		for i, word := range r.counter {
			binary.LittleEndian.PutUint32(block[i*4:], word)
		}
		synthesisEncryptCounter(&block)
		for i := range r.words {
			r.words[i] = binary.LittleEndian.Uint32(block[i*4:])
		}
		r.counter[0]++
		if r.counter[0] == 0 {
			r.counter[1]++
		}
		r.used = 0
	}
	word := r.words[r.used]
	r.used++
	return word
}

// beginCandidate discards unused words from the previous pixel. The native
// initializer always requests a fresh 128-bit block for attempt zero, then
// consumes the other three words only when that pixel needs retries.
func (r *synthesisRNG) beginCandidate() {
	r.used = len(r.words)
}

func (r *synthesisRNG) nextBlockFirst() uint32 {
	r.beginCandidate()
	return r.next()
}

var synthesisRoundKeys = [11][16]byte{
	{0x11, 0x11, 0x11, 0x22, 0x22, 0x22, 0x22, 0x33, 0x33, 0x33, 0x33, 0x44, 0x44, 0x44, 0x44, 0x0b},
	{0x0a, 0x0a, 0x0a, 0x29, 0x28, 0x28, 0x28, 0x1a, 0x1b, 0x1b, 0x1b, 0x5e, 0x5f, 0x5f, 0x5f, 0xc6},
	{0xc5, 0xc5, 0x52, 0xef, 0xed, 0xed, 0x7a, 0xf5, 0xf6, 0xf6, 0x61, 0xab, 0xa9, 0xa9, 0x3e, 0x11},
	{0x16, 0x77, 0x30, 0xfe, 0xfb, 0x9a, 0x4a, 0x0b, 0x0d, 0x6c, 0x2b, 0xa0, 0xa4, 0xc5, 0x15, 0x50},
	{0xb0, 0x2e, 0xd0, 0xae, 0x4b, 0xb4, 0x9a, 0xa5, 0x46, 0xd8, 0xb1, 0x05, 0xe2, 0x1d, 0xa4, 0xd8},
	{0x14, 0x67, 0xbb, 0x76, 0x5f, 0xd3, 0x21, 0xd3, 0x19, 0x0b, 0x90, 0xd6, 0xfb, 0x16, 0x34, 0xf7},
	{0x53, 0x7f, 0x4d, 0x81, 0x0c, 0xac, 0x6c, 0x52, 0x15, 0xa7, 0xfc, 0x84, 0xee, 0xb1, 0xc8, 0x9f},
	{0x9b, 0x97, 0x12, 0x1e, 0x97, 0x3b, 0x7e, 0x4c, 0x82, 0x9c, 0x82, 0xc8, 0x6c, 0x2d, 0x4a, 0x4f},
	{0x43, 0x41, 0xfa, 0x51, 0xd4, 0x7a, 0x84, 0x1d, 0x56, 0xe6, 0x06, 0xd5, 0x3a, 0xcb, 0x4c, 0xd4},
	{0x5c, 0x68, 0xf9, 0x85, 0x88, 0x12, 0x7d, 0x98, 0xde, 0xf4, 0x7b, 0x4d, 0xe4, 0x3f, 0x37, 0x8b},
	{0x29, 0xf2, 0x1a, 0x0e, 0xa1, 0xe0, 0x67, 0x96, 0x7f, 0x14, 0x1c, 0xdb, 0x9b, 0x2b, 0x2b, 0x11},
}

func synthesisEncryptCounter(block *[16]byte) {
	if synthesisUseAESHardware {
		synthesisEncryptCounterHardware(block, &synthesisRoundKeys)
		return
	}
	synthesisEncryptCounterScalar(block)
}

func synthesisEncryptCounterScalar(block *[16]byte) {
	for i := range block {
		block[i] ^= synthesisRoundKeys[0][i]
	}
	for round := 1; round < len(synthesisRoundKeys); round++ {
		synthesisAESRound(block, &synthesisRoundKeys[round], round == len(synthesisRoundKeys)-1)
	}
}

func synthesisAESRound(block, key *[16]byte, last bool) {
	var state [16]byte
	for column := 0; column < 4; column++ {
		for row := 0; row < 4; row++ {
			state[column*4+row] = synthesisSBox[block[((column+row)&3)*4+row]]
		}
	}
	if !last {
		for column := 0; column < 4; column++ {
			i := column * 4
			a, b, c, d := state[i], state[i+1], state[i+2], state[i+3]
			state[i] = synthesisGMul2(a) ^ synthesisGMul2(b) ^ b ^ c ^ d
			state[i+1] = a ^ synthesisGMul2(b) ^ synthesisGMul2(c) ^ c ^ d
			state[i+2] = a ^ b ^ synthesisGMul2(c) ^ synthesisGMul2(d) ^ d
			state[i+3] = synthesisGMul2(a) ^ a ^ b ^ c ^ synthesisGMul2(d)
		}
	}
	for i := range block {
		block[i] = state[i] ^ key[i]
	}
}

func synthesisGMul2(x byte) byte {
	if x&0x80 != 0 {
		return x<<1 ^ 0x1b
	}
	return x << 1
}

var synthesisSBox = [256]byte{
	0x63, 0x7c, 0x77, 0x7b, 0xf2, 0x6b, 0x6f, 0xc5, 0x30, 0x01, 0x67, 0x2b, 0xfe, 0xd7, 0xab, 0x76,
	0xca, 0x82, 0xc9, 0x7d, 0xfa, 0x59, 0x47, 0xf0, 0xad, 0xd4, 0xa2, 0xaf, 0x9c, 0xa4, 0x72, 0xc0,
	0xb7, 0xfd, 0x93, 0x26, 0x36, 0x3f, 0xf7, 0xcc, 0x34, 0xa5, 0xe5, 0xf1, 0x71, 0xd8, 0x31, 0x15,
	0x04, 0xc7, 0x23, 0xc3, 0x18, 0x96, 0x05, 0x9a, 0x07, 0x12, 0x80, 0xe2, 0xeb, 0x27, 0xb2, 0x75,
	0x09, 0x83, 0x2c, 0x1a, 0x1b, 0x6e, 0x5a, 0xa0, 0x52, 0x3b, 0xd6, 0xb3, 0x29, 0xe3, 0x2f, 0x84,
	0x53, 0xd1, 0x00, 0xed, 0x20, 0xfc, 0xb1, 0x5b, 0x6a, 0xcb, 0xbe, 0x39, 0x4a, 0x4c, 0x58, 0xcf,
	0xd0, 0xef, 0xaa, 0xfb, 0x43, 0x4d, 0x33, 0x85, 0x45, 0xf9, 0x02, 0x7f, 0x50, 0x3c, 0x9f, 0xa8,
	0x51, 0xa3, 0x40, 0x8f, 0x92, 0x9d, 0x38, 0xf5, 0xbc, 0xb6, 0xda, 0x21, 0x10, 0xff, 0xf3, 0xd2,
	0xcd, 0x0c, 0x13, 0xec, 0x5f, 0x97, 0x44, 0x17, 0xc4, 0xa7, 0x7e, 0x3d, 0x64, 0x5d, 0x19, 0x73,
	0x60, 0x81, 0x4f, 0xdc, 0x22, 0x2a, 0x90, 0x88, 0x46, 0xee, 0xb8, 0x14, 0xde, 0x5e, 0x0b, 0xdb,
	0xe0, 0x32, 0x3a, 0x0a, 0x49, 0x06, 0x24, 0x5c, 0xc2, 0xd3, 0xac, 0x62, 0x91, 0x95, 0xe4, 0x79,
	0xe7, 0xc8, 0x37, 0x6d, 0x8d, 0xd5, 0x4e, 0xa9, 0x6c, 0x56, 0xf4, 0xea, 0x65, 0x7a, 0xae, 0x08,
	0xba, 0x78, 0x25, 0x2e, 0x1c, 0xa6, 0xb4, 0xc6, 0xe8, 0xdd, 0x74, 0x1f, 0x4b, 0xbd, 0x8b, 0x8a,
	0x70, 0x3e, 0xb5, 0x66, 0x48, 0x03, 0xf6, 0x0e, 0x61, 0x35, 0x57, 0xb9, 0x86, 0xc1, 0x1d, 0x9e,
	0xe1, 0xf8, 0x98, 0x11, 0x69, 0xd9, 0x8e, 0x94, 0x9b, 0x1e, 0x87, 0xe9, 0xce, 0x55, 0x28, 0xdf,
	0x8c, 0xa1, 0x89, 0x0d, 0xbf, 0xe6, 0x42, 0x68, 0x41, 0x99, 0x2d, 0x0f, 0xb0, 0x54, 0xbb, 0x16,
}

func parallelRowsSized(ctx context.Context, start, end, width int, fn func(y int)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	rows := end - start
	if rows <= 0 {
		return nil
	}
	work := rows * maxInt(1, width)
	// Keep a processor available for the preview/IPC and the desktop WebView.
	// Limit this solver rather than changing the process-wide GOMAXPROCS.
	workers := minInt(maxInt(1, runtime.GOMAXPROCS(0)-1), rows)
	if workers <= 1 || work < 12000 || rows < 6 {
		for y := start; y < end; y++ {
			if err := ctx.Err(); err != nil {
				return err
			}
			fn(y)
			// Also cooperate when only one processor is available. A row can
			// contain many SIMD calls, so don't rely solely on preemption.
			runtime.Gosched()
		}
		return nil
	}
	var wg sync.WaitGroup
	wg.Add(workers)
	for worker := 0; worker < workers; worker++ {
		go func(worker int) {
			defer wg.Done()
			for y := start + worker; y < end; y += workers {
				if ctx.Err() != nil {
					return
				}
				fn(y)
			}
		}(worker)
	}
	wg.Wait()
	return ctx.Err()
}

func clampFloat32(value float32) float32 {
	if value < 0 {
		return 0
	}
	if value > 255 {
		return 255
	}
	return value + 0.5
}

func clampInt(value, low, high int) int {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// minFloat64/maxFloat64 replace math.Min/math.Max in the resampling loops.
// Those are assembly calls that do not inline because of their NaN and signed
// zero semantics; every overlap term here is an ordinary finite positive, so a
// plain comparison is identical and much cheaper.
func minFloat64(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func maxFloat64(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
