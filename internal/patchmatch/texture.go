package patchmatch

import (
	"image"
	"math"
)

// pmPrepareTextureModel builds a mask-independent description of the fine
// texture around the edit. RGB reconstructed during EM is deliberately not
// used for this field: otherwise patch averaging can make a smooth first pass
// self-validating on the next EM round.
func pmPrepareTextureModel(level *pmLevel) {
	if level == nil || level.src == nil || level.w == 0 || level.h == 0 {
		return
	}
	radius := level.half
	if radius > 3 {
		radius = 3
	}
	if radius < 1 {
		radius = 1
	}
	level.textureEnergy = pmTextureEnergyMap(level.src, level.sourceMask, radius)
	level.textureGuide = pmFillTextureGuide(level.textureEnergy, level.mask, level.insideDepth)
}

// pmTextureEnergyMap measures local RMS signed-gradient energy while completely
// excluding painted pixels. It responds strongly to scanner grain, moulded
// plastic speckle, paper fibres, and halftone dots, but remains low on a truly
// smooth ink/gradient field. A small integral image makes the descriptor cheap
// to query for every possible PatchMatch source center.
func pmTextureEnergyMap(src *image.NRGBA, exclusion *image.Alpha, radius int) []float32 {
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	stride := w + 1
	energyIntegral := make([]float32, stride*(h+1))
	weightIntegral := make([]float32, stride*(h+1))
	luma := func(x, y int) float32 {
		x = clampInt(x, 0, w-1)
		y = clampInt(y, 0, h-1)
		i := y*src.Stride + x*4
		return 0.299*float32(src.Pix[i]) + 0.587*float32(src.Pix[i+1]) + 0.114*float32(src.Pix[i+2])
	}
	known := func(x, y int) bool {
		if x < 0 || y < 0 || x >= w || y >= h {
			return false
		}
		if exclusion == nil {
			return true
		}
		return exclusion.Pix[y*exclusion.Stride+x] == 0
	}

	for y := 0; y < h; y++ {
		var rowEnergy, rowWeight float32
		for x := 0; x < w; x++ {
			var e, wt float32
			if known(x, y) && known(x-1, y) && known(x+1, y) && known(x, y-1) && known(x, y+1) {
				gx := (luma(x+1, y) - luma(x-1, y)) * 0.5
				gy := (luma(x, y+1) - luma(x, y-1)) * 0.5
				e = gx*gx + gy*gy
				wt = 1
			}
			rowEnergy += e
			rowWeight += wt
			d := (y+1)*stride + x + 1
			energyIntegral[d] = energyIntegral[y*stride+x+1] + rowEnergy
			weightIntegral[d] = weightIntegral[y*stride+x+1] + rowWeight
		}
	}

	out := make([]float32, w*h)
	for y := 0; y < h; y++ {
		y0, y1 := maxInt(0, y-radius), minInt(h, y+radius+1)
		for x := 0; x < w; x++ {
			x0, x1 := maxInt(0, x-radius), minInt(w, x+radius+1)
			weight := pmScalarIntegralRect(weightIntegral, stride, x0, y0, x1, y1)
			if weight < 1 {
				continue
			}
			sum := pmScalarIntegralRect(energyIntegral, stride, x0, y0, x1, y1)
			out[y*w+x] = float32(math.Sqrt(float64(maxFloat32(0, sum/weight))))
		}
	}
	return out
}

func pmScalarIntegralRect(integral []float32, stride, x0, y0, x1, y1 int) float32 {
	return integral[y1*stride+x1] - integral[y0*stride+x1] - integral[y1*stride+x0] + integral[y0*stride+x0]
}

// pmFillTextureGuide continues the measured surrounding texture magnitude
// through the hole. It first performs a nearest-boundary propagation and then
// a few harmonic smoothing sweeps. Only the scalar texture magnitude is
// diffused; no RGB or source pixels are invented here.
func pmFillTextureGuide(source []float32, mask *image.Alpha, insideDepth []int) []float32 {
	w, h := mask.Bounds().Dx(), mask.Bounds().Dy()
	out := append([]float32(nil), source...)
	bounds := maskBounds(mask)
	if bounds.Empty() {
		return out
	}

	assigned := make([]bool, w*h)
	queue := make([]int, 0, bounds.Dx()*bounds.Dy())
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if mask.Pix[y*mask.Stride+x] == 0 {
				continue
			}
			var sum float32
			count := 0
			for ny := maxInt(0, y-1); ny <= minInt(h-1, y+1); ny++ {
				for nx := maxInt(0, x-1); nx <= minInt(w-1, x+1); nx++ {
					if nx == x && ny == y || mask.Pix[ny*mask.Stride+nx] != 0 {
						continue
					}
					sum += source[ny*w+nx]
					count++
				}
			}
			if count != 0 {
				id := y*w + x
				out[id] = sum / float32(count)
				assigned[id] = true
				queue = append(queue, id)
			}
		}
	}

	// Propagate boundary values to the interior. For the brush-sized masks this
	// is both faster and less globally biased than seeding a full-image Voronoi
	// transform from every known pixel.
	for head := 0; head < len(queue); head++ {
		id := queue[head]
		x, y := id%w, id/w
		neighbors := [4]int{-1, -1, -1, -1}
		if x > bounds.Min.X {
			neighbors[0] = id - 1
		}
		if x+1 < bounds.Max.X {
			neighbors[1] = id + 1
		}
		if y > bounds.Min.Y {
			neighbors[2] = id - w
		}
		if y+1 < bounds.Max.Y {
			neighbors[3] = id + w
		}
		for _, next := range neighbors {
			if next < 0 || assigned[next] {
				continue
			}
			nx, ny := next%w, next/w
			if mask.Pix[ny*mask.Stride+nx] == 0 {
				continue
			}
			out[next] = out[id]
			assigned[next] = true
			queue = append(queue, next)
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
	iterations := clampInt(maxDepth*2, 4, 24)
	next := append([]float32(nil), out...)
	for iteration := 0; iteration < iterations; iteration++ {
		// Known values in both buffers are immutable; every masked value is
		// overwritten below, so a full ROI copy per relaxation sweep is wasted.
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				if mask.Pix[y*mask.Stride+x] == 0 {
					continue
				}
				var sum, weight float32
				for _, d := range [...]image.Point{{X: -1}, {X: 1}, {Y: -1}, {Y: 1}} {
					nx, ny := x+d.X, y+d.Y
					if nx < 0 || ny < 0 || nx >= w || ny >= h {
						continue
					}
					// Known boundary values are stronger constraints than provisional
					// values already propagated inside the mask.
					wt := float32(1)
					if mask.Pix[ny*mask.Stride+nx] == 0 {
						wt = 2
					}
					sum += wt * out[ny*w+nx]
					weight += wt
				}
				if weight != 0 {
					next[y*w+x] = sum / weight
				}
			}
		}
		out, next = next, out
	}
	return out
}

// pmTextureStrength converts a gradient RMS in 8-bit pixel units into a soft
// amount used by confidence suppression and final detail synthesis. Scanner
// noise around 0.5-1 remains mostly untouched; obvious print/plastic grain is
// close to one.
func pmTextureStrength(energy float32) float32 {
	const low = 0.9
	const high = 3.4
	t := (energy - low) / (high - low)
	t = minFloat32(1, maxFloat32(0, t))
	return t * t * (3 - 2*t)
}
