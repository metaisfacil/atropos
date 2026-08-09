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

// PatchMatchFill fills mask from patches elsewhere in src.
//
// The implementation follows the same broad shape as production content-aware
// fill engines: a coarse-to-fine image pyramid, a persistent nearest-neighbour
// field (NNF), propagation/random search, progressive confidence, coherent
// displacement voting, and frequency-separated reconstruction.
func PatchMatchFill(ctx context.Context, src *image.NRGBA, mask *image.Alpha, patchSize, iterations int) (*image.NRGBA, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if src == nil {
		return nil, errors.New("PatchMatch: nil source image")
	}

	source := normalizeNRGBA(src)
	w, h := source.Bounds().Dx(), source.Bounds().Dy()
	if w == 0 || h == 0 {
		return source, nil
	}
	fillMask := normalizeAlpha(mask, w, h)
	if maskBounds(fillMask).Empty() {
		return source, nil
	}

	if patchSize < 3 {
		patchSize = 3
	}
	if patchSize%2 == 0 {
		patchSize++
	}
	maxPatch := minInt(w, h)
	if maxPatch%2 == 0 {
		maxPatch--
	}
	if maxPatch < 1 {
		return source, nil
	}
	if patchSize > maxPatch {
		patchSize = maxPatch
	}
	if iterations < 1 {
		iterations = 1
	}

	srcPyramid, maskPyramid := buildPatchPyramid(source, fillMask, patchSize)
	var parent *pmSolution

	for levelIndex := len(srcPyramid) - 1; levelIndex >= 0; levelIndex-- {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		level := preparePMLevel(srcPyramid[levelIndex], maskPyramid[levelIndex], patchSize)
		if len(level.sources) == 0 {
			// A tiny coarse level can lose every legal source patch. The next
			// finer level still has useful source data, so simply start there.
			continue
		}

		working := seedPMWorking(level, parent)
		// A second EM round lets the first coherent reconstruction become a
		// progressively trusted target. This is essential for carrying lines
		// and texture through pixels whose initial patch was entirely masked.
		rounds := 2

		var nnf []pmPoint
		var costs []float32
		var targetStats []pmPatchStats
		var err error
		for round := 0; round < rounds; round++ {
			nnf, costs, targetStats, err = solvePMLevel(ctx, level, working, parent, iterations, round)
			if err != nil {
				return nil, err
			}
			working, err = reconstructPMLevel(ctx, level, working, nnf, costs, targetStats)
			if err != nil {
				return nil, err
			}
			// Once reconstruction has produced an estimate, refine against it
			// rather than repeatedly consulting the coarser NNF.
			parent = nil
		}

		parent = &pmSolution{
			level:   level,
			working: working,
			nnf:     nnf,
		}
	}

	if parent == nil {
		// Preserve the original image when the mask covers every possible
		// source patch. There is no defensible content-aware estimate to make.
		return source, nil
	}
	return parent.working, nil
}

type pmPoint struct {
	x int32
	y int32
}

type pmFeature struct {
	gx float32
	gy float32
}

type pmLevel struct {
	src           *image.NRGBA
	srcBase       *image.NRGBA
	mask          *image.Alpha
	w             int
	h             int
	patchSize     int
	half          int
	active        image.Rectangle
	valid         []bool
	sources       []pmPoint
	srcStats      []pmPatchStats
	targetStats   []pmPatchStats
	statsScratch  []float32
	targetPlanes  pmPackedPlanes
	targetFeature []pmFeature
	srcPlanes     pmPackedPlanes
	confidence    []float32
	confStride    int
	confSum       []float32
}

type pmSolution struct {
	level   *pmLevel
	working *image.NRGBA
	nnf     []pmPoint
}

const pmVoteClusterCount = 64

// pmVoteCluster groups overlapping patch votes by their source displacement.
// PatchMatch propagation naturally produces runs of nearly identical offsets;
// treating those runs as one hypothesis prevents unrelated source textures
// from being averaged together during reconstruction.
type pmVoteCluster struct {
	keyX, keyY int32
	bestDX     int32
	bestDY     int32
	used       bool
	count      int
	support    float32
	bestWeight float32
	bestGain   float32
	bestBias   [3]float32
	texture    float32
	sumAlpha   float32
}

func pmVoteClusterKey(value int32) int32 {
	// A two-pixel bucket tolerates the small displacement drift produced by
	// neighboring patch searches without merging visibly different mappings.
	if value >= 0 {
		return value / 2
	}
	return -((-value + 1) / 2)
}

func pmFindVoteCluster(clusters *[pmVoteClusterCount]pmVoteCluster, dx, dy int32) *pmVoteCluster {
	keyX, keyY := pmVoteClusterKey(dx), pmVoteClusterKey(dy)
	hash := uint32(keyX)*0x9e3779b1 ^ uint32(keyY)*0x85ebca77
	for probe := 0; probe < len(clusters); probe++ {
		cluster := &clusters[(int(hash)+probe)&(len(clusters)-1)]
		if !cluster.used {
			cluster.used = true
			cluster.keyX = keyX
			cluster.keyY = keyY
			cluster.bestDX = dx
			cluster.bestDY = dy
			return cluster
		}
		if cluster.keyX == keyX && cluster.keyY == keyY {
			return cluster
		}
	}
	return nil
}

func preparePMLevel(src *image.NRGBA, mask *image.Alpha, patchSize int) *pmLevel {
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	if patchSize > minInt(w, h) {
		patchSize = minInt(w, h)
		if patchSize%2 == 0 {
			patchSize--
		}
	}
	if patchSize < 1 {
		patchSize = 1
	}
	half := patchSize / 2
	srcPlanes := packPMPixels(src)
	srcFeature := buildPMFeaturesPacked(&srcPlanes, w, h, nil)
	level := &pmLevel{
		src:       src,
		srcBase:   pmLowPassNRGBA(src),
		mask:      mask,
		w:         w,
		h:         h,
		patchSize: patchSize,
		half:      half,
		valid:     make([]bool, w*h),
		srcPlanes: srcPlanes,
	}
	level.srcStats, level.statsScratch = buildPMPatchStats(src, srcFeature, nil, 0, patchSize, nil, nil)
	level.confidence, level.confStride, level.confSum = packPMConfidence(mask)

	integral := maskedIntegral(mask)
	for y := half; y < h-half; y++ {
		for x := half; x < w-half; x++ {
			if integralRectSum(integral, w+1, x-half, y-half, x+half+1, y+half+1) == 0 {
				level.valid[y*w+x] = true
				level.sources = append(level.sources, pmPoint{int32(x), int32(y)})
			}
		}
	}

	// At very coarse levels, max-pooling the mask can make a full patch-free
	// source impossible. Allow unmasked centers there; finer levels restore the
	// strict full-patch constraint.
	if len(level.sources) == 0 {
		for y := half; y < h-half; y++ {
			for x := half; x < w-half; x++ {
				if mask.Pix[y*mask.Stride+x] == 0 {
					level.valid[y*w+x] = true
					level.sources = append(level.sources, pmPoint{int32(x), int32(y)})
				}
			}
		}
	}

	bounds := maskBounds(mask)
	if !bounds.Empty() {
		padding := patchSize * 2
		level.active = image.Rect(
			maxInt(half, bounds.Min.X-padding),
			maxInt(half, bounds.Min.Y-padding),
			minInt(w-half, bounds.Max.X+padding),
			minInt(h-half, bounds.Max.Y+padding),
		)
	}
	return level
}

func solvePMLevel(ctx context.Context, level *pmLevel, working *image.NRGBA, parent *pmSolution, iterations, round int) ([]pmPoint, []float32, []pmPatchStats, error) {
	size := level.w * level.h
	current := make([]pmPoint, size)
	next := make([]pmPoint, size)
	currentCost := make([]float32, size)
	nextCost := make([]float32, size)
	acceptedConfidence := float32(0)
	if parent != nil {
		acceptedConfidence = 0.15
	} else if round > 0 {
		acceptedConfidence = 0.30
	}
	updatePMConfidence(level.mask, level.confidence, level.confStride, level.confSum, acceptedConfidence)
	level.targetPlanes = packPMPixelsInto(working, level.targetPlanes)
	targetPlanes := &level.targetPlanes
	level.targetFeature = buildPMFeaturesPacked(targetPlanes, level.w, level.h, level.targetFeature)
	level.targetStats, level.statsScratch = buildPMPatchStats(working, level.targetFeature, level.confidence, level.confStride, level.patchSize, level.targetStats, level.statsScratch)
	targetStats := level.targetStats

	err := parallelRows(ctx, level.active.Min.Y, level.active.Max.Y, func(y int) {
		for x := level.active.Min.X; x < level.active.Max.X; x++ {
			id := y*level.w + x
			var candidate pmPoint
			switch {
			case level.valid[id]:
				candidate = pmPoint{int32(x), int32(y)}
			case parent != nil && len(parent.nnf) != 0:
				px := minInt(parent.level.w-1, x*parent.level.w/level.w)
				py := minInt(parent.level.h-1, y*parent.level.h/level.h)
				coarse := parent.nnf[py*parent.level.w+px]
				candidate = pmPoint{
					x: int32(int(coarse.x) * level.w / parent.level.w),
					y: int32(int(coarse.y) * level.h / parent.level.h),
				}
				if !validPMPoint(level, candidate) {
					candidate = hashedPMSource(level, x, y, round)
				}
			default:
				candidate = hashedPMSource(level, x, y, round)
			}
			current[id] = candidate
			currentCost[id] = pmPatchCost(level, targetPlanes, targetStats, x, y, candidate, float32(math.Inf(1)))
		}
	})
	if err != nil {
		return nil, nil, nil, err
	}

	for pass := 0; pass < iterations; pass++ {
		if err := ctx.Err(); err != nil {
			return nil, nil, nil, err
		}
		err = parallelRows(ctx, level.active.Min.Y, level.active.Max.Y, func(y int) {
			for x := level.active.Min.X; x < level.active.Max.X; x++ {
				if x&15 == 0 && ctx.Err() != nil {
					return
				}
				id := y*level.w + x
				best := current[id]
				bestCost := currentCost[id]

				try := func(candidate pmPoint) {
					if !validPMPoint(level, candidate) {
						return
					}
					cost := pmPatchCost(level, targetPlanes, targetStats, x, y, candidate, bestCost)
					if cost < bestCost {
						best = candidate
						bestCost = cost
					}
				}

				// Jacobi propagation makes the entire pass safe to parallelize.
				// Translating the neighbour's match preserves its displacement.
				if x > level.active.Min.X {
					q := current[id-1]
					try(pmPoint{q.x + 1, q.y})
				}
				if x+1 < level.active.Max.X {
					q := current[id+1]
					try(pmPoint{q.x - 1, q.y})
				}
				if y > level.active.Min.Y {
					q := current[id-level.w]
					try(pmPoint{q.x, q.y + 1})
				}
				if y+1 < level.active.Max.Y {
					q := current[id+level.w]
					try(pmPoint{q.x, q.y - 1})
				}

				state := pmHash(uint32(x), uint32(y), uint32(pass+round*iterations))
				for radius := maxInt(level.w, level.h); radius >= 1; radius /= 2 {
					state = pmNext(state)
					dx := int(state%uint32(2*radius+1)) - radius
					state = pmNext(state)
					dy := int(state%uint32(2*radius+1)) - radius
					try(pmPoint{best.x + int32(dx), best.y + int32(dy)})

					// A global proposal prevents a large hole from becoming
					// trapped in a locally coherent but semantically poor basin.
					state = pmNext(state)
					try(level.sources[int(state%uint32(len(level.sources)))])
				}

				next[id] = best
				nextCost[id] = bestCost
			}
		})
		if err != nil {
			return nil, nil, nil, err
		}
		current, next = next, current
		currentCost, nextCost = nextCost, currentCost
	}
	return current, currentCost, targetStats, nil
}

func reconstructPMLevel(ctx context.Context, level *pmLevel, previous *image.NRGBA, nnf []pmPoint, costs []float32, targetStats []pmPatchStats) (*image.NRGBA, error) {
	out := cloneNRGBA(previous)
	spatial := make([]float32, level.patchSize)
	for i := range spatial {
		d := float32(i - level.half)
		spatial[i] = 1 / (1 + d*d*0.18)
	}

	err := parallelRows(ctx, 0, level.h, func(y int) {
		for x := 0; x < level.w; x++ {
			if x&15 == 0 && ctx.Err() != nil {
				return
			}
			maskAlpha := level.mask.Pix[y*level.mask.Stride+x]
			if maskAlpha == 0 {
				continue
			}

			minCX := maxInt(level.active.Min.X, x-level.half)
			maxCX := minInt(level.active.Max.X-1, x+level.half)
			minCY := maxInt(level.active.Min.Y, y-level.half)
			maxCY := minInt(level.active.Max.Y-1, y+level.half)

			var clusters [pmVoteClusterCount]pmVoteCluster
			var baseR, baseG, baseB, baseAlpha, baseWeight float32
			for cy := minCY; cy <= maxCY; cy++ {
				for cx := minCX; cx <= maxCX; cx++ {
					id := cy*level.w + cx
					match := nnf[id]
					sx := int(match.x) + x - cx
					sy := int(match.y) + y - cy
					if sx < 0 || sy < 0 || sx >= level.w || sy >= level.h {
						continue
					}
					weight := spatial[x-cx+level.half] * spatial[y-cy+level.half]
					weight /= 1 + costs[id]/256
					cluster := pmFindVoteCluster(&clusters, match.x-int32(cx), match.y-int32(cy))
					if cluster == nil {
						continue
					}
					si := sy*level.src.Stride + sx*4
					alpha := float32(level.src.Pix[si+3])
					alphaWeight := weight * alpha
					gain, bias := pmGainBias(targetStats[id], level.srcStats[int(match.y)*level.w+int(match.x)])
					cluster.count++
					cluster.support += weight
					cluster.sumAlpha += alphaWeight
					bi := sy*level.srcBase.Stride + sx*4
					adjustedBaseR := gain*float32(level.srcBase.Pix[bi]) + bias[0]
					adjustedBaseG := gain*float32(level.srcBase.Pix[bi+1]) + bias[1]
					adjustedBaseB := gain*float32(level.srcBase.Pix[bi+2]) + bias[2]
					baseR += alphaWeight * adjustedBaseR
					baseG += alphaWeight * adjustedBaseG
					baseB += alphaWeight * adjustedBaseB
					baseAlpha += alphaWeight
					baseWeight += weight
					if weight > cluster.bestWeight {
						cluster.bestWeight = weight
						cluster.bestDX = match.x - int32(cx)
						cluster.bestDY = match.y - int32(cy)
						cluster.bestGain = gain
						cluster.bestBias = bias
						cluster.texture = level.srcStats[int(match.y)*level.w+int(match.x)].gradientEnergy
					}
				}
			}

			var dominant *pmVoteCluster
			for i := range clusters {
				cluster := &clusters[i]
				if !cluster.used || cluster.sumAlpha == 0 {
					continue
				}
				// Support selects a displacement shared by many overlapping
				// patches. The small count term breaks close contests in favor
				// of the more spatially coherent mapping.
				score := cluster.support * (1 + 0.03*float32(cluster.count))
				if dominant == nil || score > dominant.support*(1+0.03*float32(dominant.count)) {
					dominant = cluster
				}
			}

			var fillR, fillG, fillB, fillA float32
			if dominant != nil && baseWeight > 0 && baseAlpha > 0 {
				// Low frequencies can be safely averaged across plausible votes;
				// doing so gives a stable tonal estimate. High-frequency residual
				// comes from one coherent mapping so grain and fine scratches do
				// not cancel each other out.
				detailX := clampInt(x+int(dominant.bestDX), 0, level.w-1)
				detailY := clampInt(y+int(dominant.bestDY), 0, level.h-1)
				chosenGain := dominant.bestGain
				chosenBias := dominant.bestBias
				texture := dominant.texture
				detailIndex := detailY*level.src.Stride + detailX*4
				baseIndex := detailY*level.srcBase.Stride + detailX*4
				detailGain := minFloat32(1.15, maxFloat32(0.85, chosenGain))
				structureBlend := minFloat32(1, maxFloat32(0, (texture-2)/12))
				globalBaseR, globalBaseG, globalBaseB := baseR/baseAlpha, baseG/baseAlpha, baseB/baseAlpha
				dominantBaseR := chosenGain*float32(level.srcBase.Pix[baseIndex]) + chosenBias[0]
				dominantBaseG := chosenGain*float32(level.srcBase.Pix[baseIndex+1]) + chosenBias[1]
				dominantBaseB := chosenGain*float32(level.srcBase.Pix[baseIndex+2]) + chosenBias[2]
				fillR = globalBaseR*(1-structureBlend) + dominantBaseR*structureBlend + detailGain*(float32(level.src.Pix[detailIndex])-float32(level.srcBase.Pix[baseIndex]))
				fillG = globalBaseG*(1-structureBlend) + dominantBaseG*structureBlend + detailGain*(float32(level.src.Pix[detailIndex+1])-float32(level.srcBase.Pix[baseIndex+1]))
				fillB = globalBaseB*(1-structureBlend) + dominantBaseB*structureBlend + detailGain*(float32(level.src.Pix[detailIndex+2])-float32(level.srcBase.Pix[baseIndex+2]))
				fillA = baseAlpha / baseWeight
			} else {
				fallback := hashedPMSource(level, x, y, 0)
				si := int(fallback.y)*level.src.Stride + int(fallback.x)*4
				fillR = float32(level.src.Pix[si])
				fillG = float32(level.src.Pix[si+1])
				fillB = float32(level.src.Pix[si+2])
				fillA = float32(level.src.Pix[si+3])
			}

			si := y*level.src.Stride + x*4
			oi := y*out.Stride + x*4
			a := float32(maskAlpha) / 255
			invA := 1 - a
			out.Pix[oi] = byte(clampFloat32(float32(level.src.Pix[si])*invA + fillR*a))
			out.Pix[oi+1] = byte(clampFloat32(float32(level.src.Pix[si+1])*invA + fillG*a))
			out.Pix[oi+2] = byte(clampFloat32(float32(level.src.Pix[si+2])*invA + fillB*a))
			out.Pix[oi+3] = byte(clampFloat32(float32(level.src.Pix[si+3])*invA + fillA*a))
		}
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func seedPMWorking(level *pmLevel, parent *pmSolution) *image.NRGBA {
	if parent == nil {
		return pmHealedWorking(level.src, level.mask)
	}
	out := cloneNRGBA(level.src)
	weights := [...]int{1, 4, 6, 4, 1}
	for y := 0; y < level.h; y++ {
		for x := 0; x < level.w; x++ {
			maskAlpha := level.mask.Pix[y*level.mask.Stride+x]
			if maskAlpha == 0 {
				continue
			}
			px := (2*x + 1) * parent.level.w / (2 * level.w)
			py := (2*y + 1) * parent.level.h / (2 * level.h)
			var rgba [4]int
			var total int
			for ky := -2; ky <= 2; ky++ {
				sy := clampInt(py+ky, 0, parent.level.h-1)
				for kx := -2; kx <= 2; kx++ {
					sx := clampInt(px+kx, 0, parent.level.w-1)
					weight := weights[ky+2] * weights[kx+2]
					i := sy*parent.working.Stride + sx*4
					for channel := 0; channel < 4; channel++ {
						rgba[channel] += int(parent.working.Pix[i+channel]) * weight
					}
					total += weight
				}
			}
			si := y*level.src.Stride + x*4
			oi := y*out.Stride + x*4
			a := int(maskAlpha)
			for channel := 0; channel < 4; channel++ {
				estimate := rgba[channel] / total
				out.Pix[oi+channel] = byte((int(level.src.Pix[si+channel])*(255-a) + estimate*a + 127) / 255)
			}
		}
	}
	return out
}

func buildPatchPyramid(src *image.NRGBA, mask *image.Alpha, patchSize int) ([]*image.NRGBA, []*image.Alpha) {
	images := []*image.NRGBA{src}
	masks := []*image.Alpha{mask}
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
		masks = append(masks, downsampleMask(masks[len(masks)-1], nextW, nextH))
	}
	return images, masks
}

func downsampleNRGBA(src *image.NRGBA, w, h int) *image.NRGBA {
	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	srcW, srcH := src.Bounds().Dx(), src.Bounds().Dy()
	for y := 0; y < h; y++ {
		y0 := y * srcH / h
		y1 := maxInt(y0+1, (y+1)*srcH/h)
		for x := 0; x < w; x++ {
			x0 := x * srcW / w
			x1 := maxInt(x0+1, (x+1)*srcW/w)
			var premulR, premulG, premulB, alpha, count int
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					i := sy*src.Stride + sx*4
					a := int(src.Pix[i+3])
					premulR += int(src.Pix[i]) * a
					premulG += int(src.Pix[i+1]) * a
					premulB += int(src.Pix[i+2]) * a
					alpha += a
					count++
				}
			}
			i := y*out.Stride + x*4
			if alpha > 0 {
				out.Pix[i] = byte(premulR / alpha)
				out.Pix[i+1] = byte(premulG / alpha)
				out.Pix[i+2] = byte(premulB / alpha)
			}
			out.Pix[i+3] = byte(alpha / count)
		}
	}
	return out
}

// pmLowPassNRGBA returns the base layer used for frequency-separated voting.
// The separable [1 4 6 4 1] kernel removes pixel-scale texture while retaining
// the gradients that should blend smoothly across an inpainted region.
func pmLowPassNRGBA(src *image.NRGBA) *image.NRGBA {
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	if w == 0 || h == 0 {
		return out
	}
	weights := [...]int{1, 4, 6, 4, 1}
	tmp := make([]uint16, w*h*4)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			for channel := 0; channel < 4; channel++ {
				sum := 0
				for k := -2; k <= 2; k++ {
					sx := clampInt(x+k, 0, w-1)
					sum += weights[k+2] * int(src.Pix[y*src.Stride+sx*4+channel])
				}
				tmp[(y*w+x)*4+channel] = uint16(sum)
			}
		}
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			for channel := 0; channel < 4; channel++ {
				sum := 0
				for k := -2; k <= 2; k++ {
					sy := clampInt(y+k, 0, h-1)
					sum += weights[k+2] * int(tmp[(sy*w+x)*4+channel])
				}
				out.Pix[y*out.Stride+x*4+channel] = byte((sum + 128) / 256)
			}
		}
	}
	return out
}

func downsampleMask(src *image.Alpha, w, h int) *image.Alpha {
	out := image.NewAlpha(image.Rect(0, 0, w, h))
	srcW, srcH := src.Bounds().Dx(), src.Bounds().Dy()
	for y := 0; y < h; y++ {
		y0 := y * srcH / h
		y1 := maxInt(y0+1, (y+1)*srcH/h)
		for x := 0; x < w; x++ {
			x0 := x * srcW / w
			x1 := maxInt(x0+1, (x+1)*srcW/w)
			var maximum byte
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					if value := src.Pix[sy*src.Stride+sx]; value > maximum {
						maximum = value
					}
				}
			}
			out.Pix[y*out.Stride+x] = maximum
		}
	}
	return out
}

func maskedIntegral(mask *image.Alpha) []int {
	w, h := mask.Bounds().Dx(), mask.Bounds().Dy()
	integral := make([]int, (w+1)*(h+1))
	for y := 0; y < h; y++ {
		rowSum := 0
		for x := 0; x < w; x++ {
			if mask.Pix[y*mask.Stride+x] != 0 {
				rowSum++
			}
			integral[(y+1)*(w+1)+x+1] = integral[y*(w+1)+x+1] + rowSum
		}
	}
	return integral
}

func integralRectSum(integral []int, stride, x0, y0, x1, y1 int) int {
	return integral[y1*stride+x1] - integral[y0*stride+x1] -
		integral[y1*stride+x0] + integral[y0*stride+x0]
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

func validPMPoint(level *pmLevel, p pmPoint) bool {
	x, y := int(p.x), int(p.y)
	return x >= 0 && y >= 0 && x < level.w && y < level.h && level.valid[y*level.w+x]
}

func hashedPMSource(level *pmLevel, x, y, salt int) pmPoint {
	state := pmHash(uint32(x), uint32(y), uint32(salt))
	return level.sources[int(state%uint32(len(level.sources)))]
}

func pmHash(x, y, salt uint32) uint32 {
	value := x*0x9e3779b1 ^ y*0x85ebca77 ^ salt*0xc2b2ae3d ^ 0x27d4eb2f
	value ^= value >> 16
	value *= 0x7feb352d
	value ^= value >> 15
	value *= 0x846ca68b
	return value ^ (value >> 16)
}

func pmNext(state uint32) uint32 {
	return state*1664525 + 1013904223
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

func parallelRows(ctx context.Context, start, end int, fn func(y int)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	rows := end - start
	if rows <= 0 {
		return nil
	}
	workers := minInt(runtime.GOMAXPROCS(0), rows)
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
