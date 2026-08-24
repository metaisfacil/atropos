package patchmatch

import "math"

// pmPhotoTransform is a deliberately restrained photometric model for one NNF
// correspondence. A single gain preserves hue relationships while three small
// channel biases absorb local illumination / print-density drift.
type pmPhotoTransform struct {
	gain float32
	bias [3]float32
}

type pmPhotoIntegral struct {
	stride                 int
	weight, r, g, b, l, l2 []float32
}

type pmPhotoStats struct {
	weight, meanL, stdL float32
	mean                [3]float32
}

const (
	pmPhotoGainMin  = float32(0.90)
	pmPhotoGainMax  = float32(1.10)
	pmPhotoBiasMax  = float32(12.75) // ~= 0.05 in normalized 0..1 RGB
	pmPhotoMinRatio = float32(0.38)
)

func pmIdentityPhotoTransform() pmPhotoTransform { return pmPhotoTransform{gain: 1} }

func pmPreparePhotoSourceStats(level *pmLevel) {
	if level == nil || level.w == 0 || level.h == 0 {
		return
	}
	level.photoSourceStats = pmBuildPhotoIntegral(level.w, level.h, func(x, y int) (w, r, g, b float32) {
		i := y*level.srcPlanes.stride + x
		return 1, level.srcPlanes.channel[0][i], level.srcPlanes.channel[1][i], level.srcPlanes.channel[2][i]
	})
}

func pmPreparePhotoTargetStats(level *pmLevel, target *pmPackedPlanes) {
	if level == nil || target == nil || level.w == 0 || level.h == 0 {
		return
	}
	level.photoTargetStats = pmBuildPhotoIntegralReuse(level.photoTargetStats, level.w, level.h, func(x, y int) (w, r, g, b float32) {
		w = level.confidence[y*level.confStride+x]
		i := y*target.stride + x
		return w, target.channel[0][i], target.channel[1][i], target.channel[2][i]
	})
}

func pmBuildPhotoIntegral(w, h int, sample func(x, y int) (weight, r, g, b float32)) pmPhotoIntegral {
	return pmBuildPhotoIntegralReuse(pmPhotoIntegral{}, w, h, sample)
}

func pmBuildPhotoIntegralReuse(field pmPhotoIntegral, w, h int, sample func(x, y int) (weight, r, g, b float32)) pmPhotoIntegral {
	stride := w + 1
	required := stride * (h + 1)
	ensure := func(buf []float32) []float32 {
		if cap(buf) < required {
			return make([]float32, required)
		}
		buf = buf[:required]
		clear(buf)
		return buf
	}
	field.stride = stride
	field.weight = ensure(field.weight)
	field.r = ensure(field.r)
	field.g = ensure(field.g)
	field.b = ensure(field.b)
	field.l = ensure(field.l)
	field.l2 = ensure(field.l2)
	for y := 0; y < h; y++ {
		var rw, rr, rg, rb, rl, rl2 float32
		for x := 0; x < w; x++ {
			wt, r, g, b := sample(x, y)
			l := 0.299*r + 0.587*g + 0.114*b
			rw += wt
			rr += wt * r
			rg += wt * g
			rb += wt * b
			rl += wt * l
			rl2 += wt * l * l
			d := (y+1)*stride + x + 1
			up := y*stride + x + 1
			field.weight[d] = field.weight[up] + rw
			field.r[d] = field.r[up] + rr
			field.g[d] = field.g[up] + rg
			field.b[d] = field.b[up] + rb
			field.l[d] = field.l[up] + rl
			field.l2[d] = field.l2[up] + rl2
		}
	}
	return field
}

func pmPhotoIntegralRect(buf []float32, stride, x0, y0, x1, y1 int) float32 {
	return buf[y1*stride+x1] - buf[y0*stride+x1] - buf[y1*stride+x0] + buf[y0*stride+x0]
}

func pmPhotoPatchStats(field *pmPhotoIntegral, cx, cy, half int) pmPhotoStats {
	if field == nil || field.stride == 0 || len(field.weight) == 0 {
		return pmPhotoStats{}
	}
	x0, y0 := cx-half, cy-half
	x1, y1 := cx+half+1, cy+half+1
	w := pmPhotoIntegralRect(field.weight, field.stride, x0, y0, x1, y1)
	if w <= 1e-5 {
		return pmPhotoStats{}
	}
	stats := pmPhotoStats{weight: w}
	stats.mean[0] = pmPhotoIntegralRect(field.r, field.stride, x0, y0, x1, y1) / w
	stats.mean[1] = pmPhotoIntegralRect(field.g, field.stride, x0, y0, x1, y1) / w
	stats.mean[2] = pmPhotoIntegralRect(field.b, field.stride, x0, y0, x1, y1) / w
	sumL := pmPhotoIntegralRect(field.l, field.stride, x0, y0, x1, y1)
	sumL2 := pmPhotoIntegralRect(field.l2, field.stride, x0, y0, x1, y1)
	stats.meanL = sumL / w
	variance := maxFloat32(0, sumL2/w-stats.meanL*stats.meanL)
	stats.stdL = float32(math.Sqrt(float64(variance)))
	return stats
}

// pmEstimatePhotoTransform uses exact patch-window means and variance from
// integral statistics, making the candidate-time cost O(1). The target moments
// are confidence weighted, so painted pixels cannot freely invent a transform.
func pmEstimatePhotoTransform(level *pmLevel, target *pmPackedPlanes, tx, ty int, source pmPoint) pmPhotoTransform {
	_ = target // target is already summarized in level.photoTargetStats.
	if level == nil || !level.photoEnabled || !validPMPoint(level, source) {
		return pmIdentityPhotoTransform()
	}
	t := pmPhotoPatchStats(&level.photoTargetStats, tx, ty, level.half)
	s := pmPhotoPatchStats(&level.photoSourceStats, int(source.x), int(source.y), level.half)
	patchArea := float32(level.patchSize * level.patchSize)
	if t.weight < maxFloat32(1.25, 0.08*patchArea) || s.weight < 1 {
		return pmIdentityPhotoTransform()
	}
	gain := float32(1)
	if s.stdL > 2.0 && t.stdL > 0.5 {
		gain = t.stdL / s.stdL
		gain = minFloat32(pmPhotoGainMax, maxFloat32(pmPhotoGainMin, gain))
	}
	reliability := minFloat32(1, maxFloat32(0, (t.weight/patchArea-0.08)/0.42))
	gain = 1 + (gain-1)*reliability
	tr := pmPhotoTransform{gain: gain}
	for c := 0; c < 3; c++ {
		bias := t.mean[c] - gain*s.mean[c]
		bias = minFloat32(pmPhotoBiasMax, maxFloat32(-pmPhotoBiasMax, bias))
		tr.bias[c] = bias * reliability
	}
	return tr
}

// pmPhotoCostAdjustment estimates the low-frequency portion of raw patch SSD
// that the bounded transform can legitimately explain. It intentionally cannot
// drive corrected SSD below pmPhotoMinRatio of the measured full-patch cost;
// geometry/texture still have to match.
func pmPhotoCostAdjustment(level *pmLevel, tx, ty int, source pmPoint, tr pmPhotoTransform) (explained, regularizer float32) {
	if level == nil || !level.photoEnabled {
		return 0, 0
	}
	t := pmPhotoPatchStats(&level.photoTargetStats, tx, ty, level.half)
	s := pmPhotoPatchStats(&level.photoSourceStats, int(source.x), int(source.y), level.half)
	if t.weight <= 1e-5 || s.weight <= 1e-5 {
		return 0, 0
	}
	var meanEnergy float32
	for c := 0; c < 3; c++ {
		d := t.mean[c] - s.mean[c]
		meanEnergy += d * d
	}
	meanEnergy /= 3
	contrast := t.stdL - s.stdL
	// Means are the dominant useful part on scans; variance correction is weaker
	// so periodic/halftone structure cannot be made artificially cheap by gain.
	explained = 0.86*meanEnergy + 0.30*contrast*contrast

	g := (tr.gain - 1) / 0.10
	regularizer = 1.25 * g * g
	for c := 0; c < 3; c++ {
		b := tr.bias[c] / pmPhotoBiasMax
		regularizer += 0.30 * b * b
	}
	return explained, regularizer
}

func pmApplyPhotoRGB(value byte, channel int, tr pmPhotoTransform) byte {
	if channel < 0 || channel >= 3 {
		return value
	}
	return byte(clampFloat32(tr.gain*float32(value) + tr.bias[channel]))
}

func pmApplyPhotoRGBFloat(value float32, channel int, tr pmPhotoTransform) float32 {
	if channel < 0 || channel >= 3 {
		return value
	}
	return clampFloat32(tr.gain*value + tr.bias[channel])
}

func pmPreparePhotoTransforms(level *pmLevel, nnf []pmPoint) {
	size := level.w * level.h
	if cap(level.photo) < size {
		level.photo = make([]pmPhotoTransform, size)
	} else {
		level.photo = level.photo[:size]
	}
	identity := pmIdentityPhotoTransform()
	for i := range level.photo {
		level.photo[i] = identity
	}
	for y := level.active.Min.Y; y < level.active.Max.Y; y++ {
		for x := level.active.Min.X; x < level.active.Max.X; x++ {
			id := y*level.w + x
			if id < len(nnf) && validPMPoint(level, nnf[id]) {
				level.photo[id] = pmEstimatePhotoTransform(level, &level.targetPlanes, x, y, nnf[id])
			}
		}
	}
}

func pmPhotoCompatible(a, b pmPhotoTransform) bool {
	if float32(math.Abs(float64(a.gain-b.gain))) > 0.045 {
		return false
	}
	var d2 float32
	for c := 0; c < 3; c++ {
		d := a.bias[c] - b.bias[c]
		d2 += d * d
	}
	return d2 <= 7.5*7.5*3
}
