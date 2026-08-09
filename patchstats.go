package main

import (
	"image"
	"math"
)

// pmPatchStats describes the low-order appearance of a complete patch. Target
// statistics are confidence weighted, so masked pixels do not bias the local
// color transform toward the material being removed.
type pmPatchStats struct {
	mean           [3]float32
	meanGradient   [2]float32
	lumaStd        float32
	chromaStd      float32
	gradientEnergy float32
	weight         float32
}

func buildPMPatchStats(src *image.NRGBA, features []pmFeature, confidence []float32, confidenceStride, patchSize int, stats []pmPatchStats, scratch []float32) ([]pmPatchStats, []float32) {
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	stride := w + 1
	area := stride * (h + 1)
	if cap(scratch) < area*10 {
		scratch = make([]float32, area*10)
	} else {
		scratch = scratch[:area*10]
		clear(scratch)
	}
	data := scratch
	weightIntegral := data[:area]
	rIntegral := data[area : area*2]
	gIntegral := data[area*2 : area*3]
	bIntegral := data[area*3 : area*4]
	lumaSqIntegral := data[area*4 : area*5]
	gxIntegral := data[area*5 : area*6]
	gyIntegral := data[area*6 : area*7]
	gradientSqIntegral := data[area*7 : area*8]
	cbSqIntegral := data[area*8 : area*9]
	crSqIntegral := data[area*9:]

	for y := 0; y < h; y++ {
		var rowWeight, rowR, rowG, rowB, rowLumaSq float32
		var rowGX, rowGY, rowGradientSq, rowCbSq, rowCrSq float32
		for x := 0; x < w; x++ {
			i := y*src.Stride + x*4
			weight := float32(src.Pix[i+3]) / 255
			if confidence != nil {
				weight *= confidence[y*confidenceStride+x]
			}
			// Centering keeps squared integral values small enough for stable
			// float32 variance while halving the descriptor workspace.
			r := float32(src.Pix[i]) - 128
			g := float32(src.Pix[i+1]) - 128
			b := float32(src.Pix[i+2]) - 128
			luma := 0.299*r + 0.587*g + 0.114*b
			gx := features[y*w+x].gx
			gy := features[y*w+x].gy
			cb := b - luma
			cr := r - luma
			rowWeight += weight
			rowR += weight * r
			rowG += weight * g
			rowB += weight * b
			rowLumaSq += weight * luma * luma
			rowGX += weight * gx
			rowGY += weight * gy
			rowGradientSq += weight * (gx*gx + gy*gy)
			rowCbSq += weight * cb * cb
			rowCrSq += weight * cr * cr
			dst := (y+1)*stride + x + 1
			above := y*stride + x + 1
			weightIntegral[dst] = weightIntegral[above] + rowWeight
			rIntegral[dst] = rIntegral[above] + rowR
			gIntegral[dst] = gIntegral[above] + rowG
			bIntegral[dst] = bIntegral[above] + rowB
			lumaSqIntegral[dst] = lumaSqIntegral[above] + rowLumaSq
			gxIntegral[dst] = gxIntegral[above] + rowGX
			gyIntegral[dst] = gyIntegral[above] + rowGY
			gradientSqIntegral[dst] = gradientSqIntegral[above] + rowGradientSq
			cbSqIntegral[dst] = cbSqIntegral[above] + rowCbSq
			crSqIntegral[dst] = crSqIntegral[above] + rowCrSq
		}
	}

	if cap(stats) < w*h {
		stats = make([]pmPatchStats, w*h)
	} else {
		stats = stats[:w*h]
		clear(stats)
	}
	half := patchSize / 2
	for y := half; y < h-half; y++ {
		for x := half; x < w-half; x++ {
			x0, y0 := x-half, y-half
			x1, y1 := x+half+1, y+half+1
			weight := pmIntegralFloatRect(weightIntegral, stride, x0, y0, x1, y1)
			if weight < 0.5 {
				continue
			}
			stat := &stats[y*w+x]
			stat.weight = weight
			stat.mean[0] = pmIntegralFloatRect(rIntegral, stride, x0, y0, x1, y1)/weight + 128
			stat.mean[1] = pmIntegralFloatRect(gIntegral, stride, x0, y0, x1, y1)/weight + 128
			stat.mean[2] = pmIntegralFloatRect(bIntegral, stride, x0, y0, x1, y1)/weight + 128
			meanLuma := 0.299*stat.mean[0] + 0.587*stat.mean[1] + 0.114*stat.mean[2] - 128
			meanSquare := pmIntegralFloatRect(lumaSqIntegral, stride, x0, y0, x1, y1) / weight
			variance := maxFloat32(0, meanSquare-meanLuma*meanLuma)
			stat.lumaStd = float32(math.Sqrt(float64(variance)))
			stat.meanGradient[0] = pmIntegralFloatRect(gxIntegral, stride, x0, y0, x1, y1) / weight
			stat.meanGradient[1] = pmIntegralFloatRect(gyIntegral, stride, x0, y0, x1, y1) / weight
			gradientSquare := pmIntegralFloatRect(gradientSqIntegral, stride, x0, y0, x1, y1) / weight
			stat.gradientEnergy = float32(math.Sqrt(float64(maxFloat32(0, gradientSquare))))
			meanLuma += 128
			meanCb := stat.mean[2] - meanLuma
			meanCr := stat.mean[0] - meanLuma
			chromaVariance := pmIntegralFloatRect(cbSqIntegral, stride, x0, y0, x1, y1)/weight - meanCb*meanCb
			chromaVariance += pmIntegralFloatRect(crSqIntegral, stride, x0, y0, x1, y1)/weight - meanCr*meanCr
			stat.chromaStd = float32(math.Sqrt(float64(maxFloat32(0, chromaVariance*0.5))))
		}
	}
	return stats, scratch
}

func pmIntegralFloatRect(integral []float32, stride, x0, y0, x1, y1 int) float32 {
	return integral[y1*stride+x1] - integral[y0*stride+x1] -
		integral[y1*stride+x0] + integral[y0*stride+x0]
}

func pmGainBias(target, source pmPatchStats) (gain float32, bias [3]float32) {
	gain = 1
	if target.weight >= 1 && source.weight >= 1 && source.lumaStd > 1 {
		gain = target.lumaStd / source.lumaStd
		gain = minFloat32(1.10, maxFloat32(0.90, gain))
	}
	// A flat source patch has no reliable contrast model. In particular, a
	// partially unknown target can report an artificial mean shift from its
	// provisional pixels; applying that shift would darken an otherwise exact
	// constant-color reconstruction.
	if target.weight >= 1 && source.weight >= 1 && source.lumaStd > 1 {
		for channel := range bias {
			bias[channel] = target.mean[channel] - gain*source.mean[channel]
			bias[channel] = minFloat32(13, maxFloat32(-13, bias[channel]))
		}
	}
	return gain, bias
}

// pmGuidedTargetStats anchors synthesis color to the mask-independent healed
// guide while allowing progressively reconstructed structure and texture to
// remain in the target descriptor. A target patch with no known support uses
// the guide outright; reconstructed pixels earn influence gradually rather
// than making an arbitrary first-round source self-validating.
func pmGuidedTargetStats(target, guide pmPatchStats) pmPatchStats {
	if guide.weight < 0.5 {
		return target
	}
	if target.weight < 0.5 {
		return guide
	}
	support := minFloat32(1, maxFloat32(0, target.weight/guide.weight))
	for channel := range target.mean {
		target.mean[channel] = guide.mean[channel]*(1-support) + target.mean[channel]*support
	}
	target.weight = maxFloat32(target.weight, guide.weight)
	return target
}

func minFloat32(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

func maxFloat32(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}
