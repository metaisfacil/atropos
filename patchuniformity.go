package main

import (
	"context"
	"math"
)

// pmUpdateOccurrence builds a source-occurrence density from the current NNF.
// Only target patches that actually intersect the painted mask contribute.
// A coherent translation maps adjacent target centers to adjacent *distinct*
// source centers, producing density ~= 1. Reusing the same source neighborhood
// for several unrelated target regions drives density above 1.
func pmUpdateOccurrence(level *pmLevel, nnf []pmPoint) {
	if level == nil || level.uniformityStrength <= 0 || len(nnf) < level.w*level.h {
		level.occurrenceReady = false
		return
	}
	size := level.w * level.h
	if cap(level.occurrenceRaw) < size {
		level.occurrenceRaw = make([]float32, size)
	} else {
		level.occurrenceRaw = level.occurrenceRaw[:size]
		clear(level.occurrenceRaw)
	}
	for y := level.active.Min.Y; y < level.active.Max.Y; y++ {
		for x := level.active.Min.X; x < level.active.Max.X; x++ {
			if !pmPatchTouchesMask(level, x, y) {
				continue
			}
			id := y*level.w + x
			q := nnf[id]
			if !validPMPoint(level, q) {
				continue
			}
			level.occurrenceRaw[int(q.y)*level.w+int(q.x)] += 1
		}
	}

	stride := level.w + 1
	required := stride * (level.h + 1)
	if cap(level.occurrenceIntegral) < required {
		level.occurrenceIntegral = make([]float32, required)
	} else {
		level.occurrenceIntegral = level.occurrenceIntegral[:required]
		clear(level.occurrenceIntegral)
	}
	for y := 0; y < level.h; y++ {
		var row float32
		for x := 0; x < level.w; x++ {
			row += level.occurrenceRaw[y*level.w+x]
			level.occurrenceIntegral[(y+1)*stride+x+1] = level.occurrenceIntegral[y*stride+x+1] + row
		}
	}
	level.occurrenceReady = true
}

func pmOccurrencePenalty(level *pmLevel, source pmPoint, unknown float32) float32 {
	if level == nil || !level.occurrenceReady || level.uniformityStrength <= 0 || unknown <= 0.02 {
		return 0
	}
	x, y := int(source.x), int(source.y)
	if x < 0 || y < 0 || x >= level.w || y >= level.h {
		return 0
	}
	// 3x3 source-density neighborhood. A one-to-one translational mapping has
	// approximately one NNF center per source center and therefore ratio ~= 1.
	x0, x1 := maxInt(0, x-1), minInt(level.w, x+2)
	y0, y1 := maxInt(0, y-1), minInt(level.h, y+2)
	stride := level.w + 1
	sum := level.occurrenceIntegral[y1*stride+x1] - level.occurrenceIntegral[y0*stride+x1] -
		level.occurrenceIntegral[y1*stride+x0] + level.occurrenceIntegral[y0*stride+x0]
	area := float32((x1 - x0) * (y1 - y0))
	if area <= 0 {
		return 0
	}
	ratio := sum / area
	// Leave natural one-to-one mapping untouched. Pressure rises only when the
	// same source neighborhood is being used substantially more than once.
	excess := maxFloat32(0, ratio-1.12)
	if excess <= 0 {
		return 0
	}
	if excess > 2.2 {
		excess = 2.2
	}
	return level.uniformityStrength * unknown * unknown * 26 * excess * excess
}

func pmPatchTouchesMask(level *pmLevel, x, y int) bool {
	if level == nil || len(level.maskIntegral) == 0 {
		return false
	}
	h := level.half
	x0, y0 := x-h, y-h
	x1, y1 := x+h+1, y+h+1
	if x0 < 0 || y0 < 0 || x1 > level.w || y1 > level.h {
		return false
	}
	return integralRectSum(level.maskIntegral, level.w+1, x0, y0, x1, y1) != 0
}

func pmRecomputeActiveCosts(ctx context.Context, level *pmLevel, nnf []pmPoint, costs []float32) error {
	width := level.active.Dx()
	return parallelRowsSized(ctx, level.active.Min.Y, level.active.Max.Y, width, func(y int) {
		for x := level.active.Min.X; x < level.active.Max.X; x++ {
			id := y*level.w + x
			if id >= len(nnf) || !validPMPoint(level, nnf[id]) {
				costs[id] = float32(math.Inf(1))
				continue
			}
			costs[id] = pmPatchCost(level, &level.targetPlanes, x, y, nnf[id], float32(math.Inf(1)))
		}
	})
}

// pmOccurrencePressure reports whether meaningful source crowding remains. It
// is used only to avoid declaring convergence one pass too early when the NNF
// has stopped changing under appearance cost but is still visibly overusing a
// source neighborhood.
func pmOccurrencePressure(level *pmLevel) float32 {
	if level == nil || !level.occurrenceReady || len(level.occurrenceRaw) == 0 {
		return 0
	}
	maxPressure := float32(0)
	for y := level.active.Min.Y; y < level.active.Max.Y; y++ {
		for x := level.active.Min.X; x < level.active.Max.X; x++ {
			id := y*level.w + x
			if !pmPatchTouchesMask(level, x, y) || id >= len(level.nnf) || !validPMPoint(level, level.nnf[id]) {
				continue
			}
			q := level.nnf[id]
			p := pmOccurrencePenalty(level, q, 1)
			if p > maxPressure {
				maxPressure = p
			}
		}
	}
	return maxPressure
}

func pmOccurrencePenaltyForTarget(level *pmLevel, tx, ty int, source pmPoint) float32 {
	if level == nil {
		return 0
	}
	h := level.half
	confidenceSum := pmConfidenceRectSum(level, tx-h, ty-h, tx+h+1, ty+h+1)
	area := float32(level.patchSize * level.patchSize)
	unknown := 1 - minFloat32(1, maxFloat32(0, confidenceSum/(area+1e-6)))
	return pmOccurrencePenalty(level, source, unknown)
}

// pmCaptureOccurrenceCosts records the occurrence term currently embedded in
// each incumbent cost. This lets later frozen-field updates adjust only that
// cheap scalar term instead of re-running full SSD/structure/photo comparison
// for every active center.
func pmCaptureOccurrenceCosts(level *pmLevel, nnf []pmPoint) {
	size := level.w * level.h
	if cap(level.occurrenceCost) < size {
		level.occurrenceCost = make([]float32, size)
	} else {
		level.occurrenceCost = level.occurrenceCost[:size]
		clear(level.occurrenceCost)
	}
	if !level.occurrenceReady {
		return
	}
	for y := level.active.Min.Y; y < level.active.Max.Y; y++ {
		for x := level.active.Min.X; x < level.active.Max.X; x++ {
			id := y*level.w + x
			if id < len(nnf) && validPMPoint(level, nnf[id]) {
				level.occurrenceCost[id] = pmOccurrencePenaltyForTarget(level, x, y, nnf[id])
			}
		}
	}
}

func pmRefreshOccurrenceCosts(level *pmLevel, nnf []pmPoint, costs []float32) {
	if len(level.occurrenceCost) < level.w*level.h {
		pmCaptureOccurrenceCosts(level, nnf)
		return
	}
	for y := level.active.Min.Y; y < level.active.Max.Y; y++ {
		for x := level.active.Min.X; x < level.active.Max.X; x++ {
			id := y*level.w + x
			if id >= len(nnf) || id >= len(costs) || !validPMPoint(level, nnf[id]) || float32IsInf(costs[id]) {
				continue
			}
			oldPenalty := level.occurrenceCost[id]
			newPenalty := pmOccurrencePenaltyForTarget(level, x, y, nnf[id])
			costs[id] = maxFloat32(0, costs[id]-oldPenalty+newPenalty)
			level.occurrenceCost[id] = newPenalty
		}
	}
}
