package patchmatch

import (
	"context"
	"image"
	"math"
)

// reconstructPMLevel is the repaint step. It keeps the sharp edges and the fine
// texture that plain patch voting would smear:
//
//  1. Overlapping matched patches vote to give a stable colour estimate.
//  2. On a low-frequency edge, one dominant displacement replaces that average.
//     Because every contributing patch then reads the same source pixel, a
//     sharp source edge stays sharp instead of being averaged into a ramp.
//  3. Away from edges, a coherent source residual puts back the fine texture
//     that averaging flattens.
func reconstructPMLevel(ctx context.Context, level *pmLevel, previous *image.NRGBA, nnf []pmPoint, costs []float32) (*image.NRGBA, error) {
	pmPreparePhotoTransforms(level, nnf)
	coherence := pmNNFCoherenceWeights(level, nnf)
	warped, err := pmPatchVote(ctx, level, previous, nnf, costs, coherence)
	if err != nil {
		return nil, err
	}
	warped, err = pmRestoreTextureDetail(ctx, level, warped, nnf, costs, coherence)
	if err != nil {
		return nil, err
	}

	out := cloneNRGBA(level.src)
	bounds := level.painted
	err = parallelRowsSized(ctx, bounds.Min.Y, bounds.Max.Y, bounds.Dx(), func(y int) {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			maskAlpha := level.mask.Pix[y*level.mask.Stride+x]
			if maskAlpha == 0 {
				continue
			}
			si := y*level.src.Stride + x*4
			wi := y*warped.Stride + x*4
			di := y*out.Stride + x*4
			a := int(maskAlpha)
			for c := 0; c < 4; c++ {
				out.Pix[di+c] = byte((int(level.src.Pix[si+c])*(255-a) + int(warped.Pix[wi+c])*a + 127) / 255)
			}
		}
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

type pmStructureVoteCluster struct {
	used       bool
	dx, dy     int32
	support    float32
	bestWeight float32
	bestID     int
}

type pmVoteFactor struct {
	evidence float32
	cost     float32
}

// Both weights depend only on the matched patch, not on which output pixel it
// votes for, so they are computed once per patch instead of once per vote: up
// to 225 times over for a 15x15 patch. Multiply them in the same order at the
// use sites, or the results shift in the last bits.
func pmPrepareVoteFactors(level *pmLevel, costs []float32) []pmVoteFactor {
	size := level.w * level.h
	if cap(level.voteFactors) < size {
		level.voteFactors = make([]pmVoteFactor, size)
	} else {
		level.voteFactors = level.voteFactors[:size]
	}
	half := level.half
	area := float32(level.patchSize * level.patchSize)
	for y := level.active.Min.Y; y < level.active.Max.Y; y++ {
		for x := level.active.Min.X; x < level.active.Max.X; x++ {
			id := y*level.w + x
			evidence := pmConfidenceRectSum(level, x-half, y-half, x+half+1, y+half+1) / area
			cost := float32(1)
			if id < len(costs) && !float32IsInf(costs[id]) {
				cost = 1 / (1 + costs[id]/384)
			}
			level.voteFactors[id] = pmVoteFactor{0.20 + 0.80*minFloat32(1, evidence), cost}
		}
	}
	return level.voteFactors
}

func pmFindStructureVoteCluster(clusters *[256]pmStructureVoteCluster, dx, dy int32) *pmStructureVoteCluster {
	hash := uint32(dx)*0x9e3779b1 ^ uint32(dy)*0x85ebca77
	for probe := 0; probe < len(clusters); probe++ {
		cluster := &clusters[(int(hash)+probe)&(len(clusters)-1)]
		if !cluster.used {
			cluster.used = true
			cluster.dx, cluster.dy = dx, dy
			cluster.bestID = -1
			return cluster
		}
		if cluster.dx == dx && cluster.dy == dy {
			return cluster
		}
	}
	return nil
}

// pmBestStructureVote picks a single displacement. Displacements within one
// pixel of a candidate lend it partial support when deciding which one wins,
// because they are usually the same edge sampled slightly differently. Only the
// winner is rendered: neighbouring displacements never reach the output.
func pmBestStructureVote(clusters *[256]pmStructureVoteCluster, totalSupport float32) (dx, dy int32, bestID int, dominance float32, ok bool) {
	bestID = -1
	if totalSupport <= 1e-7 {
		return 0, 0, -1, 0, false
	}
	bestScore := float32(-1)
	bestNeighborhood := float32(0)
	for i := range clusters {
		candidate := &clusters[i]
		if !candidate.used || candidate.support <= 0 {
			continue
		}
		score := candidate.support
		neighborhood := candidate.support
		for j := range clusters {
			if i == j {
				continue
			}
			other := &clusters[j]
			if !other.used || absInt(int(other.dx-candidate.dx)) > 1 || absInt(int(other.dy-candidate.dy)) > 1 {
				continue
			}
			// Near-miss displacements help identify the right winner, but they count
			// for less than exact agreement and the rendered pixel still comes only
			// from candidate.dx/candidate.dy.
			score += 0.34 * other.support
			neighborhood += 0.58 * other.support
		}
		if score > bestScore {
			bestScore = score
			bestNeighborhood = neighborhood
			dx, dy = candidate.dx, candidate.dy
			bestID = candidate.bestID
			ok = true
		}
	}
	if ok {
		dominance = minFloat32(1, bestNeighborhood/maxFloat32(totalSupport, 1e-6))
	}
	return
}

// pmPatchVote rebuilds each pixel from the overlapping matched patches that
// cover it, using the same patch size as the search. Flat and textured areas
// keep the weighted average. The stronger the edge, the more the pixel comes
// instead from a single displacement, so two sharp edges at different offsets
// are never averaged into a soft ramp.
func pmPatchVote(ctx context.Context, level *pmLevel, previous *image.NRGBA, nnf []pmPoint, costs, coherence []float32) (*image.NRGBA, error) {
	warped := cloneNRGBA(previous)
	half := level.half
	spatial := pmVoteSpatialWeights(level.patchSize)
	factors := pmPrepareVoteFactors(level, costs)

	voteBounds := level.painted
	if voteBounds.Empty() {
		return warped, nil
	}
	voteBounds = image.Rect(
		maxInt(0, voteBounds.Min.X-half),
		maxInt(0, voteBounds.Min.Y-half),
		minInt(level.w, voteBounds.Max.X+half),
		minInt(level.h, voteBounds.Max.Y+half),
	)

	err := parallelRowsSized(ctx, voteBounds.Min.Y, voteBounds.Max.Y, voteBounds.Dx()*level.patchSize, func(y int) {
		for x := voteBounds.Min.X; x < voteBounds.Max.X; x++ {
			if level.mask.Pix[y*level.mask.Stride+x] == 0 {
				continue
			}
			idOut := y*level.w + x
			structure := float32(0)
			if idOut < len(level.structureGuide.strength) {
				structure = level.structureGuide.strength[idOut]
			}

			var sum [4]float64
			var total float64
			var structureClusters [256]pmStructureVoteCluster
			var structureSupport float32
			minCY := maxInt(level.active.Min.Y, y-half)
			maxCY := minInt(level.active.Max.Y-1, y+half)
			minCX := maxInt(level.active.Min.X, x-half)
			maxCX := minInt(level.active.Max.X-1, x+half)
			for cy := minCY; cy <= maxCY; cy++ {
				wy := spatial[y-cy+half]
				for cx := minCX; cx <= maxCX; cx++ {
					id := cy*level.w + cx
					match := nnf[id]
					if !validPMPoint(level, match) {
						continue
					}
					sx := int(match.x) + x - cx
					sy := int(match.y) + y - cy
					if sx < 0 || sy < 0 || sx >= level.w || sy >= level.h {
						continue
					}

					factor := factors[id]
					weight := spatial[x-cx+half] * wy * coherence[id] * factor.evidence * factor.cost
					if weight <= 1e-7 {
						continue
					}
					si := sy*level.src.Stride + sx*4
					photo := pmIdentityPhotoTransform()
					if id < len(level.photo) {
						photo = level.photo[id]
					}
					if len(level.photo) == 0 {
						sum[0] += float64(weight) * float64(level.src.Pix[si])
						sum[1] += float64(weight) * float64(level.src.Pix[si+1])
						sum[2] += float64(weight) * float64(level.src.Pix[si+2])
					} else {
						for c := 0; c < 3; c++ {
							sum[c] += float64(weight) * float64(pmApplyPhotoRGBFloat(float32(level.src.Pix[si+c]), c, photo))
						}
					}
					sum[3] += float64(weight) * float64(level.src.Pix[si+3])
					total += float64(weight)

					if structure > 0.015 {
						dx := match.x - int32(cx)
						dy := match.y - int32(cy)
						cluster := pmFindStructureVoteCluster(&structureClusters, dx, dy)
						if cluster != nil {
							cluster.support += weight
							if weight > cluster.bestWeight {
								cluster.bestWeight = weight
								cluster.bestID = id
							}
							structureSupport += weight
						}
					}
				}
			}

			di := y*warped.Stride + x*4
			if total <= 1e-9 {
				if idOut >= 0 && idOut < len(nnf) && validPMPoint(level, nnf[idOut]) {
					sx, sy := int(nnf[idOut].x), int(nnf[idOut].y)
					si := sy*level.src.Stride + sx*4
					photo := pmIdentityPhotoTransform()
					if idOut < len(level.photo) {
						photo = level.photo[idOut]
					}
					for c := 0; c < 3; c++ {
						warped.Pix[di+c] = pmApplyPhotoRGB(level.src.Pix[si+c], c, photo)
					}
					warped.Pix[di+3] = level.src.Pix[si+3]
				}
				continue
			}

			var voted [4]float32
			for c := 0; c < 4; c++ {
				voted[c] = float32(sum[c] / total)
			}

			structureMix := float32(0)
			var direct [4]byte
			if structure > 0.015 {
				dx, dy, bestID, dominance, ok := pmBestStructureVote(&structureClusters, structureSupport)
				if ok {
					sx, sy := x+int(dx), y+int(dy)
					if sx >= 0 && sy >= 0 && sx < level.w && sy < level.h {
						si := sy*level.src.Stride + sx*4
						photo := pmIdentityPhotoTransform()
						if bestID >= 0 && bestID < len(level.photo) {
							photo = level.photo[bestID]
						}
						for c := 0; c < 3; c++ {
							direct[c] = pmApplyPhotoRGB(level.src.Pix[si+c], c, photo)
						}
						direct[3] = level.src.Pix[si+3]
						sourceStructure := float32(0)
						if sy*level.w+sx < len(level.structureSource.strength) {
							sourceStructure = level.structureSource.strength[sy*level.w+sx]
						}
						supportGate := pmSmoothStep(0.12, 0.36, dominance)
						sourceGate := 0.35 + 0.65*pmSmoothStep(0.06, 0.38, sourceStructure)
						structureMix = structure * supportGate * sourceGate
						// Where the target is clearly an edge and the displacements agree,
						// commit hard to the single sharp source sample. This is the case
						// where plain patch voting looks worst.
						if structure > 0.82 && dominance > 0.22 && sourceStructure > 0.14 {
							floor := 0.78 + 0.20*pmSmoothStep(0.22, 0.48, dominance)
							structureMix = maxFloat32(structureMix, floor)
						}
						structureMix = minFloat32(1, structureMix)
					}
				}
			}

			for c := 0; c < 4; c++ {
				value := voted[c]
				if structureMix > 0 {
					value = value*(1-structureMix) + float32(direct[c])*structureMix
				}
				warped.Pix[di+c] = byte(clampFloat32(value))
			}
		}
	})
	return warped, err
}

type pmDetailCluster struct {
	used                bool
	keyX, keyY          int32
	bestDX, bestDY      int32
	support, bestWeight float32
	bestID              int
}

func pmDetailClusterKey(value int32) int32 {
	if value >= 0 {
		return value / 2
	}
	return -((-value + 1) / 2)
}

func pmFindDetailCluster(clusters *[128]pmDetailCluster, dx, dy int32) *pmDetailCluster {
	keyX, keyY := pmDetailClusterKey(dx), pmDetailClusterKey(dy)
	hash := uint32(keyX)*0x9e3779b1 ^ uint32(keyY)*0x85ebca77
	for probe := 0; probe < len(clusters); probe++ {
		cluster := &clusters[(int(hash)+probe)&(len(clusters)-1)]
		if !cluster.used {
			cluster.used = true
			cluster.keyX, cluster.keyY = keyX, keyY
			cluster.bestDX, cluster.bestDY = dx, dy
			cluster.bestID = -1
			return cluster
		}
		if cluster.keyX == keyX && cluster.keyY == keyY {
			return cluster
		}
	}
	return nil
}

type pmTextureWarpPixel struct {
	dx, dy    int32
	dominance float32
	photoGain float32
	mapped    bool
}

type pmTextureWarpField struct {
	bounds image.Rectangle
	stride int
	pixels []pmTextureWarpPixel
}

func (f *pmTextureWarpField) index(x, y int) int {
	return (y-f.bounds.Min.Y)*f.stride + (x - f.bounds.Min.X)
}

func (f *pmTextureWarpField) at(x, y int) *pmTextureWarpPixel {
	if f == nil || x < f.bounds.Min.X || y < f.bounds.Min.Y || x >= f.bounds.Max.X || y >= f.bounds.Max.Y {
		return nil
	}
	return &f.pixels[f.index(x, y)]
}

// pmDominantTextureWarp stores warp state for the painted rectangle only. Only
// masked output pixels ever read it, so full-working-image arrays would be
// almost entirely wasted.
func pmDominantTextureWarp(ctx context.Context, level *pmLevel, voted *image.NRGBA, nnf []pmPoint, costs, coherence []float32) (*pmTextureWarpField, error) {
	bounds := level.painted
	field := &pmTextureWarpField{bounds: bounds, stride: bounds.Dx()}
	if bounds.Empty() {
		return field, nil
	}
	field.pixels = make([]pmTextureWarpPixel, bounds.Dx()*bounds.Dy())
	spatial := pmVoteSpatialWeights(level.patchSize)
	factors := pmPrepareVoteFactors(level, costs)
	half := level.half

	err := parallelRowsSized(ctx, bounds.Min.Y, bounds.Max.Y, bounds.Dx()*level.patchSize, func(y int) {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if level.mask.Pix[y*level.mask.Stride+x] == 0 {
				continue
			}
			var clusters [128]pmDetailCluster
			var totalSupport float32
			minCY := maxInt(level.active.Min.Y, y-half)
			maxCY := minInt(level.active.Max.Y-1, y+half)
			minCX := maxInt(level.active.Min.X, x-half)
			maxCX := minInt(level.active.Max.X-1, x+half)
			for cy := minCY; cy <= maxCY; cy++ {
				wy := spatial[y-cy+half]
				for cx := minCX; cx <= maxCX; cx++ {
					id := cy*level.w + cx
					match := nnf[id]
					if !validPMPoint(level, match) {
						continue
					}
					factor := factors[id]
					weight := spatial[x-cx+half] * wy * coherence[id] * factor.evidence * factor.cost
					if weight <= 1e-7 {
						continue
					}
					offsetX := match.x - int32(cx)
					offsetY := match.y - int32(cy)
					cluster := pmFindDetailCluster(&clusters, offsetX, offsetY)
					if cluster == nil {
						continue
					}
					cluster.support += weight
					totalSupport += weight
					if weight > cluster.bestWeight {
						cluster.bestWeight = weight
						cluster.bestDX, cluster.bestDY = offsetX, offsetY
						cluster.bestID = id
					}
				}
			}
			var best *pmDetailCluster
			for i := range clusters {
				cluster := &clusters[i]
				if !cluster.used || cluster.support <= 0 {
					continue
				}
				if best == nil || cluster.support > best.support {
					best = cluster
				}
			}
			if best == nil {
				continue
			}
			pixel := field.at(x, y)
			pixel.dx, pixel.dy = best.bestDX, best.bestDY
			pixel.dominance = best.support / maxFloat32(totalSupport, 1e-6)
			pixel.photoGain = 1
			if best.bestID >= 0 && best.bestID < len(level.photo) {
				pixel.photoGain = level.photo[best.bestID].gain
			}
			pixel.mapped = true
		}
	})
	if err != nil {
		return nil, err
	}
	pmSmoothTextureWarp(level, voted, field)
	return field, nil
}

func pmSmoothTextureWarp(level *pmLevel, voted *image.NRGBA, field *pmTextureWarpField) {
	if field == nil || field.bounds.Empty() {
		return
	}
	bounds := field.bounds
	old := append([]pmTextureWarpPixel(nil), field.pixels...)
	oldAt := func(x, y int) *pmTextureWarpPixel {
		if x < bounds.Min.X || y < bounds.Min.Y || x >= bounds.Max.X || y >= bounds.Max.Y {
			return nil
		}
		return &old[(y-bounds.Min.Y)*field.stride+x-bounds.Min.X]
	}
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			cur := oldAt(x, y)
			if cur == nil || !cur.mapped {
				continue
			}
			bestScore := float32(-1)
			best := *cur
			for cy := maxInt(bounds.Min.Y, y-1); cy <= minInt(bounds.Max.Y-1, y+1); cy++ {
				for cx := maxInt(bounds.Min.X, x-1); cx <= minInt(bounds.Max.X-1, x+1); cx++ {
					candidate := oldAt(cx, cy)
					if candidate == nil || !candidate.mapped {
						continue
					}
					keyX, keyY := pmDetailClusterKey(candidate.dx), pmDetailClusterKey(candidate.dy)
					var score float32
					for ny := maxInt(bounds.Min.Y, y-1); ny <= minInt(bounds.Max.Y-1, y+1); ny++ {
						for nx := maxInt(bounds.Min.X, x-1); nx <= minInt(bounds.Max.X-1, x+1); nx++ {
							n := oldAt(nx, ny)
							if n == nil || !n.mapped || pmDetailClusterKey(n.dx) != keyX || pmDetailClusterKey(n.dy) != keyY {
								continue
							}
							colorDifference := pmPixelColorDifference(voted, x, y, nx, ny)
							conductance := 1 / (1 + colorDifference/12)
							id, nid := y*level.w+x, ny*level.w+nx
							if id < len(level.structureGuide.strength) && nid < len(level.structureGuide.strength) {
								barrier := maxFloat32(level.structureGuide.strength[id], level.structureGuide.strength[nid])
								conductance *= 1 - 0.90*barrier
							}
							spatial := float32(1)
							if nx != x || ny != y {
								spatial = 0.72
							}
							score += (0.25 + 0.75*n.dominance) * conductance * spatial
						}
					}
					if keyX == pmDetailClusterKey(cur.dx) && keyY == pmDetailClusterKey(cur.dy) {
						score += 0.35
					}
					if score > bestScore {
						bestScore = score
						best = *candidate
					}
				}
			}
			pixel := field.at(x, y)
			pixel.dx, pixel.dy = best.dx, best.dy
			pixel.dominance = maxFloat32(cur.dominance, best.dominance*0.85)
			pixel.mapped = true
		}
	}
}

func pmRestoreTextureDetail(ctx context.Context, level *pmLevel, voted *image.NRGBA, nnf []pmPoint, costs, coherence []float32) (*image.NRGBA, error) {
	if len(level.textureGuide) != level.w*level.h || len(level.textureEnergy) != level.w*level.h {
		return voted, nil
	}
	warp, err := pmDominantTextureWarp(ctx, level, voted, nnf, costs, coherence)
	if err != nil {
		return nil, err
	}
	out := cloneNRGBA(voted)
	bounds := level.painted
	if bounds.Empty() {
		return out, nil
	}
	blurRadius := minInt(2, maxInt(1, level.half))

	err = parallelRowsSized(ctx, bounds.Min.Y, bounds.Max.Y, bounds.Dx(), func(y int) {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			id := y*level.w + x
			warpPixel := warp.at(x, y)
			if level.mask.Pix[y*level.mask.Stride+x] == 0 || warpPixel == nil || !warpPixel.mapped {
				continue
			}
			targetTexture := level.textureGuide[id]
			textureMix := pmTextureStrength(targetTexture)
			if textureMix <= 0.001 {
				continue
			}
			structureProtection := float32(0)
			if id < len(level.structureGuide.strength) {
				structureProtection = level.structureGuide.strength[id]
			}
			// On a strong edge the voting step above already copied a real sharp
			// source sample. Do not put a blurred base back over it.
			if structureProtection >= 0.94 {
				continue
			}

			sx := x + int(warpPixel.dx)
			sy := y + int(warpPixel.dy)
			if sx < 0 || sy < 0 || sx >= level.w || sy >= level.h {
				continue
			}
			sourceID := sy*level.w + sx
			sourceTexture := level.textureEnergy[sourceID]
			if sourceTexture < 0.15 {
				continue
			}

			var votedBase, sourceBase [3]float32
			if structureProtection < 0.12 {
				// In genuinely noisy regions the low-pass must be plain and linear, so
				// that the full grain ends up in the residual rather than being
				// absorbed into an edge-aware base.
				votedBase = pmLowPassPixel(voted, nil, x, y, blurRadius)
				sourceBase = pmLowPassPixel(level.src, level.sourceMask, sx, sy, blurRadius)
			} else {
				// Near an edge, use an edge-aware base instead, so splitting the pixel
				// into base and residual does not itself average across the boundary.
				votedBase = pmEdgeAwareLowPassPixel(voted, nil, x, y, blurRadius)
				sourceBase = pmEdgeAwareLowPassPixel(level.src, level.sourceMask, sx, sy, blurRadius)
			}
			scale := targetTexture / maxFloat32(sourceTexture, 0.35)
			scale = minFloat32(1.55, maxFloat32(0.65, scale))

			dominanceWeight := 0.84 + 0.16*float32(math.Sqrt(float64(minFloat32(1, warpPixel.dominance))))
			// This reaches exactly zero on a fully structural pixel. Leaving even a
			// third of the residual path in place at the strongest edges was enough
			// to visibly soften black/gold and silver/colour boundaries.
			mix := textureMix * dominanceWeight * (1 - structureProtection)
			mix = minFloat32(1, maxFloat32(0, mix))
			if mix <= 0.001 {
				continue
			}

			si := sy*level.src.Stride + sx*4
			di := y*out.Stride + x*4
			vi := y*voted.Stride + x*4
			for c := 0; c < 3; c++ {
				residual := float32(level.src.Pix[si+c]) - sourceBase[c]
				photoGain := warpPixel.photoGain
				if photoGain <= 0 {
					photoGain = 1
				}
				detailed := votedBase[c] + residual*scale*photoGain
				value := float32(voted.Pix[vi+c])*(1-mix) + detailed*mix
				out.Pix[di+c] = byte(clampFloat32(value))
			}
		}
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// pmLowPassPixel returns a small binomial low-pass sample, skipping masked
// pixels. It is plain and linear, not edge-aware, because the flat noisy
// regions that use it need the whole high-frequency residual left intact for
// texture transfer.
func pmLowPassPixel(src *image.NRGBA, exclusion *image.Alpha, x, y, radius int) [3]float32 {
	weights := [...]float32{1, 4, 6, 4, 1}
	if radius <= 1 {
		weights = [...]float32{0, 1, 2, 1, 0}
	}
	var sum [3]float32
	var total float32
	for oy := -2; oy <= 2; oy++ {
		wy := weights[oy+2]
		if wy == 0 {
			continue
		}
		sy := clampInt(y+oy, 0, src.Bounds().Dy()-1)
		for ox := -2; ox <= 2; ox++ {
			wx := weights[ox+2]
			if wx == 0 {
				continue
			}
			sx := clampInt(x+ox, 0, src.Bounds().Dx()-1)
			if exclusion != nil && exclusion.Pix[sy*exclusion.Stride+sx] != 0 {
				continue
			}
			i := sy*src.Stride + sx*4
			alpha := float32(src.Pix[i+3]) / 255
			weight := wx * wy * alpha
			if weight <= 0 {
				continue
			}
			sum[0] += weight * float32(src.Pix[i])
			sum[1] += weight * float32(src.Pix[i+1])
			sum[2] += weight * float32(src.Pix[i+2])
			total += weight
		}
	}
	if total > 1e-6 {
		for c := 0; c < 3; c++ {
			sum[c] /= total
		}
		return sum
	}
	i := clampInt(y, 0, src.Bounds().Dy()-1)*src.Stride + clampInt(x, 0, src.Bounds().Dx()-1)*4
	return [3]float32{float32(src.Pix[i]), float32(src.Pix[i+1]), float32(src.Pix[i+2])}
}

// pmEdgeAwareLowPassPixel is a small bilateral low-pass, used only to split a
// pixel into base and residual. Nearby pixels on the far side of a colour edge
// get very little weight, so the base never blends a colour that exists on
// neither side of the edge.
func pmEdgeAwareLowPassPixel(src *image.NRGBA, exclusion *image.Alpha, x, y, radius int) [3]float32 {
	weights := [...]float32{1, 4, 6, 4, 1}
	if radius <= 1 {
		weights = [...]float32{0, 1, 2, 1, 0}
	}
	center := y*src.Stride + x*4
	cr, cg, cb := float32(src.Pix[center]), float32(src.Pix[center+1]), float32(src.Pix[center+2])
	var sum [3]float32
	var total float32
	for oy := -2; oy <= 2; oy++ {
		wy := weights[oy+2]
		if wy == 0 {
			continue
		}
		sy := clampInt(y+oy, 0, src.Bounds().Dy()-1)
		for ox := -2; ox <= 2; ox++ {
			wx := weights[ox+2]
			if wx == 0 {
				continue
			}
			sx := clampInt(x+ox, 0, src.Bounds().Dx()-1)
			if exclusion != nil && exclusion.Pix[sy*exclusion.Stride+sx] != 0 {
				continue
			}
			i := sy*src.Stride + sx*4
			dr := float32(src.Pix[i]) - cr
			dg := float32(src.Pix[i+1]) - cg
			db := float32(src.Pix[i+2]) - cb
			distance2 := (dr*dr + dg*dg + db*db) / 3
			// A rational falloff rather than a Gaussian, to avoid an exp() call in
			// this inner loop.
			colourWeight := 1 / (1 + distance2/(18*18))
			alpha := float32(src.Pix[i+3]) / 255
			weight := wx * wy * colourWeight * alpha
			if weight <= 0 {
				continue
			}
			sum[0] += weight * float32(src.Pix[i])
			sum[1] += weight * float32(src.Pix[i+1])
			sum[2] += weight * float32(src.Pix[i+2])
			total += weight
		}
	}
	if total > 1e-6 {
		for c := 0; c < 3; c++ {
			sum[c] /= total
		}
		return sum
	}
	return [3]float32{cr, cg, cb}
}

func pmPixelLuma(src *image.NRGBA, x, y int) float32 {
	i := y*src.Stride + x*4
	return 0.299*float32(src.Pix[i]) + 0.587*float32(src.Pix[i+1]) + 0.114*float32(src.Pix[i+2])
}

func pmSmoothStep(low, high, value float32) float32 {
	if high <= low {
		if value >= high {
			return 1
		}
		return 0
	}
	t := (value - low) / (high - low)
	t = minFloat32(1, maxFloat32(0, t))
	return t * t * (3 - 2*t)
}

func pmVoteSpatialWeights(patchSize int) []float32 {
	half := patchSize / 2
	weights := make([]float32, patchSize)
	sigma := math.Max(1, float64(half)*0.65)
	denominator := 2 * sigma * sigma
	for i := range weights {
		d := float64(i - half)
		weights[i] = float32(math.Exp(-(d * d) / denominator))
	}
	return weights
}

// pmNNFCoherenceWeights weights each center by how well its displacement agrees
// with its neighbours. Centers that disagree keep a small share of the base
// vote, but cannot drive structure or texture unless their displacement also
// wins the cluster selection above.
func pmNNFCoherenceWeights(level *pmLevel, nnf []pmPoint) []float32 {
	size := level.w * level.h
	if cap(level.coherence) < size {
		level.coherence = make([]float32, size)
	} else {
		level.coherence = level.coherence[:size]
		clear(level.coherence)
	}
	weights := level.coherence
	neighbors := [...]image.Point{{X: -1}, {X: 1}, {Y: -1}, {Y: 1}}
	for y := level.active.Min.Y; y < level.active.Max.Y; y++ {
		for x := level.active.Min.X; x < level.active.Max.X; x++ {
			id := y*level.w + x
			match := nnf[id]
			if !validPMPoint(level, match) {
				continue
			}
			dx := int(match.x) - x
			dy := int(match.y) - y
			coherent, available := 0, 0
			for _, step := range neighbors {
				nx, ny := x+step.X, y+step.Y
				if nx < level.active.Min.X || ny < level.active.Min.Y || nx >= level.active.Max.X || ny >= level.active.Max.Y {
					continue
				}
				neighbor := nnf[ny*level.w+nx]
				if !validPMPoint(level, neighbor) {
					continue
				}
				available++
				ndx := int(neighbor.x) - nx
				ndy := int(neighbor.y) - ny
				if absInt(ndx-dx) <= 1 && absInt(ndy-dy) <= 1 {
					coherent++
				}
			}
			if available == 0 {
				weights[id] = 0.25
				continue
			}
			fraction := float32(coherent) / float32(available)
			weights[id] = 0.06 + 0.94*fraction*fraction
		}
	}
	return weights
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
