package main

import (
	"context"
	"math"
)

type pmRegionInfo struct {
	size         int
	weight       float32
	sumDX, sumDY float32
	repDX, repDY int32
	bestNeighbor int32
}

// pmRegularizeCoherentRegions turns the purely local coherence idea into a
// connected NNF segmentation. It is deliberately conservative: large coherent
// regions are allowed to absorb one-pixel jitter and tiny islands only when the
// region hypothesis remains close in PatchMatch cost.
func pmRegularizeCoherentRegions(ctx context.Context, level *pmLevel, nnf []pmPoint, costs []float32) (bool, error) {
	if level == nil || !level.regionEnabled || level.active.Empty() || len(nnf) < level.w*level.h {
		return false, nil
	}
	pmPreparePhotoTransforms(level, nnf)
	regions := pmSegmentCoherentRegions(level, nnf, costs)
	if len(regions) == 0 {
		return false, nil
	}

	changed := false
	// First collapse +/-1-pixel jitter inside established regions toward their
	// weighted representative, but only if the exact correspondence is almost as
	// good photometrically/structurally as the current member.
	for y := level.active.Min.Y; y < level.active.Max.Y; y++ {
		if err := ctx.Err(); err != nil {
			return changed, err
		}
		for x := level.active.Min.X; x < level.active.Max.X; x++ {
			id := y*level.w + x
			if id >= len(level.regionIDs) || level.regionIDs[id] < 0 || !pmPatchTouchesMask(level, x, y) {
				continue
			}
			rid := int(level.regionIDs[id])
			if rid >= len(regions) || regions[rid].size < 4 {
				continue
			}
			cur := nnf[id]
			cdx, cdy := cur.x-int32(x), cur.y-int32(y)
			repDX, repDY := regions[rid].repDX, regions[rid].repDY
			if cdx == repDX && cdy == repDY || absInt(int(cdx-repDX)) > 1 || absInt(int(cdy-repDY)) > 1 {
				continue
			}
			candidate := pmPoint{x: int32(x) + repDX, y: int32(y) + repDY}
			if !validPMPoint(level, candidate) {
				continue
			}
			candidateCost := pmPatchCost(level, &level.targetPlanes, x, y, candidate, float32(math.Inf(1)))
			if candidateCost <= costs[id]*1.045+0.35 {
				nnf[id], costs[id] = candidate, candidateCost
				changed = true
			}
		}
	}

	if changed {
		pmPreparePhotoTransforms(level, nnf)
		regions = pmSegmentCoherentRegions(level, nnf, costs)
	}

	// Then offer tiny isolated components the representative displacement of a
	// substantially larger touching region. This removes single-patch NNF islands
	// without forcing genuinely different large mappings to merge.
	maxTiny := maxInt(3, level.patchSize/2)
	for rid := range regions {
		region := &regions[rid]
		if region.size == 0 || region.size > maxTiny || region.bestNeighbor < 0 || int(region.bestNeighbor) >= len(regions) {
			continue
		}
		n := &regions[region.bestNeighbor]
		if n.size < maxInt(6, region.size*3) {
			continue
		}
		for y := level.active.Min.Y; y < level.active.Max.Y; y++ {
			for x := level.active.Min.X; x < level.active.Max.X; x++ {
				id := y*level.w + x
				if id >= len(level.regionIDs) || level.regionIDs[id] != int32(rid) {
					continue
				}
				candidate := pmPoint{x: int32(x) + n.repDX, y: int32(y) + n.repDY}
				if !validPMPoint(level, candidate) {
					continue
				}
				candidateCost := pmPatchCost(level, &level.targetPlanes, x, y, candidate, float32(math.Inf(1)))
				if candidateCost <= costs[id]*1.12+1.25 {
					nnf[id], costs[id] = candidate, candidateCost
					changed = true
				}
			}
		}
	}

	if changed {
		pmPreparePhotoTransforms(level, nnf)
		regions = pmSegmentCoherentRegions(level, nnf, costs)
	}
	pmWriteRegionConfidence(level, regions)
	return changed, nil
}

func pmSegmentCoherentRegions(level *pmLevel, nnf []pmPoint, costs []float32) []pmRegionInfo {
	size := level.w * level.h
	if cap(level.regionIDs) < size {
		level.regionIDs = make([]int32, size)
	} else {
		level.regionIDs = level.regionIDs[:size]
	}
	for i := range level.regionIDs {
		level.regionIDs[i] = -1
	}
	queue := level.regionQueue[:0]
	regions := make([]pmRegionInfo, 0, 32)
	neighbors := [...]pmPoint{{x: -1}, {x: 1}, {y: -1}, {y: 1}}

	for y := level.active.Min.Y; y < level.active.Max.Y; y++ {
		for x := level.active.Min.X; x < level.active.Max.X; x++ {
			startID := y*level.w + x
			if level.regionIDs[startID] >= 0 || !pmPatchTouchesMask(level, x, y) || !validPMPoint(level, nnf[startID]) {
				continue
			}
			rid := int32(len(regions))
			regions = append(regions, pmRegionInfo{bestNeighbor: -1})
			level.regionIDs[startID] = rid
			queue = append(queue, startID)
			for head := 0; head < len(queue); head++ {
				id := queue[head]
				cx, cy := id%level.w, id/level.w
				q := nnf[id]
				dx, dy := q.x-int32(cx), q.y-int32(cy)
				weight := float32(1)
				if id < len(costs) && !float32IsInf(costs[id]) {
					weight = 1 / (1 + costs[id]/256)
				}
				regions[rid].size++
				regions[rid].weight += weight
				regions[rid].sumDX += weight * float32(dx)
				regions[rid].sumDY += weight * float32(dy)

				for _, d := range neighbors {
					nx, ny := cx+int(d.x), cy+int(d.y)
					if nx < level.active.Min.X || ny < level.active.Min.Y || nx >= level.active.Max.X || ny >= level.active.Max.Y {
						continue
					}
					nid := ny*level.w + nx
					if level.regionIDs[nid] >= 0 || !pmPatchTouchesMask(level, nx, ny) || !validPMPoint(level, nnf[nid]) {
						continue
					}
					nq := nnf[nid]
					ndx, ndy := nq.x-int32(nx), nq.y-int32(ny)
					if absInt(int(ndx-dx)) > 1 || absInt(int(ndy-dy)) > 1 {
						continue
					}
					// Separate NNF/gain-bias segment concepts. A large photometric
					// discontinuity prevents two otherwise similar displacements
					// from being declared one region.
					if id < len(level.photo) && nid < len(level.photo) && !pmPhotoCompatible(level.photo[id], level.photo[nid]) {
						continue
					}
					level.regionIDs[nid] = rid
					queue = append(queue, nid)
				}
			}
			queue = queue[:0]
		}
	}
	level.regionQueue = queue

	for i := range regions {
		if regions[i].weight > 1e-6 {
			regions[i].repDX = int32(math.Round(float64(regions[i].sumDX / regions[i].weight)))
			regions[i].repDY = int32(math.Round(float64(regions[i].sumDY / regions[i].weight)))
		}
	}

	// Count contacts between already-labelled regions. Do this in a second pass so
	// traversal order cannot affect the adjacency graph.
	for y := level.active.Min.Y; y < level.active.Max.Y; y++ {
		for x := level.active.Min.X; x < level.active.Max.X; x++ {
			id := y*level.w + x
			rid := level.regionIDs[id]
			if rid < 0 {
				continue
			}
			for _, d := range [...]pmPoint{{x: 1}, {y: 1}} {
				nx, ny := x+int(d.x), y+int(d.y)
				if nx >= level.active.Max.X || ny >= level.active.Max.Y {
					continue
				}
				nrid := level.regionIDs[ny*level.w+nx]
				if nrid < 0 || nrid == rid {
					continue
				}
				if regions[nrid].size > 0 && (regions[rid].bestNeighbor < 0 || regions[nrid].size > regions[regions[rid].bestNeighbor].size) {
					regions[rid].bestNeighbor = nrid
				}
				if regions[rid].size > 0 && (regions[nrid].bestNeighbor < 0 || regions[rid].size > regions[regions[nrid].bestNeighbor].size) {
					regions[nrid].bestNeighbor = rid
				}
			}
		}
	}
	return regions
}

func pmWriteRegionConfidence(level *pmLevel, regions []pmRegionInfo) {
	size := level.w * level.h
	if cap(level.regionConfidence) < size {
		level.regionConfidence = make([]float32, size)
	} else {
		level.regionConfidence = level.regionConfidence[:size]
		clear(level.regionConfidence)
	}
	for y := level.active.Min.Y; y < level.active.Max.Y; y++ {
		for x := level.active.Min.X; x < level.active.Max.X; x++ {
			id := y*level.w + x
			if id >= len(level.regionIDs) || level.regionIDs[id] < 0 {
				continue
			}
			rid := int(level.regionIDs[id])
			if rid >= len(regions) {
				continue
			}
			s := float32(regions[rid].size)
			level.regionConfidence[id] = pmSmoothStep(2, 12, s)
		}
	}
}
