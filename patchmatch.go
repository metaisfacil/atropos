package main

import (
	"context"
	"errors"
	"image"
	"image/draw"
	"math"
	"runtime"
	"sync"
)

// PatchMatchFill replaces pixels covered by mask with patches sampled from
// elsewhere in src. It preserves the original API; performance-sensitive brush
// code should prefer PatchMatchFillROI or PatchMatchFillBounds with a known
// dirty rectangle.
func PatchMatchFill(ctx context.Context, src *image.NRGBA, mask *image.Alpha, patchSize, iterations int) (*image.NRGBA, error) {
	return PatchMatchFillBounds(ctx, src, mask, image.Rectangle{}, patchSize, iterations)
}

// PatchMatchFillBounds is the drop-in full-image result API with an optional
// dirty rectangle. dirtyBounds is expressed relative to src.Bounds().Min and
// must contain every non-zero mask pixel. Passing an empty rectangle scans mask
// to discover it.
func PatchMatchFillBounds(ctx context.Context, src *image.NRGBA, mask *image.Alpha, dirtyBounds image.Rectangle, patchSize, iterations int) (*image.NRGBA, error) {
	local, workBounds, err := PatchMatchFillROI(ctx, src, mask, dirtyBounds, patchSize, iterations)
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

// PatchMatchFillROI is the lowest-latency integration API. It returns only the
// local working image and the rectangle where it belongs in the source image.
// An editor that already owns a mutable/tiled document buffer can composite
// this ROI itself and avoid the final full-document copy performed by
// PatchMatchFill/PatchMatchFillBounds.
//
// dirtyBounds follows the same convention as PatchMatchFillBounds. If there is
// nothing to fill, local is nil and workBounds is empty.
func PatchMatchFillROI(ctx context.Context, src *image.NRGBA, mask *image.Alpha, dirtyBounds image.Rectangle, patchSize, iterations int) (local *image.NRGBA, workBounds image.Rectangle, err error) {
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

func patchMatchFillLocal(ctx context.Context, source *image.NRGBA, targetMask *image.Alpha, patchSize, iterations int) (*image.NRGBA, error) {
	images, targetMasks, sourceMasks := buildPatchPyramid(source, targetMask, patchSize)
	var parent *pmSolution

	for levelIndex := len(images) - 1; levelIndex >= 0; levelIndex-- {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		level := preparePMLevel(images[levelIndex], targetMasks[levelIndex], sourceMasks[levelIndex], patchSize)
		// Fine texture and exact colour-edge structure are only useful at native
		// resolution. Coarse levels solve large displacement cheaply.
		if levelIndex == 0 {
			if err := pmPrepareStructureModel(ctx, level); err != nil {
				return nil, err
			}
			pmPrepareTextureModel(level)
		}
		if len(level.sources) == 0 || level.active.Empty() {
			continue
		}

		working := seedPMWorking(level, parent)
		rounds := 2
		if parent == nil {
			rounds = 3
		}

		var nnf []pmPoint
		var costs []float32
		var err error
		seed := parent
		for round := 0; round < rounds; round++ {
			var stats pmSolveStats
			nnf, costs, stats, err = solvePMLevel(ctx, level, working, seed, iterations, round)
			if err != nil {
				return nil, err
			}
			working, err = reconstructPMLevel(ctx, level, working, nnf, costs)
			if err != nil {
				return nil, err
			}
			seed = &pmSolution{level: level, working: working, nnf: nnf}

			// A well-seeded fine level frequently converges after one EM round. Keep
			// the configured counts as maxima, not mandatory work.
			if stats.stable && (parent != nil || round > 0) {
				break
			}
		}

		parent = &pmSolution{level: level, working: working, nnf: nnf}
	}

	if parent == nil {
		return cloneNRGBA(source), nil
	}
	return parent.working, nil
}

// pmWorkingROI contains every target center used by voting plus a generous
// random-search/source halo. It mirrors the solver's search-radius policy, so
// small touch-ups do not accidentally trigger full-document preprocessing.
func pmWorkingROI(maskBounds, imageBounds image.Rectangle, patchSize int) image.Rectangle {
	brushSpan := maxInt(maskBounds.Dx(), maskBounds.Dy())
	searchRadius := maxInt(48, brushSpan*6+patchSize*2)
	// Random search is centred on the current winner, so retain an additional
	// half-radius beyond the nominal target-centred search domain. The descriptor
	// and patch filters need only a few more pixels.
	halo := searchRadius + searchRadius/2 + patchSize + 8
	return image.Rect(
		maskBounds.Min.X-halo,
		maskBounds.Min.Y-halo,
		maskBounds.Max.X+halo,
		maskBounds.Max.Y+halo,
	).Intersect(imageBounds)
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

type pmPoint struct {
	x int32
	y int32
}

type pmLevel struct {
	src        *image.NRGBA
	mask       *image.Alpha // target coverage; preserves antialiasing/partial coverage
	sourceMask *image.Alpha // conservative binary source exclusion
	w          int
	h          int
	patchSize  int
	half       int
	active     image.Rectangle

	valid   []bool
	sources []pmPoint

	// NNF/cost storage is retained for every EM round at this level. v3
	// reallocated these full arrays for each round.
	nnf        []pmPoint
	costs      []float32
	rowChanges []int

	srcPlanes    pmPackedPlanes
	targetPlanes pmPackedPlanes

	// Texture synthesis state. textureEnergy measures local high-frequency
	// gradient RMS in source space; textureGuide carries the surrounding
	// texture level through the hole independently of provisional RGB.
	textureEnergy []float32
	textureGuide  []float32

	// Colour-aware low-frequency structure fields. structureGuide is derived
	// only from image content outside the brush mask.
	structureSource pmStructureField
	structureGuide  pmStructureField

	confidence  []float32
	confStride  int
	confSum     []float32
	insideDepth []int

	searchRadius int
	coherence    []float32
}

type pmSolution struct {
	level   *pmLevel
	working *image.NRGBA
	nnf     []pmPoint
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

func preparePMLevel(src *image.NRGBA, targetMask, sourceMask *image.Alpha, requestedPatchSize int) *pmLevel {
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	patchSize := normalizePatchSize(requestedPatchSize, w, h)
	half := patchSize / 2
	level := &pmLevel{
		src:        src,
		mask:       targetMask,
		sourceMask: sourceMask,
		w:          w,
		h:          h,
		patchSize:  patchSize,
		half:       half,
		valid:      make([]bool, w*h),
		srcPlanes:  packPMPixels(src),
	}
	level.confidence, level.confStride, level.confSum = packPMConfidence(targetMask)
	level.insideDepth = pmMaskInteriorDistance(targetMask)

	// A source center is valid only when the entire search/vote patch avoids the
	// conservative source exclusion mask. No center-only fallback is allowed.
	integral := maskedIntegral(sourceMask)
	for y := half; y < h-half; y++ {
		for x := half; x < w-half; x++ {
			if integralRectSum(integral, w+1, x-half, y-half, x+half+1, y+half+1) != 0 {
				continue
			}
			id := y*w + x
			level.valid[id] = true
			level.sources = append(level.sources, pmPoint{x: int32(x), y: int32(y)})
		}
	}
	bounds := maskBounds(targetMask)
	if !bounds.Empty() {
		// Only centers whose patches can overlap a painted output pixel need an
		// NNF. One extra cell keeps the coherence neighborhood available.
		padding := half + 1
		level.active = image.Rect(
			maxInt(half, bounds.Min.X-padding),
			maxInt(half, bounds.Min.Y-padding),
			minInt(w-half, bounds.Max.X+padding),
			minInt(h-half, bounds.Max.Y+padding),
		)
		brushSpan := maxInt(bounds.Dx(), bounds.Dy())
		level.searchRadius = minInt(maxInt(w, h), maxInt(48, brushSpan*6+patchSize*2))
	}
	return level
}

type pmSolveStats struct {
	passes      int
	lastChanges int
	active      int
	stable      bool
}

func solvePMLevel(ctx context.Context, level *pmLevel, working *image.NRGBA, seed *pmSolution, iterations, round int) ([]pmPoint, []float32, pmSolveStats, error) {
	updatePMConfidence(level, round, seed != nil)
	level.targetPlanes = packPMPixelsInto(working, level.targetPlanes)

	size := level.w * level.h
	if cap(level.nnf) < size {
		level.nnf = make([]pmPoint, size)
	} else {
		level.nnf = level.nnf[:size]
	}
	if cap(level.costs) < size {
		level.costs = make([]float32, size)
	} else {
		level.costs = level.costs[:size]
	}
	nnf, costs := level.nnf, level.costs

	sameLevelSeed := seed != nil && seed.level == level && len(seed.nnf) == size
	activeWidth := level.active.Dx()
	activeRows := level.active.Dy()
	stats := pmSolveStats{active: maxInt(1, activeWidth*activeRows)}

	if sameLevelSeed {
		// Keep the exact NNF from the previous EM round, but recompute its cost
		// against the newly reconstructed target. No reinitialization or copy is
		// required because seed.nnf and level.nnf intentionally alias.
		if err := parallelRowsSized(ctx, level.active.Min.Y, level.active.Max.Y, activeWidth, func(y int) {
			for x := level.active.Min.X; x < level.active.Max.X; x++ {
				id := y*level.w + x
				if !validPMPoint(level, nnf[id]) {
					costs[id] = float32(math.Inf(1))
					continue
				}
				costs[id] = pmPatchCost(level, &level.targetPlanes, x, y, nnf[id], float32(math.Inf(1)))
			}
		}); err != nil {
			return nil, nil, stats, err
		}
	} else {
		// Initialization is independent per center and therefore parallel.
		if err := parallelRowsSized(ctx, level.active.Min.Y, level.active.Max.Y, activeWidth, func(y int) {
			for x := level.active.Min.X; x < level.active.Max.X; x++ {
				id := y*level.w + x
				nnf[id] = pmPoint{x: -1, y: -1}
				costs[id] = float32(math.Inf(1))
				best, ok := pmInitialCandidate(level, seed, x, y)
				if !ok {
					continue
				}
				bestCost := pmPatchCost(level, &level.targetPlanes, x, y, best, float32(math.Inf(1)))

				// One deterministic local alternative prevents an ambiguous interior
				// from starting entirely from the same boundary-side hypothesis.
				state := pmHash(uint32(x), uint32(y), uint32(round+1))
				altRadius := level.searchRadius
				if seed != nil {
					altRadius = maxInt(16, level.searchRadius/2)
				}
				if alternative, ok := pmRandomValidNear(level, x, y, altRadius, &state); ok {
					candidateCost := pmPatchCost(level, &level.targetPlanes, x, y, alternative, bestCost)
					if candidateCost < bestCost {
						best, bestCost = alternative, candidateCost
					}
				}
				nnf[id], costs[id] = best, bestCost
			}
		}); err != nil {
			return nil, nil, stats, err
		}
	}

	if cap(level.rowChanges) < activeRows {
		level.rowChanges = make([]int, activeRows)
	} else {
		level.rowChanges = level.rowChanges[:activeRows]
	}

	for pass := 0; pass < iterations; pass++ {
		if err := ctx.Err(); err != nil {
			return nil, nil, stats, err
		}
		changes := 0

		// Classic in-place directional propagation. This stays sequential because
		// propagating a good displacement through a sweep is central to PatchMatch
		// quality. The more expensive random-search phase remains parallel.
		if pass&1 == 0 {
			for y := level.active.Min.Y; y < level.active.Max.Y; y++ {
				for x := level.active.Min.X; x < level.active.Max.X; x++ {
					id := y*level.w + x
					if x > level.active.Min.X {
						q := nnf[id-1]
						if pmTryCandidate(level, &level.targetPlanes, x, y, pmPoint{x: q.x + 1, y: q.y}, &nnf[id], &costs[id]) {
							changes++
						}
					}
					if y > level.active.Min.Y {
						q := nnf[id-level.w]
						if pmTryCandidate(level, &level.targetPlanes, x, y, pmPoint{x: q.x, y: q.y + 1}, &nnf[id], &costs[id]) {
							changes++
						}
					}
				}
			}
		} else {
			for y := level.active.Max.Y - 1; y >= level.active.Min.Y; y-- {
				for x := level.active.Max.X - 1; x >= level.active.Min.X; x-- {
					id := y*level.w + x
					if x+1 < level.active.Max.X {
						q := nnf[id+1]
						if pmTryCandidate(level, &level.targetPlanes, x, y, pmPoint{x: q.x - 1, y: q.y}, &nnf[id], &costs[id]) {
							changes++
						}
					}
					if y+1 < level.active.Max.Y {
						q := nnf[id+level.w]
						if pmTryCandidate(level, &level.targetPlanes, x, y, pmPoint{x: q.x, y: q.y - 1}, &nnf[id], &costs[id]) {
							changes++
						}
					}
				}
			}
		}

		startRadius := pmRandomSearchStartRadius(level.searchRadius, pass, round, seed != nil)
		clear(level.rowChanges)
		if err := parallelRowsSized(ctx, level.active.Min.Y, level.active.Max.Y, activeWidth, func(y int) {
			rowChanges := 0
			for x := level.active.Min.X; x < level.active.Max.X; x++ {
				id := y*level.w + x
				best := nnf[id]
				bestCost := costs[id]
				if !validPMPoint(level, best) {
					continue
				}
				state := pmHash(uint32(x), uint32(y), uint32(pass+1+round*iterations))
				for radius := startRadius; radius >= 1; radius /= 2 {
					state = pmNext(state)
					dx := int(state%uint32(2*radius+1)) - radius
					state = pmNext(state)
					dy := int(state%uint32(2*radius+1)) - radius
					candidate := pmPoint{x: best.x + int32(dx), y: best.y + int32(dy)}
					if pmTryCandidate(level, &level.targetPlanes, x, y, candidate, &best, &bestCost) {
						rowChanges++
					}
				}
				nnf[id], costs[id] = best, bestCost
			}
			level.rowChanges[y-level.active.Min.Y] = rowChanges
		}); err != nil {
			return nil, nil, stats, err
		}
		for _, n := range level.rowChanges {
			changes += n
		}

		stats.passes = pass + 1
		stats.lastChanges = changes
		// Require at least one forward and one reverse pass. Thereafter stop when
		// fewer than roughly 0.4% of active centers improve. This preserves hard
		// cases while avoiding two redundant passes on the common easy brush dab.
		stableThreshold := maxInt(2, stats.active/250)
		if pass >= 1 && changes <= stableThreshold {
			stats.stable = true
			break
		}
	}
	return nnf, costs, stats, nil
}

func pmRandomSearchStartRadius(searchRadius, pass, round int, haveSeed bool) int {
	radius := maxInt(1, searchRadius)
	if haveSeed {
		if round > 0 {
			radius = minInt(radius, maxInt(20, searchRadius/4))
		} else if pass > 0 {
			radius = minInt(radius, maxInt(28, searchRadius/2))
		}
	}
	if pass >= 2 {
		radius = minInt(radius, 32)
	}
	return maxInt(1, radius)
}

func pmInitialCandidate(level *pmLevel, seed *pmSolution, x, y int) (pmPoint, bool) {
	id := y*level.w + x
	// Clean target/source patches are already their own perfect local mapping.
	if level.valid[id] && level.mask.Pix[y*level.mask.Stride+x] == 0 {
		return pmPoint{x: int32(x), y: int32(y)}, true
	}
	if seed != nil && len(seed.nnf) != 0 {
		if candidate, ok := pmSeedFromSolution(level, seed, x, y); ok && validPMPoint(level, candidate) {
			return candidate, true
		}
	}
	if candidate, ok := pmNearbyValidSource(level, x, y); ok {
		return candidate, true
	}
	if len(level.sources) != 0 {
		// Extremely large/fully covered local holes may have no nearby legal
		// center. A deterministic source-list fallback is sufficient to bootstrap
		// random search without building a full-image Voronoi field.
		state := pmHash(uint32(x), uint32(y), 0x243f6a88)
		return level.sources[int(state%uint32(len(level.sources)))], true
	}
	return pmPoint{}, false
}

func pmNearbyValidSource(level *pmLevel, x, y int) (pmPoint, bool) {
	maxRadius := minInt(64, maxInt(level.w, level.h))
	for radius := 1; radius <= maxRadius; radius++ {
		left, right := x-radius, x+radius
		top, bottom := y-radius, y+radius
		for sx := left; sx <= right; sx++ {
			for _, sy := range [...]int{top, bottom} {
				p := pmPoint{x: int32(sx), y: int32(sy)}
				if validPMPoint(level, p) {
					return p, true
				}
			}
		}
		for sy := top + 1; sy < bottom; sy++ {
			for _, sx := range [...]int{left, right} {
				p := pmPoint{x: int32(sx), y: int32(sy)}
				if validPMPoint(level, p) {
					return p, true
				}
			}
		}
	}
	return pmPoint{}, false
}

// pmSeedFromSolution upsamples displacement, not absolute source coordinates.
// This preserves a constant motion field across odd/even child pixels and
// avoids the one-pixel checkerboard phase error produced by q_child=scale*q_parent.
func pmSeedFromSolution(level *pmLevel, seed *pmSolution, x, y int) (pmPoint, bool) {
	if seed == nil || seed.level == nil || len(seed.nnf) == 0 {
		return pmPoint{}, false
	}
	pw, ph := seed.level.w, seed.level.h
	if pw == level.w && ph == level.h {
		id := y*level.w + x
		if id < 0 || id >= len(seed.nnf) {
			return pmPoint{}, false
		}
		return seed.nnf[id], true
	}

	px := clampInt(int((float64(x)+0.5)*float64(pw)/float64(level.w)), 0, pw-1)
	py := clampInt(int((float64(y)+0.5)*float64(ph)/float64(level.h)), 0, ph-1)
	q := seed.nnf[py*pw+px]
	if q.x < 0 || q.y < 0 {
		return pmPoint{}, false
	}
	dx := float64(q.x) - float64(px)
	dy := float64(q.y) - float64(py)
	sx := float64(level.w) / float64(pw)
	sy := float64(level.h) / float64(ph)
	return pmPoint{
		x: int32(math.Round(float64(x) + dx*sx)),
		y: int32(math.Round(float64(y) + dy*sy)),
	}, true
}

func pmTryCandidate(level *pmLevel, target *pmPackedPlanes, tx, ty int, candidate pmPoint, best *pmPoint, bestCost *float32) bool {
	if !validPMPoint(level, candidate) {
		return false
	}
	cost := pmPatchCost(level, target, tx, ty, candidate, *bestCost)
	if cost < *bestCost {
		*best = candidate
		*bestCost = cost
		return true
	}
	return false
}

func pmRandomValidNear(level *pmLevel, tx, ty, radius int, state *uint32) (pmPoint, bool) {
	for attempt := 0; attempt < 12; attempt++ {
		*state = pmNext(*state)
		dx := int(*state%uint32(2*radius+1)) - radius
		*state = pmNext(*state)
		dy := int(*state%uint32(2*radius+1)) - radius
		candidate := pmPoint{x: int32(tx + dx), y: int32(ty + dy)}
		if validPMPoint(level, candidate) {
			return candidate, true
		}
	}
	return pmPoint{}, false
}

func validPMPoint(level *pmLevel, p pmPoint) bool {
	x, y := int(p.x), int(p.y)
	return x >= 0 && y >= 0 && x < level.w && y < level.h && level.valid[y*level.w+x]
}

func seedPMWorking(level *pmLevel, parent *pmSolution) *image.NRGBA {
	if parent == nil {
		// Masked content is ignored by round-zero confidence, so retaining source
		// bytes here is harmless and avoids inventing a second fill algorithm.
		return cloneNRGBA(level.src)
	}
	out := cloneNRGBA(level.src)
	bounds := maskBounds(level.mask)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			alpha := level.mask.Pix[y*level.mask.Stride+x]
			if alpha == 0 {
				continue
			}
			sample := pmBilinearParent(parent.working, level.w, level.h, x, y)
			si := y*level.src.Stride + x*4
			di := y*out.Stride + x*4
			a := int(alpha)
			for c := 0; c < 4; c++ {
				out.Pix[di+c] = byte((int(level.src.Pix[si+c])*(255-a) + int(sample[c])*a + 127) / 255)
			}
		}
	}
	return out
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

// buildPatchPyramid keeps two different mask semantics:
//   - targetMasks preserve fractional coverage for confidence and compositing;
//   - sourceMasks conservatively mark any covered fine pixel as unusable source.
func buildPatchPyramid(src *image.NRGBA, mask *image.Alpha, patchSize int) ([]*image.NRGBA, []*image.Alpha, []*image.Alpha) {
	images := []*image.NRGBA{src}
	targetMasks := []*image.Alpha{mask}
	sourceMasks := []*image.Alpha{binarySourceMask(mask)}
	minSide := maxInt(32, patchSize*4)
	const maxLevels = 7
	for len(images) < maxLevels {
		last := images[len(images)-1]
		w, h := last.Bounds().Dx(), last.Bounds().Dy()
		if minInt(w, h) <= minSide {
			break
		}
		nextW := maxInt(1, (w+1)/2)
		nextH := maxInt(1, (h+1)/2)
		images = append(images, downsampleNRGBA(last, nextW, nextH))
		targetMasks = append(targetMasks, downsampleTargetMask(targetMasks[len(targetMasks)-1], nextW, nextH))
		sourceMasks = append(sourceMasks, downsampleSourceMask(sourceMasks[len(sourceMasks)-1], nextW, nextH))
	}
	return images, targetMasks, sourceMasks
}

// downsampleNRGBA uses a separable binomial low-pass before resampling. This
// avoids aliasing halftone dots, thin typography, line art, and print grain into
// misleading coarse-level structures.
func downsampleNRGBA(src *image.NRGBA, w, h int) *image.NRGBA {
	sw, sh := src.Bounds().Dx(), src.Bounds().Dy()
	if sw == w && sh == h {
		return cloneNRGBA(src)
	}
	weights := [...]int{1, 4, 6, 4, 1}
	// Premultiplied working channels, scaled by 16 after horizontal filtering.
	tmp := make([]int64, sw*sh*4)
	for y := 0; y < sh; y++ {
		for x := 0; x < sw; x++ {
			var sum [4]int64
			for k := -2; k <= 2; k++ {
				sx := clampInt(x+k, 0, sw-1)
				i := y*src.Stride + sx*4
				a := int64(src.Pix[i+3])
				weight := int64(weights[k+2])
				sum[0] += int64(src.Pix[i]) * a * weight
				sum[1] += int64(src.Pix[i+1]) * a * weight
				sum[2] += int64(src.Pix[i+2]) * a * weight
				sum[3] += a * 255 * weight
			}
			base := (y*sw + x) * 4
			copy(tmp[base:base+4], sum[:])
		}
	}

	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		sy := clampInt(int(math.Round((float64(y)+0.5)*float64(sh)/float64(h)-0.5)), 0, sh-1)
		for x := 0; x < w; x++ {
			sx := clampInt(int(math.Round((float64(x)+0.5)*float64(sw)/float64(w)-0.5)), 0, sw-1)
			var sum [4]int64
			for k := -2; k <= 2; k++ {
				yy := clampInt(sy+k, 0, sh-1)
				base := (yy*sw + sx) * 4
				weight := int64(weights[k+2])
				for c := 0; c < 4; c++ {
					sum[c] += tmp[base+c] * weight
				}
			}
			di := y*out.Stride + x*4
			alphaNumerator := sum[3]
			if alphaNumerator > 0 {
				out.Pix[di] = byte(clampInt(int((sum[0]*255+alphaNumerator/2)/alphaNumerator), 0, 255))
				out.Pix[di+1] = byte(clampInt(int((sum[1]*255+alphaNumerator/2)/alphaNumerator), 0, 255))
				out.Pix[di+2] = byte(clampInt(int((sum[2]*255+alphaNumerator/2)/alphaNumerator), 0, 255))
			}
			// 256 is the total 2-D binomial weight; sum[3] contains alpha*255.
			out.Pix[di+3] = byte(clampInt(int((alphaNumerator+255*128)/(255*256)), 0, 255))
		}
	}
	return out
}

func binarySourceMask(src *image.Alpha) *image.Alpha {
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	out := image.NewAlpha(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if src.Pix[y*src.Stride+x] != 0 {
				out.Pix[y*out.Stride+x] = 255
			}
		}
	}
	return out
}

func downsampleTargetMask(src *image.Alpha, w, h int) *image.Alpha {
	out := image.NewAlpha(image.Rect(0, 0, w, h))
	sw, sh := src.Bounds().Dx(), src.Bounds().Dy()
	for y := 0; y < h; y++ {
		y0 := y * sh / h
		y1 := maxInt(y0+1, (y+1)*sh/h)
		for x := 0; x < w; x++ {
			x0 := x * sw / w
			x1 := maxInt(x0+1, (x+1)*sw/w)
			sum, count := 0, 0
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					sum += int(src.Pix[sy*src.Stride+sx])
					count++
				}
			}
			out.Pix[y*out.Stride+x] = byte((sum + count/2) / count)
		}
	}
	return out
}

func downsampleSourceMask(src *image.Alpha, w, h int) *image.Alpha {
	out := image.NewAlpha(image.Rect(0, 0, w, h))
	sw, sh := src.Bounds().Dx(), src.Bounds().Dy()
	for y := 0; y < h; y++ {
		y0 := y * sh / h
		y1 := maxInt(y0+1, (y+1)*sh/h)
		for x := 0; x < w; x++ {
			x0 := x * sw / w
			x1 := maxInt(x0+1, (x+1)*sw/w)
			covered := false
			for sy := y0; sy < y1 && !covered; sy++ {
				for sx := x0; sx < x1; sx++ {
					if src.Pix[sy*src.Stride+sx] != 0 {
						covered = true
						break
					}
				}
			}
			if covered {
				out.Pix[y*out.Stride+x] = 255
			}
		}
	}
	return out
}

func pmNearestValidSource(valid []bool, w, h int) []pmPoint {
	nearest := make([]pmPoint, w*h)
	distance := make([]int, w*h)
	queue := make([]int, 0, w*h/4)
	for id := range nearest {
		nearest[id] = pmPoint{x: -1, y: -1}
		distance[id] = -1
		if valid[id] {
			x, y := id%w, id/w
			nearest[id] = pmPoint{x: int32(x), y: int32(y)}
			distance[id] = 0
			queue = append(queue, id)
		}
	}
	for head := 0; head < len(queue); head++ {
		id := queue[head]
		x, y := id%w, id/w
		neighbors := [4]int{-1, -1, -1, -1}
		if x > 0 {
			neighbors[0] = id - 1
		}
		if x+1 < w {
			neighbors[1] = id + 1
		}
		if y > 0 {
			neighbors[2] = id - w
		}
		if y+1 < h {
			neighbors[3] = id + w
		}
		for _, next := range neighbors {
			if next < 0 || distance[next] >= 0 {
				continue
			}
			distance[next] = distance[id] + 1
			nearest[next] = nearest[id]
			queue = append(queue, next)
		}
	}
	return nearest
}

// pmMaskInteriorDistance returns distance (in pixels) from each fully/partly
// covered target pixel to known image content. It is used only to taper
// provisional EM confidence; it never changes source validity.
func pmMaskInteriorDistance(mask *image.Alpha) []int {
	w, h := mask.Bounds().Dx(), mask.Bounds().Dy()
	distance := make([]int, w*h)
	bounds := maskBounds(mask)
	if bounds.Empty() {
		return distance
	}
	queue := make([]int, 0, bounds.Dx()*bounds.Dy())

	// Only covered pixels participate. v3 enqueued every known pixel in the ROI,
	// making this tiny brush-distance calculation O(working-image area).
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if mask.Pix[y*mask.Stride+x] == 0 {
				continue
			}
			id := y*w + x
			distance[id] = -1
			boundary := mask.Pix[y*mask.Stride+x] < 255
			if !boundary {
				for _, d := range [...]image.Point{{X: -1}, {X: 1}, {Y: -1}, {Y: 1}} {
					nx, ny := x+d.X, y+d.Y
					if nx < 0 || ny < 0 || nx >= w || ny >= h || mask.Pix[ny*mask.Stride+nx] < 255 {
						boundary = true
						break
					}
				}
			}
			if boundary {
				distance[id] = 1
				queue = append(queue, id)
			}
		}
	}
	if len(queue) == 0 {
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				if mask.Pix[y*mask.Stride+x] != 0 {
					distance[y*w+x] = 1
				}
			}
		}
		return distance
	}
	for head := 0; head < len(queue); head++ {
		id := queue[head]
		x, y := id%w, id/w
		nextDistance := distance[id] + 1
		for _, d := range [...]image.Point{{X: -1}, {X: 1}, {Y: -1}, {Y: 1}} {
			nx, ny := x+d.X, y+d.Y
			if nx < bounds.Min.X || ny < bounds.Min.Y || nx >= bounds.Max.X || ny >= bounds.Max.Y {
				continue
			}
			nid := ny*w + nx
			if mask.Pix[ny*mask.Stride+nx] == 0 || distance[nid] >= 0 {
				continue
			}
			distance[nid] = nextDistance
			queue = append(queue, nid)
		}
	}
	return distance
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

func pmHash(x, y, salt uint32) uint32 {
	value := x*0x9e3779b1 ^ y*0x85ebca77 ^ salt*0xc2b2ae3d ^ 0x27d4eb2f
	value ^= value >> 16
	value *= 0x7feb352d
	value ^= value >> 15
	value *= 0x846ca68b
	return value ^ (value >> 16)
}

func pmNext(state uint32) uint32 { return state*1664525 + 1013904223 }

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

func parallelRows(ctx context.Context, start, end int, fn func(y int)) error {
	return parallelRowsSized(ctx, start, end, 256, fn)
}

// parallelRowsSized avoids goroutine setup on tiny brush regions and uses the
// existing row-parallel strategy only when there is enough work to amortize it.
func parallelRowsSized(ctx context.Context, start, end, width int, fn func(y int)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	rows := end - start
	if rows <= 0 {
		return nil
	}
	work := rows * maxInt(1, width)
	workers := minInt(runtime.GOMAXPROCS(0), rows)
	if workers <= 1 || work < 12000 || rows < 6 {
		for y := start; y < end; y++ {
			if err := ctx.Err(); err != nil {
				return err
			}
			fn(y)
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
