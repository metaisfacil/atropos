package main

import (
	"image"
	"math"
	"unsafe"
)

// pmPackedPlanes stores premultiplied channels in structure-of-arrays form.
// Rows include at least eight padding floats so SIMD tail loads are always
// within allocated memory.
type pmPackedPlanes struct {
	channel [4][]float32
	luma    []float32
	data    []float32
	stride  int
}

// pmKernelArgs is deliberately pointer-only and has a stable layout shared by
// the Go reference kernel and the architecture assembly kernels.
type pmKernelArgs struct {
	targetR    *float32 // 0
	targetG    *float32 // 8
	targetB    *float32 // 16
	targetA    *float32 // 24
	sourceR    *float32 // 32
	sourceG    *float32 // 40
	sourceB    *float32 // 48
	sourceA    *float32 // 56
	confidence *float32 // 64
	stride     int      // 72, in float32 elements
	patchSize  int      // 80
	limit      float32  // 88, raw weighted SSD early-exit threshold
}

func packPMPixels(src *image.NRGBA) pmPackedPlanes {
	return packPMPixelsInto(src, pmPackedPlanes{})
}

func packPMPixelsInto(src *image.NRGBA, planes pmPackedPlanes) pmPackedPlanes {
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	stride := (w + 15) &^ 7
	required := stride * h * 5
	if cap(planes.data) < required {
		planes.data = make([]float32, required)
	} else {
		planes.data = planes.data[:required]
		clear(planes.data)
	}
	planes.stride = stride
	for channel := range planes.channel {
		start := channel * stride * h
		planes.channel[channel] = planes.data[start : start+stride*h]
	}
	planes.luma = planes.data[4*stride*h:]
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			srcIndex := y*src.Stride + x*4
			dstIndex := y*stride + x
			alpha := float32(src.Pix[srcIndex+3])
			scale := alpha / 255
			planes.channel[0][dstIndex] = float32(src.Pix[srcIndex]) * scale
			planes.channel[1][dstIndex] = float32(src.Pix[srcIndex+1]) * scale
			planes.channel[2][dstIndex] = float32(src.Pix[srcIndex+2]) * scale
			planes.channel[3][dstIndex] = alpha * 0.35
			planes.luma[dstIndex] = 0.299*planes.channel[0][dstIndex] +
				0.587*planes.channel[1][dstIndex] +
				0.114*planes.channel[2][dstIndex]
		}
	}
	return planes
}

func buildPMFeaturesPacked(planes *pmPackedPlanes, w, h int, features []pmFeature) []pmFeature {
	if cap(features) < w*h {
		features = make([]pmFeature, w*h)
	} else {
		features = features[:w*h]
	}
	for y := 0; y < h; y++ {
		up := maxInt(0, y-1) * planes.stride
		row := y * planes.stride
		down := minInt(h-1, y+1) * planes.stride
		for x := 0; x < w; x++ {
			left := maxInt(0, x-1)
			right := minInt(w-1, x+1)
			features[y*w+x] = pmFeature{
				gx: (planes.luma[row+right] - planes.luma[row+left]) * 0.5,
				gy: (planes.luma[down+x] - planes.luma[up+x]) * 0.5,
			}
		}
	}
	return features
}

func packPMConfidence(mask *image.Alpha) (values []float32, stride int, integral []float32) {
	w, h := mask.Bounds().Dx(), mask.Bounds().Dy()
	stride = (w + 15) &^ 7
	values = make([]float32, stride*h)
	integral = make([]float32, (w+1)*(h+1))
	updatePMConfidence(mask, values, stride, integral, 0)
	return values, stride, integral
}

func updatePMConfidence(mask *image.Alpha, values []float32, stride int, integral []float32, accepted float32) {
	w, h := mask.Bounds().Dx(), mask.Bounds().Dy()
	clear(values)
	clear(integral)
	for y := 0; y < h; y++ {
		var rowSum float32
		for x := 0; x < w; x++ {
			confidence := float32(255-mask.Pix[y*mask.Stride+x]) / 255
			if accepted > 0 && confidence < 1 {
				confidence += (1 - confidence) * accepted
			}
			values[y*stride+x] = confidence
			rowSum += confidence
			integral[(y+1)*(w+1)+x+1] = integral[y*(w+1)+x+1] + rowSum
		}
	}
}

func pmPatchCost(level *pmLevel, target *pmPackedPlanes, targetStats []pmPatchStats, tx, ty int, source pmPoint, bestCost float32) float32 {
	sx, sy := int(source.x), int(source.y)
	targetStat := targetStats[ty*level.w+tx]
	sourceStat := level.srcStats[sy*level.w+sx]
	if targetStat.weight < 0.5 {
		// A fully unsupported patch has no defensible appearance descriptor
		// yet. Its healed estimate becomes comparable only after a coherent
		// reconstruction raises the progressive confidence.
		targetStat = sourceStat
	}
	dgx := targetStat.meanGradient[0] - sourceStat.meanGradient[0]
	dgy := targetStat.meanGradient[1] - sourceStat.meanGradient[1]
	detailDifference := targetStat.gradientEnergy - sourceStat.gradientEnergy
	varianceDifference := targetStat.lumaStd - sourceStat.lumaStd
	chromaVarianceDifference := targetStat.chromaStd - sourceStat.chromaStd
	targetLuma := 0.299*targetStat.mean[0] + 0.587*targetStat.mean[1] + 0.114*targetStat.mean[2]
	sourceLuma := 0.299*sourceStat.mean[0] + 0.587*sourceStat.mean[1] + 0.114*sourceStat.mean[2]
	lumaDifference := targetLuma - sourceLuma
	targetCb, targetCr := targetStat.mean[2]-targetLuma, targetStat.mean[0]-targetLuma
	sourceCb, sourceCr := sourceStat.mean[2]-sourceLuma, sourceStat.mean[0]-sourceLuma
	chromaMeanCost := squareFloat32(targetCb-sourceCb) + squareFloat32(targetCr-sourceCr)
	descriptorCost := (dgx*dgx+dgy*dgy)*1.5 + detailDifference*detailDifference*0.6 +
		varianceDifference*varianceDifference*0.8 + chromaVarianceDifference*chromaVarianceDifference*0.3 +
		chromaMeanCost*0.12 + lumaDifference*lumaDifference*0.02
	if descriptorCost >= bestCost {
		return descriptorCost
	}
	meanDifferenceCost := (squareFloat32(targetStat.mean[0]-sourceStat.mean[0]) +
		squareFloat32(targetStat.mean[1]-sourceStat.mean[1]) +
		squareFloat32(targetStat.mean[2]-sourceStat.mean[2])) / 3

	half := level.half
	x0, y0 := tx-half, ty-half
	confidenceSum := pmConfidenceRectSum(level, x0, y0, x0+level.patchSize, y0+level.patchSize)
	denominator := confidenceSum*3 + 0.0001
	rawLimit := float32(math.Inf(1))
	if !float32IsInf(bestCost) {
		// Mean normalization may remove this much energy from the raw SSD,
		// so include it in the SIMD kernel's safe early-exit allowance.
		rawLimit = (bestCost - descriptorCost + meanDifferenceCost) * denominator
		if rawLimit <= 0 {
			return descriptorCost
		}
	}

	targetIndex := y0*target.stride + x0
	sourceIndex := (sy-half)*level.srcPlanes.stride + sx - half
	args := pmKernelArgs{
		targetR:    &target.channel[0][targetIndex],
		targetG:    &target.channel[1][targetIndex],
		targetB:    &target.channel[2][targetIndex],
		targetA:    &target.channel[3][targetIndex],
		sourceR:    &level.srcPlanes.channel[0][sourceIndex],
		sourceG:    &level.srcPlanes.channel[1][sourceIndex],
		sourceB:    &level.srcPlanes.channel[2][sourceIndex],
		sourceA:    &level.srcPlanes.channel[3][sourceIndex],
		confidence: &level.confidence[y0*level.confStride+x0],
		stride:     target.stride,
		patchSize:  level.patchSize,
		limit:      rawLimit,
	}
	sum := pmRunPatchKernel(&args)
	normalizedPixelCost := maxFloat32(0, sum/denominator-meanDifferenceCost)
	return normalizedPixelCost + descriptorCost
}

func squareFloat32(value float32) float32 { return value * value }

func pmConfidenceRectSum(level *pmLevel, x0, y0, x1, y1 int) float32 {
	stride := level.w + 1
	return level.confSum[y1*stride+x1] - level.confSum[y0*stride+x1] -
		level.confSum[y1*stride+x0] + level.confSum[y0*stride+x0]
}

func pmPatchSSDScalar(args *pmKernelArgs) float32 {
	length := (args.patchSize-1)*args.stride + args.patchSize
	target := [4][]float32{
		unsafe.Slice(args.targetR, length),
		unsafe.Slice(args.targetG, length),
		unsafe.Slice(args.targetB, length),
		unsafe.Slice(args.targetA, length),
	}
	source := [4][]float32{
		unsafe.Slice(args.sourceR, length),
		unsafe.Slice(args.sourceG, length),
		unsafe.Slice(args.sourceB, length),
		unsafe.Slice(args.sourceA, length),
	}
	confidence := unsafe.Slice(args.confidence, length)

	var sum float32
	for y := 0; y < args.patchSize; y++ {
		row := y * args.stride
		for x := 0; x < args.patchSize; x++ {
			index := row + x
			weight := confidence[index]
			for channel := 0; channel < 4; channel++ {
				difference := target[channel][index] - source[channel][index]
				sum += weight * difference * difference
			}
		}
		if sum > args.limit {
			return sum
		}
	}
	return sum
}

func float32IsInf(value float32) bool {
	return value > math.MaxFloat32 || value < -math.MaxFloat32
}
