package main

import (
	"context"
	"image"
	"image/draw"
	"math"
	"math/rand"
	"time"
)

// pt is a 2-D integer point used throughout the PatchMatch algorithm.
type pt struct{ x, y int }

// PatchMatchFill fills the region marked in mask (alpha > 0) using a
// PatchMatch nearest-neighbour field.  patchSize should be odd (e.g. 7).
// iterations controls the number of forward/reverse propagation passes per
// EM step.  The function runs numEM EM iterations: each step refines the
// reconstruction using the previous step's output as additional context,
// letting good matches propagate from the boundary into large holes.
func PatchMatchFill(ctx context.Context, src *image.NRGBA, mask *image.Alpha, patchSize, iterations int) (*image.NRGBA, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if patchSize%2 == 0 {
		patchSize++
	}
	bounds := src.Bounds()
	w := bounds.Dx()
	h := bounds.Dy()
	half := patchSize / 2

	alphaAt := func(x, y int) uint8 {
		if mask == nil {
			return 0
		}
		mb := mask.Bounds()
		if x < mb.Min.X || y < mb.Min.Y || x >= mb.Max.X || y >= mb.Max.Y {
			return 0
		}
		return mask.Pix[(y-mb.Min.Y)*mask.Stride+(x-mb.Min.X)]
	}

	inBounds := func(x, y int) bool {
		return x >= half && y >= half && x < w-half && y < h-half
	}

	// ── Collect targets and compute mask bounding box ────────────────────────
	var targets []pt
	bbMinX, bbMinY, bbMaxX, bbMaxY := w, h, 0, 0
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if alphaAt(x, y) > 0 {
				targets = append(targets, pt{x, y})
				if x < bbMinX {
					bbMinX = x
				}
				if y < bbMinY {
					bbMinY = y
				}
				if x > bbMaxX {
					bbMaxX = x
				}
				if y > bbMaxY {
					bbMaxY = y
				}
			}
		}
	}
	if len(targets) == 0 {
		dst := image.NewNRGBA(bounds)
		draw.Draw(dst, bounds, src, bounds.Min, draw.Src)
		return dst, nil
	}

	// Active region: mask bbox expanded by patchSize so propagation can reach
	// all masked pixels from their unmasked neighbours.
	ax0 := clamp(bbMinX-patchSize, half, w-half-1)
	ay0 := clamp(bbMinY-patchSize, half, h-half-1)
	ax1 := clamp(bbMaxX+patchSize, half, w-half-1)
	ay1 := clamp(bbMaxY+patchSize, half, h-half-1)

	// ── Build valid-source set ────────────────────────────────────────────────
	// A source patch is valid if none of its pixels are originally masked.
	validSrc := make([]bool, w*h)
	var sources []pt
	for y := half; y < h-half; y++ {
		for x := half; x < w-half; x++ {
			ok := true
		outerLoop:
			for dy := -half; dy <= half; dy++ {
				for dx := -half; dx <= half; dx++ {
					if alphaAt(x+dx, y+dy) > 0 {
						ok = false
						break outerLoop
					}
				}
			}
			if ok {
				validSrc[y*w+x] = true
				sources = append(sources, pt{x, y})
			}
		}
	}
	if len(sources) == 0 {
		dst := image.NewNRGBA(bounds)
		draw.Draw(dst, bounds, src, bounds.Min, draw.Src)
		return dst, nil
	}

	rand.Seed(time.Now().UnixNano()) //nolint:staticcheck

	// ── Working image and resolved map ───────────────────────────────────────
	// working holds the current best estimate of the full image.
	// Masked pixels start as zeros (unresolved); they are filled in by the
	// reconstruction step and used as context in subsequent EM iterations.
	working := image.NewNRGBA(bounds)
	draw.Draw(working, bounds, src, bounds.Min, draw.Src)
	for _, t := range targets {
		idx := (t.y*w + t.x) * 4
		working.Pix[idx] = 0
		working.Pix[idx+1] = 0
		working.Pix[idx+2] = 0
		working.Pix[idx+3] = 0
	}

	// resolved[y*w+x] is true when the pixel value in working is meaningful.
	// Initially only unmasked pixels are resolved.
	resolved := make([]bool, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if alphaAt(x, y) == 0 {
				resolved[y*w+x] = true
			}
		}
	}

	maxDim := w
	if h > maxDim {
		maxDim = h
	}

	nnf := make([]pt, w*h)
	cost := make([]float64, w*h)

	// ── EM loop ───────────────────────────────────────────────────────────────
	// Each EM iteration:
	//   E-step: compute nearest-neighbour field using current working image
	//   M-step: reconstruct masked pixels via similarity-weighted patch voting
	//
	// After iteration 0, all masked pixels have initial estimates in working.
	// Subsequent iterations benefit from this context: the patchSSD can compare
	// full patches (not just the unmasked fringe), enabling better propagation
	// into the interior of large holes.
	const numEM = 3

	for emIt := 0; emIt < numEM; emIt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		// patchSSD compares working[target patch] vs src[source patch].
		// Target pixels not yet resolved are skipped (NaN treatment).
		// Returns MaxFloat64 when no comparable pixels exist.
		patchSSD := func(tx, ty, sx, sy int) float64 {
			var s float64
			var count int
			for dy := -half; dy <= half; dy++ {
				for dx := -half; dx <= half; dx++ {
					tpx, tpy := tx+dx, ty+dy
					if !resolved[tpy*w+tpx] {
						continue
					}
					tp := working.Pix[(tpy*w+tpx)*4:]
					sp := src.Pix[((sy+dy)*w+(sx+dx))*4:]
					dr := float64(int(tp[0]) - int(sp[0]))
					dg := float64(int(tp[1]) - int(sp[1]))
					db := float64(int(tp[2]) - int(sp[2]))
					s += dr*dr + dg*dg + db*db
					count++
				}
			}
			if count == 0 {
				return math.MaxFloat64
			}
			return s / float64(count)
		}

		// Initialise NNF: identity for valid-source pixels, random otherwise.
		// The identity constraint anchors the known region so propagation
		// carries correct offsets into the masked area (same idea as the C++
		// reference: "force the link between unmasked patches in source/target").
		for i := range cost {
			cost[i] = math.MaxFloat64
		}
		for y := ay0; y <= ay1; y++ {
			for x := ax0; x <= ax1; x++ {
				if validSrc[y*w+x] {
					nnf[y*w+x] = pt{x, y}
					cost[y*w+x] = 0
				} else {
					s := sources[rand.Intn(len(sources))]
					nnf[y*w+x] = s
					cost[y*w+x] = patchSSD(x, y, s.x, s.y)
				}
			}
		}

		tryImprove := func(tx, ty, cx, cy int, best *pt, bestCost *float64) {
			if !inBounds(cx, cy) || !validSrc[cy*w+cx] {
				return
			}
			c := patchSSD(tx, ty, cx, cy)
			if c < *bestCost {
				*bestCost = c
				*best = pt{cx, cy}
			}
		}

		randomSearch := func(tx, ty int, best *pt, bestCost *float64) {
			r := maxDim
			for r >= 1 {
				minx := clamp(best.x-r, half, w-half-1)
				maxx := clamp(best.x+r, half, w-half-1)
				miny := clamp(best.y-r, half, h-half-1)
				maxy := clamp(best.y+r, half, h-half-1)
				if maxx > minx && maxy > miny {
					rx := rand.Intn(maxx-minx+1) + minx
					ry := rand.Intn(maxy-miny+1) + miny
					tryImprove(tx, ty, rx, ry, best, bestCost)
				}
				r /= 2
			}
		}

		// ── PatchMatch propagation iterations ────────────────────────────────
		// Alternating forward/reverse passes.  Skip validSrc pixels — they hold
		// the identity mapping and need no improvement.
		for it := 0; it < iterations; it++ {
			if it%2 == 0 {
				// Forward pass: top-left → bottom-right
				for y := ay0; y <= ay1; y++ {
					for x := ax0; x <= ax1; x++ {
						if validSrc[y*w+x] {
							continue
						}
						idx := y*w + x
						best := nnf[idx]
						bestCost := cost[idx]
						if x > ax0 {
							ns := nnf[y*w+(x-1)]
							tryImprove(x, y, ns.x+1, ns.y, &best, &bestCost)
						}
						if y > ay0 {
							ns := nnf[(y-1)*w+x]
							tryImprove(x, y, ns.x, ns.y+1, &best, &bestCost)
						}
						randomSearch(x, y, &best, &bestCost)
						nnf[idx] = best
						cost[idx] = bestCost
					}
				}
			} else {
				// Reverse pass: bottom-right → top-left
				for y := ay1; y >= ay0; y-- {
					for x := ax1; x >= ax0; x-- {
						if validSrc[y*w+x] {
							continue
						}
						idx := y*w + x
						best := nnf[idx]
						bestCost := cost[idx]
						if x < ax1 {
							ns := nnf[y*w+(x+1)]
							tryImprove(x, y, ns.x-1, ns.y, &best, &bestCost)
						}
						if y < ay1 {
							ns := nnf[(y+1)*w+x]
							tryImprove(x, y, ns.x, ns.y-1, &best, &bestCost)
						}
						randomSearch(x, y, &best, &bestCost)
						nnf[idx] = best
						cost[idx] = bestCost
					}
				}
			}
		}

		// ── Similarity-weighted patch-voting reconstruction (M-step) ─────────
		// Patches with lower cost receive exponentially higher weight, so good
		// matches dominate and poor ones (e.g. unmatched interior early on)
		// contribute little.  The exponent is normalised by the mean cost of
		// non-identity patches so it adapts to the image content.
		var costSum float64
		var costCount int
		for y := ay0; y <= ay1; y++ {
			for x := ax0; x <= ax1; x++ {
				if !validSrc[y*w+x] {
					c := cost[y*w+x]
					if c < math.MaxFloat64 {
						costSum += c
						costCount++
					}
				}
			}
		}
		costNorm := 1.0
		if costCount > 0 && costSum > 0 {
			costNorm = costSum / float64(costCount)
		}

		accR := make([]float64, w*h)
		accG := make([]float64, w*h)
		accB := make([]float64, w*h)
		accWt := make([]float64, w*h)

		// Iterate ALL active-region pixels as patch centres (including unmasked
		// pixels near the boundary).  A non-masked centre q contributes its
		// local neighbourhood to any masked pixel inside its footprint, which
		// naturally blends the fill into the seam.
		for y := ay0; y <= ay1; y++ {
			for x := ax0; x <= ax1; x++ {
				s := nnf[y*w+x]
				c := cost[y*w+x]
				if c == math.MaxFloat64 {
					continue
				}
				simWeight := math.Exp(-c / costNorm)
				if simWeight < 1e-10 {
					continue
				}
				for dy := -half; dy <= half; dy++ {
					for dx := -half; dx <= half; dx++ {
						px, py := x+dx, y+dy
						if alphaAt(px, py) == 0 {
							continue // only fill masked pixels
						}
						sx2, sy2 := s.x+dx, s.y+dy
						if sx2 < 0 || sy2 < 0 || sx2 >= w || sy2 >= h {
							continue
						}
						sp := src.Pix[(sy2*w+sx2)*4:]
						pidx := py*w + px
						accR[pidx] += simWeight * float64(sp[0])
						accG[pidx] += simWeight * float64(sp[1])
						accB[pidx] += simWeight * float64(sp[2])
						accWt[pidx] += simWeight
					}
				}
			}
		}

		// Update working image; mark all reconstructed pixels as resolved so
		// subsequent EM iterations can use them as context.
		for _, t := range targets {
			idx := t.y*w + t.x
			if accWt[idx] > 0 {
				working.Pix[idx*4+0] = uint8(accR[idx] / accWt[idx])
				working.Pix[idx*4+1] = uint8(accG[idx] / accWt[idx])
				working.Pix[idx*4+2] = uint8(accB[idx] / accWt[idx])
				working.Pix[idx*4+3] = 255
				resolved[idx] = true
			}
		}
	}

	// ── Build output ──────────────────────────────────────────────────────────
	dst := image.NewNRGBA(bounds)
	draw.Draw(dst, bounds, src, bounds.Min, draw.Src)
	for _, t := range targets {
		idx := t.y*w + t.x
		if resolved[idx] {
			dst.Pix[idx*4+0] = working.Pix[idx*4+0]
			dst.Pix[idx*4+1] = working.Pix[idx*4+1]
			dst.Pix[idx*4+2] = working.Pix[idx*4+2]
			dst.Pix[idx*4+3] = 255
		}
	}

	return dst, nil
}
