package main

import "image"

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
				coarseX := minInt(coarse.w-1, x*coarse.w/fine.w)
				coarseY := minInt(coarse.h-1, y*coarse.h/fine.h)
				ci := coarseY*coarse.w + coarseX
				coarseConfidence := coarse.confidence[ci]
				if known < 1 && coarseConfidence > 0 {
					for channel := 0; channel < 4; channel++ {
						fine.pixels[fi*4+channel] = known*fine.pixels[fi*4+channel] +
							(1-known)*coarse.pixels[ci*4+channel]
					}
					fine.confidence[fi] = known + (1-known)*coarseConfidence
				}
			}
		}
	}

	out := cloneNRGBA(src)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			maskAlpha := mask.Pix[y*mask.Stride+x]
			if maskAlpha == 0 {
				continue
			}
			i := y*w + x
			oi := y*out.Stride + x*4
			a := float32(maskAlpha) / 255
			for channel := 0; channel < 4; channel++ {
				value := (1-a)*float32(src.Pix[oi+channel]) + a*base.pixels[i*4+channel]
				out.Pix[oi+channel] = byte(clampFloat32(value))
			}
		}
	}
	return out
}
