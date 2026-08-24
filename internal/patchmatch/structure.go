package patchmatch

import (
	"context"
	"image"
	"math"
)

// pmStructureField is a compact descriptor used directly by the E- and M-steps.
// strength is the low-frequency edge magnitude mapped to 0..1. orientX/Y are
// the double-angle structure orientation multiplied by tensor coherence:
//
//	orientX = (Jxx-Jyy)/(Jxx+Jyy)
//	orientY = 2*Jxy/(Jxx+Jyy)
//
// This representation removes repeated hypot/divide/normalization work from
// every PatchMatch candidate and uses three planes instead of v3's four.
type pmStructureField struct {
	strength []float32
	orientX  []float32
	orientY  []float32
}

func pmPrepareStructureModel(ctx context.Context, level *pmLevel) error {
	if level == nil || level.src == nil || level.w == 0 || level.h == 0 {
		return nil
	}
	var err error
	level.structureSource, err = pmStructureTensorMap(ctx, level.src)
	if err != nil {
		return err
	}
	guideImage := pmStructureGuideImage(level.src, level.mask, level.insideDepth)
	level.structureGuide, err = pmStructureTensorMap(ctx, guideImage)
	return err
}

// pmStructureGuideImage creates a low-frequency continuation through the mask
// using only observed pixels. Known pixels are immutable constraints; only the
// small brush bounds are relaxed. Unlike v3, the second ping-pong image is not
// recopied in full on every Jacobi iteration.
func pmStructureGuideImage(src *image.NRGBA, mask *image.Alpha, insideDepth []int) *image.NRGBA {
	out := cloneNRGBA(src)
	bounds := maskBounds(mask)
	if bounds.Empty() {
		return out
	}
	w, h := src.Bounds().Dx(), src.Bounds().Dy()

	assigned := make([]bool, w*h)
	queue := make([]int, 0, bounds.Dx()*bounds.Dy())
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if mask.Pix[y*mask.Stride+x] == 0 {
				continue
			}
			var sum [4]int
			count := 0
			for ny := maxInt(0, y-1); ny <= minInt(h-1, y+1); ny++ {
				for nx := maxInt(0, x-1); nx <= minInt(w-1, x+1); nx++ {
					if (nx == x && ny == y) || mask.Pix[ny*mask.Stride+nx] != 0 {
						continue
					}
					i := ny*src.Stride + nx*4
					for c := 0; c < 4; c++ {
						sum[c] += int(src.Pix[i+c])
					}
					count++
				}
			}
			if count == 0 {
				continue
			}
			i := y*out.Stride + x*4
			for c := 0; c < 4; c++ {
				out.Pix[i+c] = byte((sum[c] + count/2) / count)
			}
			id := y*w + x
			assigned[id] = true
			queue = append(queue, id)
		}
	}

	// Onion-peel the boundary colours inward so all masked pixels have a stable
	// initial value before relaxation.
	for head := 0; head < len(queue); head++ {
		id := queue[head]
		x, y := id%w, id/w
		for _, d := range [...]image.Point{{X: -1}, {X: 1}, {Y: -1}, {Y: 1}} {
			nx, ny := x+d.X, y+d.Y
			if nx < bounds.Min.X || ny < bounds.Min.Y || nx >= bounds.Max.X || ny >= bounds.Max.Y {
				continue
			}
			nid := ny*w + nx
			if assigned[nid] || mask.Pix[ny*mask.Stride+nx] == 0 {
				continue
			}
			si := y*out.Stride + x*4
			di := ny*out.Stride + nx*4
			copy(out.Pix[di:di+4], out.Pix[si:si+4])
			assigned[nid] = true
			queue = append(queue, nid)
		}
	}

	maxDepth := 1
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			id := y*w + x
			if id < len(insideDepth) && insideDepth[id] > maxDepth {
				maxDepth = insideDepth[id]
			}
		}
	}
	iterations := clampInt(maxDepth*3, 10, 54)
	next := cloneNRGBA(out)
	for iteration := 0; iteration < iterations; iteration++ {
		// Known pixels in both ping-pong images were initialized from src and are
		// never modified, so no full-image copy is necessary here.
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				if mask.Pix[y*mask.Stride+x] == 0 {
					continue
				}
				var sum [4]float32
				var weight float32
				for _, d := range [...]image.Point{{X: -1}, {X: 1}, {Y: -1}, {Y: 1}} {
					nx, ny := x+d.X, y+d.Y
					if nx < 0 || ny < 0 || nx >= w || ny >= h {
						continue
					}
					wt := float32(1)
					if mask.Pix[ny*mask.Stride+nx] == 0 {
						wt = 2.4
					}
					i := ny*out.Stride + nx*4
					for c := 0; c < 4; c++ {
						sum[c] += wt * float32(out.Pix[i+c])
					}
					weight += wt
				}
				if weight <= 0 {
					continue
				}
				i := y*next.Stride + x*4
				for c := 0; c < 4; c++ {
					next.Pix[i+c] = byte(clampFloat32(sum[c] / weight))
				}
			}
		}
		out, next = next, out
	}
	return out
}

// pmStructureTensorMap computes a two-pass 5-tap low-pass and immediately
// converts the tensor into the compact descriptor above. The blur uses two
// interleaved RGB buffers (6 floats/pixel peak) rather than v3's nine separate
// temporary planes.
func pmStructureTensorMap(ctx context.Context, src *image.NRGBA) (pmStructureField, error) {
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	size := w * h
	field := pmStructureField{
		strength: make([]float32, size),
		orientX:  make([]float32, size),
		orientY:  make([]float32, size),
	}
	if w == 0 || h == 0 {
		return field, nil
	}

	current := make([]float32, size*3)
	tmp := make([]float32, size*3)
	if err := parallelRowsSized(ctx, 0, h, w, func(y int) {
		for x := 0; x < w; x++ {
			si := y*src.Stride + x*4
			di := (y*w + x) * 3
			a := float32(src.Pix[si+3]) / 255
			current[di] = float32(src.Pix[si]) * a
			current[di+1] = float32(src.Pix[si+1]) * a
			current[di+2] = float32(src.Pix[si+2]) * a
		}
	}); err != nil {
		return field, err
	}
	weights := [...]float32{1, 4, 6, 4, 1}
	for pass := 0; pass < 2; pass++ {
		// Horizontal current -> tmp.
		if err := parallelRowsSized(ctx, 0, h, w, func(y int) {
			for x := 0; x < w; x++ {
				di := (y*w + x) * 3
				var r, g, b float32
				for k := -2; k <= 2; k++ {
					sx := clampInt(x+k, 0, w-1)
					si := (y*w + sx) * 3
					wt := weights[k+2]
					r += wt * current[si]
					g += wt * current[si+1]
					b += wt * current[si+2]
				}
				tmp[di], tmp[di+1], tmp[di+2] = r/16, g/16, b/16
			}
		}); err != nil {
			return field, err
		}
		// Vertical tmp -> current. Reading only tmp makes in-place reuse safe.
		if err := parallelRowsSized(ctx, 0, h, w, func(y int) {
			for x := 0; x < w; x++ {
				di := (y*w + x) * 3
				var r, g, b float32
				for k := -2; k <= 2; k++ {
					sy := clampInt(y+k, 0, h-1)
					si := (sy*w + x) * 3
					wt := weights[k+2]
					r += wt * tmp[si]
					g += wt * tmp[si+1]
					b += wt * tmp[si+2]
				}
				current[di], current[di+1], current[di+2] = r/16, g/16, b/16
			}
		}); err != nil {
			return field, err
		}
	}

	if err := parallelRowsSized(ctx, 0, h, w, func(y int) {
		ym, yp := maxInt(0, y-1), minInt(h-1, y+1)
		for x := 0; x < w; x++ {
			xm, xp := maxInt(0, x-1), minInt(w-1, x+1)
			var jxx, jxy, jyy float32
			for c := 0; c < 3; c++ {
				gx := (current[(y*w+xp)*3+c] - current[(y*w+xm)*3+c]) * 0.5
				gy := (current[(yp*w+x)*3+c] - current[(ym*w+x)*3+c]) * 0.5
				jxx += gx * gx
				jxy += gx * gy
				jyy += gy * gy
			}
			jxx, jxy, jyy = jxx/3, jxy/3, jyy/3
			trace := jxx + jyy
			disc2 := maxFloat32(0, (jxx-jyy)*(jxx-jyy)+4*jxy*jxy)
			disc := float32(math.Sqrt(float64(disc2)))
			lambda := 0.5 * (trace + disc)
			id := y*w + x
			field.strength[id] = pmStructureStrength(float32(math.Sqrt(float64(maxFloat32(0, lambda)))))
			if trace > 1e-5 {
				field.orientX[id] = (jxx - jyy) / trace
				field.orientY[id] = (2 * jxy) / trace
			}
		}
	}); err != nil {
		return field, err
	}
	return field, nil
}

func pmStructureStrength(magnitude float32) float32 {
	return pmSmoothStep(4.2, 13.0, magnitude)
}

// pmStructurePatchPenalty is intentionally arithmetic-light because it runs for
// every candidate. The expensive tensor normalization from v3 has already been
// absorbed into the prepared descriptor.
func pmStructurePatchPenalty(level *pmLevel, tx, ty, sx, sy int, unknown float32) float32 {
	if len(level.structureGuide.strength) != level.w*level.h || len(level.structureSource.strength) != level.w*level.h || level.half < 2 {
		return 0
	}
	r := maxInt(1, level.half-2)
	if r > 3 {
		r = 3
	}
	offsets := [...]image.Point{
		{}, {X: -r}, {X: r}, {Y: -r}, {Y: r},
		{X: -r, Y: -r}, {X: r, Y: -r}, {X: -r, Y: r}, {X: r, Y: r},
	}
	var total, weight float32
	for _, o := range offsets {
		tx2, ty2 := tx+o.X, ty+o.Y
		sx2, sy2 := sx+o.X, sy+o.Y
		if tx2 < 0 || ty2 < 0 || tx2 >= level.w || ty2 >= level.h || sx2 < 0 || sy2 < 0 || sx2 >= level.w || sy2 >= level.h {
			continue
		}
		tid := ty2*level.w + tx2
		sid := sy2*level.w + sx2
		ts := level.structureGuide.strength[tid]
		ss := level.structureSource.strength[sid]
		importance := maxFloat32(ts, ss)
		if importance < 0.035 {
			continue
		}
		presence := ss - ts
		cost := presence * presence
		if ts > 0.12 && ss > 0.12 {
			dx := level.structureGuide.orientX[tid] - level.structureSource.orientX[sid]
			dy := level.structureGuide.orientY[tid] - level.structureSource.orientY[sid]
			// Squared distance between coherence-weighted double-angle vectors is
			// zero for the same undirected edge orientation and rises smoothly for
			// shifted/crossing structure.
			cost += 0.45 * (dx*dx + dy*dy) * minFloat32(ts, ss)
		}
		wt := 0.25 + 0.75*importance
		total += wt * cost
		weight += wt
	}
	if weight <= 1e-6 {
		return 0
	}
	return (55 + 145*unknown) * total / weight
}

func pmPixelColorDifference(src *image.NRGBA, x0, y0, x1, y1 int) float32 {
	i0 := y0*src.Stride + x0*4
	i1 := y1*src.Stride + x1*4
	dr := float32(src.Pix[i0]) - float32(src.Pix[i1])
	dg := float32(src.Pix[i0+1]) - float32(src.Pix[i1+1])
	db := float32(src.Pix[i0+2]) - float32(src.Pix[i1+2])
	return float32(math.Sqrt(float64(0.30*dr*dr + 0.50*dg*dg + 0.20*db*db)))
}
