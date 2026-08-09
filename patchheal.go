package main

import (
	"image"
	"math"
)

type pmHealLayer struct {
	w, h       int
	pixels     []float32
	confidence []float32
}

// pmHealedWorking constructs a low-frequency target without consulting pixels
// inside the hard mask. It is an alpha-normalized pull/push pyramid: known
// color is reduced with its confidence, then progressively expanded back into
// unsupported pixels. PatchMatch can therefore search against a continuous
// tonal estimate without preserving the dust being removed.
func pmHealedWorking(src *image.NRGBA, mask *image.Alpha) *image.NRGBA {
	return pmHealedWorkingFromBase(src, mask, src)
}

// pmHealedWorkingFromBase builds the pull/push fallback and then replaces it
// where possible with a directional boundary continuation. The latter matters
// near strong nearby features: an isotropic pyramid can drag a bright object
// above a hole into an otherwise dark horizontal band, while opposite boundary
// samples along the band describe the missing appearance accurately.
func pmHealedWorkingFromBase(src *image.NRGBA, mask *image.Alpha, appearance *image.NRGBA) *image.NRGBA {
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	base := pmHealLayer{
		w:          w,
		h:          h,
		pixels:     make([]float32, w*h*4),
		confidence: make([]float32, w*h),
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := y*w + x
			si := y*src.Stride + x*4
			base.confidence[i] = float32(255-mask.Pix[y*mask.Stride+x]) / 255
			for channel := 0; channel < 4; channel++ {
				base.pixels[i*4+channel] = float32(src.Pix[si+channel])
			}
		}
	}

	layers := []pmHealLayer{base}
	for layers[len(layers)-1].w > 1 || layers[len(layers)-1].h > 1 {
		fine := &layers[len(layers)-1]
		coarseW := maxInt(1, (fine.w+1)/2)
		coarseH := maxInt(1, (fine.h+1)/2)
		coarse := pmHealLayer{
			w:          coarseW,
			h:          coarseH,
			pixels:     make([]float32, coarseW*coarseH*4),
			confidence: make([]float32, coarseW*coarseH),
		}
		for y := 0; y < coarseH; y++ {
			for x := 0; x < coarseW; x++ {
				var sums [4]float32
				var confidenceSum float32
				var samples float32
				for fy := y * 2; fy < minInt(fine.h, y*2+2); fy++ {
					for fx := x * 2; fx < minInt(fine.w, x*2+2); fx++ {
						fi := fy*fine.w + fx
						confidence := fine.confidence[fi]
						confidenceSum += confidence
						samples++
						for channel := range sums {
							sums[channel] += confidence * fine.pixels[fi*4+channel]
						}
					}
				}
				ci := y*coarseW + x
				if confidenceSum > 0 {
					for channel := range sums {
						coarse.pixels[ci*4+channel] = sums[channel] / confidenceSum
					}
				}
				coarse.confidence[ci] = minFloat32(1, confidenceSum/samples)
			}
		}
		layers = append(layers, coarse)
	}

	for level := len(layers) - 2; level >= 0; level-- {
		fine := &layers[level]
		coarse := &layers[level+1]
		for y := 0; y < fine.h; y++ {
			for x := 0; x < fine.w; x++ {
				fi := y*fine.w + x
				known := fine.confidence[fi]
				x0, x1, tx := pmHealSampleAxis(x, fine.w, coarse.w)
				y0, y1, ty := pmHealSampleAxis(y, fine.h, coarse.h)
				samples := [4]int{y0*coarse.w + x0, y0*coarse.w + x1, y1*coarse.w + x0, y1*coarse.w + x1}
				weights := [4]float32{(1 - tx) * (1 - ty), tx * (1 - ty), (1 - tx) * ty, tx * ty}
				var coarseConfidence float32
				var coarseColor [4]float32
				for sampleIndex, ci := range samples {
					weight := weights[sampleIndex] * coarse.confidence[ci]
					coarseConfidence += weight
					for channel := range coarseColor {
						coarseColor[channel] += weight * coarse.pixels[ci*4+channel]
					}
				}
				if known < 1 && coarseConfidence > 0 {
					for channel := range coarseColor {
						coarseValue := coarseColor[channel] / coarseConfidence
						fine.pixels[fi*4+channel] = known*fine.pixels[fi*4+channel] +
							(1-known)*coarseValue
					}
					fine.confidence[fi] = known + (1-known)*coarseConfidence
				}
			}
		}
	}
	pmApplyDirectionalGuide(&base, mask, appearance)

	out := cloneNRGBA(src)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			maskAlpha := mask.Pix[y*mask.Stride+x]
			if maskAlpha == 0 {
				continue
			}
			i := y*w + x
			oi := y*out.Stride + x*4
			for channel := 0; channel < 4; channel++ {
				out.Pix[oi+channel] = byte(clampFloat32(base.pixels[i*4+channel]))
			}
		}
	}
	return out
}

// pmApplyDirectionalGuide continues color between opposite known boundaries.
// For every masked run it considers the horizontal and vertical pairs and
// keeps the direction whose endpoints change least per pixel. This is a cheap
// edge-aware appearance estimate: it follows bands and gradients through a
// brush mark without inventing texture (texture still comes from PatchMatch).
// base.confidence is scratch after pull/push and is reused to avoid another
// full-frame allocation.
func pmApplyDirectionalGuide(base *pmHealLayer, mask *image.Alpha, appearance *image.NRGBA) {
	if base == nil || appearance == nil || base.w == 0 || base.h == 0 {
		return
	}
	for i := range base.confidence {
		base.confidence[i] = float32(math.Inf(1))
	}

	for y := 0; y < base.h; y++ {
		for x := 0; x < base.w; {
			if mask.Pix[y*mask.Stride+x] == 0 {
				x++
				continue
			}
			start := x
			for x < base.w && mask.Pix[y*mask.Stride+x] != 0 {
				x++
			}
			end := x
			if start == 0 || end == base.w {
				continue
			}
			left, right := start-1, end
			leftColor := pmDirectionalBoundarySample(appearance, mask, left, y, false)
			rightColor := pmDirectionalBoundarySample(appearance, mask, right, y, false)
			score := pmDirectionalPairScore(leftColor, rightColor, right-left)
			span := float32(right - left)
			for fillX := start; fillX < end; fillX++ {
				fraction := float32(fillX-left) / span
				pmSetDirectionalPixel(base, fillX, y, leftColor, rightColor, fraction, score)
			}
		}
	}

	for x := 0; x < base.w; x++ {
		for y := 0; y < base.h; {
			if mask.Pix[y*mask.Stride+x] == 0 {
				y++
				continue
			}
			start := y
			for y < base.h && mask.Pix[y*mask.Stride+x] != 0 {
				y++
			}
			end := y
			if start == 0 || end == base.h {
				continue
			}
			top, bottom := start-1, end
			topColor := pmDirectionalBoundarySample(appearance, mask, x, top, true)
			bottomColor := pmDirectionalBoundarySample(appearance, mask, x, bottom, true)
			score := pmDirectionalPairScore(topColor, bottomColor, bottom-top)
			span := float32(bottom - top)
			for fillY := start; fillY < end; fillY++ {
				id := fillY*base.w + x
				if score >= base.confidence[id] {
					continue
				}
				fraction := float32(fillY-top) / span
				pmSetDirectionalPixel(base, x, fillY, topColor, bottomColor, fraction, score)
			}
		}
	}
}

// pmSmoothedDirectionalGuide removes scanline-correlated variation from the
// low-frequency reconstruction base without changing the unsmoothed guide used
// by NNF search. Original pixels provide the Dirichlet boundary, while an
// edge-aware screened term carries strong continued color edges across the
// domain. Weak guide variations diffuse freely; large RGB discontinuities act
// as barriers, so smoothing cannot wash a reconstructed object edge across the
// brush mark.
func pmSmoothedDirectionalGuide(src *image.NRGBA, mask *image.Alpha) *image.NRGBA {
	out := cloneNRGBA(src)
	bounds := maskBounds(mask)
	if bounds.Empty() {
		return out
	}
	bw, bh := bounds.Dx(), bounds.Dy()
	values := make([]float32, bw*bh*4)
	domain := make([]bool, bw*bh)
	conductance := make([]float32, bw*bh*4)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			local := (y-bounds.Min.Y)*bw + x - bounds.Min.X
			domain[local] = mask.Pix[y*mask.Stride+x] != 0
			si := y*src.Stride + x*4
			for channel := 0; channel < 4; channel++ {
				values[local*4+channel] = float32(src.Pix[si+channel])
			}
			neighbors := [4]image.Point{{X: x - 1, Y: y}, {X: x + 1, Y: y}, {X: x, Y: y - 1}, {X: x, Y: y + 1}}
			for direction, neighbor := range neighbors {
				nx := clampInt(neighbor.X, 0, src.Bounds().Dx()-1)
				ny := clampInt(neighbor.Y, 0, src.Bounds().Dy()-1)
				conductance[local*4+direction] = pmGuideConductance(src, x, y, nx, ny)
			}
		}
	}

	// This is the screened 2D analogue of Adobe Heal's multilevel offset
	// harmonization. The weak data term retains useful directional edge
	// continuation, while the weighted Laplacian removes minor row/column
	// variation without mixing across a real color edge. Red-black SOR mirrors
	// the solver organization visible in AdbePM.
	const screen = float32(0.025)
	maxAxis := maxInt(bw, bh)
	iterations := clampInt(maxAxis*2, 40, 160)
	omega := float32(2 / (1 + math.Sin(math.Pi/float64(maxInt(2, maxAxis)))))
	omega = minFloat32(1.88, maxFloat32(1.20, omega))
	for iteration := 0; iteration < iterations; iteration++ {
		var maximumDelta float32
		for parity := 0; parity < 2; parity++ {
			for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
				for x := bounds.Min.X; x < bounds.Max.X; x++ {
					if (x+y)&1 != parity {
						continue
					}
					local := (y-bounds.Min.Y)*bw + x - bounds.Min.X
					if !domain[local] {
						continue
					}
					guideIndex := y*src.Stride + x*4
					for channel := 0; channel < 3; channel++ {
						neighborSum := float32(0)
						weightSum := float32(0)
						neighbors := [4]image.Point{{X: x - 1, Y: y}, {X: x + 1, Y: y}, {X: x, Y: y - 1}, {X: x, Y: y + 1}}
						for direction, neighbor := range neighbors {
							nx := clampInt(neighbor.X, 0, src.Bounds().Dx()-1)
							ny := clampInt(neighbor.Y, 0, src.Bounds().Dy()-1)
							weight := conductance[local*4+direction]
							weightSum += weight
							if nx >= bounds.Min.X && nx < bounds.Max.X && ny >= bounds.Min.Y && ny < bounds.Max.Y {
								nLocal := (ny-bounds.Min.Y)*bw + nx - bounds.Min.X
								if domain[nLocal] {
									neighborSum += weight * values[nLocal*4+channel]
									continue
								}
							}
							neighborSum += weight * float32(src.Pix[ny*src.Stride+nx*4+channel])
						}
						index := local*4 + channel
						candidate := (neighborSum + screen*float32(src.Pix[guideIndex+channel])) / (weightSum + screen)
						previous := values[index]
						updated := previous + omega*(candidate-previous)
						values[index] = updated
						maximumDelta = maxFloat32(maximumDelta, float32(math.Abs(float64(updated-previous))))
					}
				}
			}
		}
		if iteration >= 24 && maximumDelta < 0.015 {
			break
		}
	}

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if mask.Pix[y*mask.Stride+x] == 0 {
				continue
			}
			local := (y-bounds.Min.Y)*bw + x - bounds.Min.X
			oi := y*out.Stride + x*4
			for channel := 0; channel < 3; channel++ {
				out.Pix[oi+channel] = byte(clampFloat32(values[local*4+channel]))
			}
		}
	}
	return out
}

// pmGuideConductance distinguishes the low-amplitude scanline variation that
// should be removed from a hard structural edge that must survive healing. A
// biweight response leaves small differences almost untouched, then rapidly
// closes as the average RGB step grows. The nonzero floor keeps every masked
// component connected and avoids isolated numerical plateaus.
func pmGuideConductance(src *image.NRGBA, x0, y0, x1, y1 int) float32 {
	i0 := y0*src.Stride + x0*4
	i1 := y1*src.Stride + x1*4
	var differenceSquared float32
	for channel := 0; channel < 3; channel++ {
		difference := float32(src.Pix[i0+channel]) - float32(src.Pix[i1+channel])
		differenceSquared += difference * difference
	}
	differenceSquared /= 3
	const (
		edgeScale = float32(12 * 12)
		edgeFloor = float32(0.002)
	)
	normalized := differenceSquared / edgeScale
	return edgeFloor + (1-edgeFloor)/(1+normalized*normalized)
}

func pmDirectionalBoundarySample(src *image.NRGBA, mask *image.Alpha, x, y int, horizontal bool) [4]float32 {
	var result [4]float32
	var values [4][9]byte
	samples := 0
	for offset := -4; offset <= 4; offset++ {
		sx, sy := x, y
		if horizontal {
			sx = clampInt(x+offset, 0, src.Bounds().Dx()-1)
		} else {
			sy = clampInt(y+offset, 0, src.Bounds().Dy()-1)
		}
		if mask.Pix[sy*mask.Stride+sx] != 0 {
			continue
		}
		i := sy*src.Stride + sx*4
		for channel := range result {
			values[channel][samples] = src.Pix[i+channel]
		}
		samples++
	}
	if samples == 0 {
		i := y*src.Stride + x*4
		for channel := range result {
			result[channel] = float32(src.Pix[i+channel])
		}
		return result
	}
	for channel := range result {
		for i := 1; i < samples; i++ {
			value := values[channel][i]
			j := i - 1
			for ; j >= 0 && values[channel][j] > value; j-- {
				values[channel][j+1] = values[channel][j]
			}
			values[channel][j+1] = value
		}
		if samples%2 == 1 {
			result[channel] = float32(values[channel][samples/2])
		} else {
			result[channel] = 0.5 * float32(int(values[channel][samples/2-1])+int(values[channel][samples/2]))
		}
	}
	// A tangent median rejects dust and grain, but it can also erase a narrow
	// line whose width is smaller than the nine-pixel sampling window. Preserve
	// a strong center discontinuity here; the opposite boundary sample must
	// independently agree with it before this direction can win the pair score,
	// so an isolated speck still loses to the clean orthogonal continuation.
	centerIndex := y*src.Stride + x*4
	var centerDifferenceSquared float32
	for channel := 0; channel < 3; channel++ {
		difference := float32(src.Pix[centerIndex+channel]) - result[channel]
		centerDifferenceSquared += difference * difference
	}
	if centerDifferenceSquared/3 > 30*30 {
		for channel := range result {
			result[channel] = float32(src.Pix[centerIndex+channel])
		}
	}
	return result
}

func pmDirectionalPairScore(a, b [4]float32, distance int) float32 {
	var difference float32
	for channel := 0; channel < 3; channel++ {
		difference += float32(math.Abs(float64(a[channel] - b[channel])))
	}
	return difference / (3 * float32(maxInt(1, distance)))
}

func pmSetDirectionalPixel(base *pmHealLayer, x, y int, a, b [4]float32, fraction, score float32) {
	id := y*base.w + x
	pixel := pmDirectionalPixel(a, b, fraction)
	for channel := 0; channel < 4; channel++ {
		base.pixels[id*4+channel] = pixel[channel]
	}
	base.confidence[id] = score
}

func pmDirectionalPixel(a, b [4]float32, fraction float32) [4]float32 {
	var pixel [4]float32
	for channel := range pixel {
		pixel[channel] = (1-fraction)*a[channel] + fraction*b[channel]
	}
	return pixel
}

func pmHealSampleAxis(position, fineSize, coarseSize int) (lower, upper int, fraction float32) {
	coordinate := (float32(position)+0.5)*float32(coarseSize)/float32(fineSize) - 0.5
	if coordinate <= 0 {
		return 0, 0, 0
	}
	if coordinate >= float32(coarseSize-1) {
		last := coarseSize - 1
		return last, last, 0
	}
	lower = int(coordinate)
	return lower, lower + 1, coordinate - float32(lower)
}
