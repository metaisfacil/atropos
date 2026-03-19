package main

import (
	"context"
	"image"
	"image/draw"
	"math"
	"math/rand"
	"runtime"
	"sync"
	"time"
)

// PatchMatch-style approximate nearest-neighbour field used for a simple
// content-aware fill. This is a pragmatic, self-contained implementation
// suitable for a touch-up brush preview and initial commit behavior.

// PatchMatchFill fills regions marked in mask (alpha>0) in src and returns
// a new `*image.NRGBA` with filled pixels. patchSize should be odd (e.g. 7).
// iterations controls the number of propagation/random-search passes.
// ctx is checked between iterations; a cancellation returns (nil, ctx.Err()).
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

	// Destination starts as a copy of src.
	dst := image.NewNRGBA(bounds)
	draw.Draw(dst, bounds, src, bounds.Min, draw.Src)

	// Helper to test whether a patch centered at (cx,cy) lies fully inside image
	inBounds := func(cx, cy int) bool {
		return cx-half >= 0 && cy-half >= 0 && cx+half < w && cy+half < h
	}

	// helper to read alpha quickly (returns 0 if mask is nil)
	alphaAt := func(m *image.Alpha, x, y int) uint8 {
		if m == nil {
			return 0
		}
		bounds := m.Bounds()
		if x < bounds.Min.X || y < bounds.Min.Y || x >= bounds.Max.X || y >= bounds.Max.Y {
			return 0
		}
		return m.Pix[(y-bounds.Min.Y)*m.Stride+(x-bounds.Min.X)]
	}

	// Build list of target centers (centres whose center pixel is masked)
	type pt struct{ x, y int }
	var targets []pt
	for y := half; y < h-half; y++ {
		if y%64 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		for x := half; x < w-half; x++ {
			if alphaAt(mask, x, y) > 0 {
				targets = append(targets, pt{x, y})
			}
		}
	}
	if len(targets) == 0 {
		return dst, nil
	}

	// Build list of valid source centers (patches that do not overlap the mask)
	var sources []pt
	for y := half; y < h-half; y++ {
		if y%64 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		for x := half; x < w-half; x++ {
			ok := true
			for dy := -half; dy <= half && ok; dy++ {
				for dx := -half; dx <= half; dx++ {
					if alphaAt(mask, x+dx, y+dy) > 0 {
						ok = false
						break
					}
				}
			}
			if ok {
				sources = append(sources, pt{x, y})
			}
		}
	}
	if len(sources) == 0 {
		// no valid sources; nothing sensible to do
		return dst, nil
	}

	rand.Seed(time.Now().UnixNano())

	// NNF: slice indexed by target index -> source center
	nnf := make([]pt, len(targets))

	// cost cache for each target
	cost := make([]float64, len(targets))

	// compute SSD between a target patch at (tx,ty) and source patch at (sx,sy)
	patchSSD := func(tx, ty, sx, sy int) float64 {
		var s float64
		var count int
		for dy := -half; dy <= half; dy++ {
			for dx := -half; dx <= half; dx++ {
				px := tx + dx
				py := ty + dy
				if alphaAt(mask, px, py) > 0 {
					// unknown at target; skip
					continue
				}
				sxp := sx + dx
				syp := sy + dy
				sp := src.Pix[(syp*w+sxp)*4 : (syp*w+sxp)*4+4]
				tp := src.Pix[(py*w+px)*4 : (py*w+px)*4+4]
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

	// initialize nnf randomly
	for i, t := range targets {
		s := sources[rand.Intn(len(sources))]
		nnf[i] = s
		cost[i] = patchSSD(t.x, t.y, s.x, s.y)
	}

	maxDim := w
	if h > maxDim {
		maxDim = h
	}

	// PatchMatch main loop — parallelised by partitioning targets across workers.
	workers := runtime.NumCPU()
	if workers < 1 {
		workers = 1
	}

	for it := 0; it < iterations; it++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err() // cancelled between iterations
		default:
		}
		// Snapshot current field and cost for read-only access during this iteration.
		srcNnf := make([]pt, len(nnf))
		srcCost := make([]float64, len(cost))
		copy(srcNnf, nnf)
		copy(srcCost, cost)

		newNnf := make([]pt, len(nnf))
		newCost := make([]float64, len(cost))

		done := ctx.Done() // nil for context.Background() — select default always fires
		var wg sync.WaitGroup
		wg.Add(workers)
		for wi := 0; wi < workers; wi++ {
			start := wi * len(targets) / workers
			end := (wi + 1) * len(targets) / workers
			go func(start, end int) {
				defer wg.Done()
				for i := start; i < end; i++ {
					select {
					case <-done:
						return
					default:
					}
					t := targets[i]
					cx, cy := t.x, t.y
					best := srcNnf[i]
					bestCost := srcCost[i]

					// propagation proposals from neighbors (read from snapshot)
					for _, nIdx := range []int{i - 1, i - len(targets)} {
						if nIdx < 0 || nIdx >= len(targets) {
							continue
						}
						n := targets[nIdx]
						ns := srcNnf[nIdx]
						cand := pt{ns.x + (cx - n.x), ns.y + (cy - n.y)}
						if !inBounds(cand.x, cand.y) {
							continue
						}
						c := patchSSD(cx, cy, cand.x, cand.y)
						if c < bestCost {
							bestCost = c
							best = cand
						}
					}

					// random search
					r := maxDim
					for r >= 1 {
						minx := clamp(best.x-r, half, w-half-1)
						maxx := clamp(best.x+r, half, w-half-1)
						miny := clamp(best.y-r, half, h-half-1)
						maxy := clamp(best.y+r, half, h-half-1)
						rx := rand.Intn(maxx-minx+1) + minx
						ry := rand.Intn(maxy-miny+1) + miny
						c := patchSSD(cx, cy, rx, ry)
						if c < bestCost {
							bestCost = c
							best = pt{rx, ry}
						}
						r /= 2
					}

					newNnf[i] = best
					newCost[i] = bestCost
				}
			}(start, end)
		}
		wg.Wait()
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		// Reverse pass: process indices in reverse order but still partitioned
		// across workers. We reuse srcNnf/srcCost for proposals and write
		// again into newNnf/newCost (overwriting previous forward results
		// for indices processed here).
		wg.Add(workers)
		for wi := 0; wi < workers; wi++ {
			start := wi * len(targets) / workers
			end := (wi + 1) * len(targets) / workers
			go func(start, end int) {
				defer wg.Done()
				for ii := end - 1; ii >= start; ii-- {
					select {
					case <-done:
						return
					default:
					}
					t := targets[ii]
					cx, cy := t.x, t.y
					best := srcNnf[ii]
					bestCost := srcCost[ii]

					for _, nIdx := range []int{ii + 1, ii + len(targets)} {
						if nIdx < 0 || nIdx >= len(targets) {
							continue
						}
						n := targets[nIdx]
						ns := srcNnf[nIdx]
						cand := pt{ns.x + (cx - n.x), ns.y + (cy - n.y)}
						if !inBounds(cand.x, cand.y) {
							continue
						}
						c := patchSSD(cx, cy, cand.x, cand.y)
						if c < bestCost {
							bestCost = c
							best = cand
						}
					}

					r := maxDim
					for r >= 1 {
						minx := clamp(best.x-r, half, w-half-1)
						maxx := clamp(best.x+r, half, w-half-1)
						miny := clamp(best.y-r, half, h-half-1)
						maxy := clamp(best.y+r, half, h-half-1)
						rx := rand.Intn(maxx-minx+1) + minx
						ry := rand.Intn(maxy-miny+1) + miny
						c := patchSSD(cx, cy, rx, ry)
						if c < bestCost {
							bestCost = c
							best = pt{rx, ry}
						}
						r /= 2
					}

					newNnf[ii] = best
					newCost[ii] = bestCost
				}
			}(start, end)
		}
		wg.Wait()
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		// swap in new arrays
		nnf = newNnf
		cost = newCost
	}

	// Reconstruction: average contributions from mapped source patches for each masked pixel
	// Reconstruction: parallel accumulation. Each worker gets a partition
	// of targets and writes into its local accumulators which are merged
	// afterwards to avoid fine-grained synchronization.
	workers = runtime.NumCPU()
	if workers < 1 {
		workers = 1
	}

	// per-worker accumulators
	accRw := make([][]uint32, workers)
	accGw := make([][]uint32, workers)
	accBw := make([][]uint32, workers)
	accAw := make([][]uint32, workers)
	countw := make([][]uint32, workers)
	for wi := 0; wi < workers; wi++ {
		accRw[wi] = make([]uint32, w*h)
		accGw[wi] = make([]uint32, w*h)
		accBw[wi] = make([]uint32, w*h)
		accAw[wi] = make([]uint32, w*h)
		countw[wi] = make([]uint32, w*h)
	}

	var wg sync.WaitGroup
	wg.Add(workers)
	for wi := 0; wi < workers; wi++ {
		start := wi * len(targets) / workers
		end := (wi + 1) * len(targets) / workers
		go func(wi, start, end int) {
			defer wg.Done()
			aR := accRw[wi]
			aG := accGw[wi]
			aB := accBw[wi]
			aA := accAw[wi]
			cT := countw[wi]
			for i := start; i < end; i++ {
				s := nnf[i]
				t := targets[i]
				for dy := -half; dy <= half; dy++ {
					for dx := -half; dx <= half; dx++ {
						tx := t.x + dx
						ty := t.y + dy
						if alphaAt(mask, tx, ty) == 0 {
							continue
						}
						sx := s.x + dx
						sy := s.y + dy
						sp := src.Pix[(sy*w+sx)*4 : (sy*w+sx)*4+4]
						idx := ty*w + tx
						aR[idx] += uint32(sp[0])
						aG[idx] += uint32(sp[1])
						aB[idx] += uint32(sp[2])
						aA[idx] += uint32(sp[3])
						cT[idx]++
					}
				}
			}
		}(wi, start, end)
	}
	wg.Wait()

	// merge per-worker accumulators into single arrays
	accR := make([]uint32, w*h)
	accG := make([]uint32, w*h)
	accB := make([]uint32, w*h)
	accA := make([]uint32, w*h)
	count := make([]uint32, w*h)
	for wi := 0; wi < workers; wi++ {
		aR := accRw[wi]
		aG := accGw[wi]
		aB := accBw[wi]
		aA := accAw[wi]
		cT := countw[wi]
		for i := 0; i < w*h; i++ {
			if cT[i] == 0 {
				continue
			}
			accR[i] += aR[i]
			accG[i] += aG[i]
			accB[i] += aB[i]
			accA[i] += aA[i]
			count[i] += cT[i]
		}
	}

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if alphaAt(mask, x, y) == 0 {
				continue
			}
			idx := y*w + x
			if count[idx] == 0 {
				// fallback — copy original
				p := src.Pix[idx*4 : idx*4+4]
				dst.Pix[idx*4+0] = p[0]
				dst.Pix[idx*4+1] = p[1]
				dst.Pix[idx*4+2] = p[2]
				dst.Pix[idx*4+3] = p[3]
			} else {
				dst.Pix[idx*4+0] = uint8(accR[idx] / count[idx])
				dst.Pix[idx*4+1] = uint8(accG[idx] / count[idx])
				dst.Pix[idx*4+2] = uint8(accB[idx] / count[idx])
				dst.Pix[idx*4+3] = uint8(accA[idx] / count[idx])
			}
		}
	}

	return dst, nil
}
