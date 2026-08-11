package main

import (
	"image"
	"math"
	"unsafe"
)

// pmPackedPlanes stores premultiplied channels in structure-of-arrays form.
// Rows contain SIMD-safe padding; the architecture-specific kernels retained
// from the original implementation use this exact layout.
type pmPackedPlanes struct {
	channel [4][]float32
	data    []float32
	stride  int
}

// pmKernelArgs is shared with patchcost_amd64.s / patchcost_arm64.s.
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
	stride     int      // 72, float32 elements
	patchSize  int      // 80
	limit      float32  // 88, raw weighted SSD early-exit threshold
}

func packPMPixels(src *image.NRGBA) pmPackedPlanes {
	return packPMPixelsInto(src, pmPackedPlanes{})
}

func packPMPixelsInto(src *image.NRGBA, planes pmPackedPlanes) pmPackedPlanes {
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	stride := (w + 15) &^ 7
	required := stride * h * 4
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
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			si := y*src.Stride + x*4
			di := y*stride + x
			alpha := float32(src.Pix[si+3])
			premul := alpha / 255
			planes.channel[0][di] = float32(src.Pix[si]) * premul
			planes.channel[1][di] = float32(src.Pix[si+1]) * premul
			planes.channel[2][di] = float32(src.Pix[si+2]) * premul
			// Alpha matters at transparent scan boundaries but should not dominate
			// ordinary RGB appearance.
			planes.channel[3][di] = alpha * 0.35
		}
	}
	return planes
}

func packPMConfidence(mask *image.Alpha) (values []float32, stride int, integral []float32) {
	w, h := mask.Bounds().Dx(), mask.Bounds().Dy()
	stride = (w + 15) &^ 7
	values = make([]float32, stride*h)
	integral = make([]float32, (w+1)*(h+1))
	for y := 0; y < h; y++ {
		var rowSum float32
		for x := 0; x < w; x++ {
			confidence := float32(255-mask.Pix[y*mask.Stride+x]) / 255
			values[y*stride+x] = confidence
			rowSum += confidence
			integral[(y+1)*(w+1)+x+1] = integral[y*(w+1)+x+1] + rowSum
		}
	}
	return values, stride, integral
}

// updatePMConfidence keeps the original mask coverage and provisional EM
// confidence separate. Known pixels always retain their true known fraction;
// reconstructed pixels earn confidence gradually, with less trust deep inside
// the hole than immediately behind a known boundary.
func updatePMConfidence(level *pmLevel, round int, haveSeed bool) {
	var provisional float32
	switch round {
	case 0:
		if haveSeed {
			provisional = 0.18
		}
	case 1:
		provisional = 0.42
	default:
		provisional = 0.70
	}

	clear(level.confidence)
	clear(level.confSum)
	for y := 0; y < level.h; y++ {
		var rowSum float32
		for x := 0; x < level.w; x++ {
			id := y*level.w + x
			known := float32(255-level.mask.Pix[y*level.mask.Stride+x]) / 255
			confidence := known
			if known < 1 && provisional > 0 {
				depth := level.insideDepth[id]
				if depth < 1 {
					depth = 1
				}
				// Deep provisional pixels never become as authoritative as observed
				// pixels. On texture-rich material we trust reconstructed RGB even
				// less: patch voting attenuates stochastic detail, and feeding that
				// smooth result back into the next E-step would otherwise cause a
				// self-reinforcing collapse toward smooth source patches.
				depthWeight := float32(0.35 + 0.65*math.Exp(-float64(depth-1)/6.0))
				textureRetain := float32(1)
				if id < len(level.textureGuide) {
					textureRetain = 1 - 0.82*pmTextureStrength(level.textureGuide[id])
				}
				// A blurred provisional edge must not become authoritative evidence in
				// the next E-step. Keep structure-rich masked pixels guide-driven until
				// the NNF has converged on one coherent edge hypothesis.
				structureRetain := float32(1)
				if id < len(level.structureGuide.strength) {
					structureRetain = 1 - 0.88*level.structureGuide.strength[id]
				}
				confidence += (1 - known) * provisional * depthWeight * textureRetain * structureRetain
			}
			level.confidence[y*level.confStride+x] = confidence
			rowSum += confidence
			level.confSum[(y+1)*(level.w+1)+x+1] = level.confSum[y*(level.w+1)+x+1] + rowSum
		}
	}
}

// pmPatchCost uses the same appearance model that reconstruction uses: raw
// source pixels. There is deliberately no mean-subtraction or un-applied
// gain/bias model. A weak locality prior is used only where target evidence is
// missing; it vanishes as a patch becomes observed/reconstructed.
func pmPatchCost(level *pmLevel, target *pmPackedPlanes, tx, ty int, source pmPoint, bestCost float32) float32 {
	if !validPMPoint(level, source) {
		return float32(math.Inf(1))
	}
	sx, sy := int(source.x), int(source.y)
	half := level.half
	x0, y0 := tx-half, ty-half
	confidenceSum := pmConfidenceRectSum(level, x0, y0, x0+level.patchSize, y0+level.patchSize)
	patchArea := float32(level.patchSize * level.patchSize)
	knownFraction := minFloat32(1, maxFloat32(0, confidenceSum/(patchArea+1e-6)))

	dx := float32(sx - tx)
	dy := float32(sy - ty)
	unknown := 1 - knownFraction
	localityPrior := unknown * unknown * 0.0035 * (dx*dx + dy*dy)

	// Preserve an independent texture objective through the hole. This is the
	// important complement to RGB SSD: once a stochastic texture has been
	// averaged by voting, the provisional RGB is smoother than the material we
	// are trying to synthesize. Comparing source texture against a guide derived
	// only from known surrounding pixels prevents that smooth reconstruction
	// from becoming the preferred source model on the next EM round.
	texturePenalty := float32(0)
	targetID := ty*level.w + tx
	sourceID := sy*level.w + sx
	if targetID >= 0 && targetID < len(level.textureGuide) && sourceID >= 0 && sourceID < len(level.textureEnergy) {
		targetTexture := level.textureGuide[targetID]
		sourceTexture := level.textureEnergy[sourceID]
		denom := 1.5 + targetTexture
		difference := (sourceTexture - targetTexture) / denom
		// Keep a modest texture term even when the patch has observed support,
		// but make it strongest for deep/unknown target patches.
		textureNeed := 0.28 + 0.72*unknown
		texturePenalty = 140 * textureNeed * difference * difference
	}

	structurePenalty := pmStructurePatchPenalty(level, tx, ty, sx, sy, unknown)
	prior := localityPrior + texturePenalty + structurePenalty
	if confidenceSum < 0.01 {
		return prior
	}

	// Three RGB channels plus the 0.35-scaled alpha channel (0.35^2=0.1225).
	denominator := confidenceSum*3.1225 + 1e-6
	rawLimit := float32(math.Inf(1))
	if !float32IsInf(bestCost) {
		remaining := bestCost - prior
		if remaining <= 0 {
			return prior
		}
		rawLimit = remaining * denominator
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
	return sum/denominator + prior
}

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
