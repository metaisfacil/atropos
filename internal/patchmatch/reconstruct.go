package patchmatch

import (
	"context"
	"image"
	"math"
)

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

// Onion-peel pre-heal for holes that touch the image border. The hole is
// grown inward from the known content in 2 px rings at a coarse power-of-two
// scale; every ring gets a smooth initial guess and a short single-level
// PatchMatch solve before the next ring is taken, and the ordinary pyramid
// then starts from the fully pre-healed coarse image.
const (
	peelMorphology    = 2   // pixels the known region grows per peel
	peelMaxCoarseSize = 50  // -max_onion_peel_coarse_size
	peelMaxDimension  = 512 // coarse level is halved while larger than this
	peelRounds        = 2
	peelMinRemaining  = 10 // fewer remaining hole pixels are taken at once
	peelWindowBase    = 10 // hole search window of peel i is 10 + 4*i
	peelWindowStep    = 4
)

var synthesisDebugPeelRing func(index int, img *image.NRGBA, ring *image.Alpha)

// synthesisSearchWindow reproduces the engine's automatic hole search window
// (half width, in level pixels): from the hole pixel count and its depth,
// where the depth is the L1 maxDist for interior holes and half of it for
// holes touching the border.
func synthesisSearchWindow(mask *image.Alpha, maxDist int, touchesBorder bool) int {
	w, h := mask.Bounds().Dx(), mask.Bounds().Dy()
	count := 0
	for y := 0; y < h; y++ {
		row := mask.Pix[y*mask.Stride : y*mask.Stride+w]
		for _, v := range row {
			if v != 0 {
				count++
			}
		}
	}
	radius := maxDist
	if touchesBorder {
		radius = maxDist / 2
	}
	i := radius - 3
	iMin := i
	if iMin < 1 {
		iMin = 1
	}
	d := math.Sqrt(float64(count))*0.56419/float64(iMin) - 0.9
	if d <= 0.01 {
		d = 0.01
	}
	d = math.Pow(d, -2)
	return int(float64(int(d*float64(i)*0.5+float64(i)*4.5+25.0)) * 0.5)
}

// maskTouchesBorder reports whether any masked pixel lies on the image edge.
func maskTouchesBorder(mask *image.Alpha) bool {
	w, h := mask.Bounds().Dx(), mask.Bounds().Dy()
	if w == 0 || h == 0 {
		return false
	}
	for x := 0; x < w; x++ {
		if mask.Pix[x] != 0 || mask.Pix[(h-1)*mask.Stride+x] != 0 {
			return true
		}
	}
	for y := 0; y < h; y++ {
		if mask.Pix[y*mask.Stride] != 0 || mask.Pix[y*mask.Stride+w-1] != 0 {
			return true
		}
	}
	return false
}

// synthesisMaxL1Distance returns the largest Manhattan distance from a masked
// pixel to the nearest unmasked pixel. Pixels outside the image are unknown.
func synthesisMaxL1Distance(mask *image.Alpha) int {
	w, h := mask.Bounds().Dx(), mask.Bounds().Dy()
	distance := make([]int32, w*h)
	queue := make([]int32, 0, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			id := y*w + x
			if mask.Pix[y*mask.Stride+x] == 0 {
				queue = append(queue, int32(id))
			} else {
				distance[id] = -1
			}
		}
	}
	best := 0
	for head := 0; head < len(queue); head++ {
		id := int(queue[head])
		x, y := id%w, id/w
		next := distance[id] + 1
		for _, d := range [...]image.Point{{X: -1}, {X: 1}, {Y: -1}, {Y: 1}} {
			nx, ny := x+d.X, y+d.Y
			if nx < 0 || ny < 0 || nx >= w || ny >= h {
				continue
			}
			nid := ny*w + nx
			if distance[nid] >= 0 {
				continue
			}
			distance[nid] = next
			if int(next) > best {
				best = int(next)
			}
			queue = append(queue, int32(nid))
		}
	}
	return best
}

// synthesisPeelScale reproduces the coarse scale chosen for onion peeling:
// max(50/w, min(0.25, 10/maxDist)) rounded up to a power of two (1 when
// that exceeds 0.5), halved while the coarse image is larger than 512 px.
func synthesisPeelScale(w, h, maxDist int) float32 {
	scale := float32(1)
	if maxDist*2 > 20 {
		scale = float32(peelMaxCoarseSize) / float32(w)
		limit := float32(20) / float32(maxDist*2)
		if limit > 0.25 {
			limit = 0.25
		}
		if scale <= limit {
			scale = limit
		}
		if scale <= 0.5 {
			exponent := 0
			for scale <= float32(math.Pow(2, float64(exponent-1))) {
				exponent--
			}
			scale = float32(math.Pow(2, float64(exponent)))
		} else {
			scale = 1
		}
	}
	for maxInt(int(float32(w)*scale+0.5), int(float32(h)*scale+0.5)) > peelMaxDimension {
		scale *= 0.5
	}
	return scale
}

// synthesisPeel fills the hole of the coarse level image ring by ring and
// returns the pre-healed image together with the union of the ring fields,
// which the main solve inherits as its incumbent. depth is the hole depth in
// coarse pixels.
func synthesisPeel(ctx context.Context, src *image.NRGBA, mask *image.Alpha, depth int) (*image.NRGBA, []pmPoint, error) {
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	working := cloneNRGBA(src)
	field := make([]pmPoint, w*h)
	for i := range field {
		field[i] = pmPoint{x: -1, y: -1}
	}
	known := make([]bool, w*h)
	remaining := 0
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if mask.Pix[y*mask.Stride+x] == 0 {
				known[y*w+x] = true
			} else {
				remaining++
			}
		}
	}
	count := (depth + 1) / 2
	if count < 1 {
		count = 1
	}
	for i := 0; i < count && remaining > 0; i++ {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		ring := image.NewAlpha(image.Rect(0, 0, w, h))
		rest := image.NewAlpha(image.Rect(0, 0, w, h))
		ringCount := 0
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				id := y*w + x
				if known[id] {
					continue
				}
				if synthesisNearKnown(known, w, h, x, y, peelMorphology) {
					ring.Pix[y*ring.Stride+x] = 255
					ringCount++
				}
			}
		}
		if i == count-1 || remaining-ringCount < peelMinRemaining || ringCount == 0 {
			ringCount = 0
			for y := 0; y < h; y++ {
				for x := 0; x < w; x++ {
					if !known[y*w+x] {
						ring.Pix[y*ring.Stride+x] = 255
						ringCount++
					}
				}
			}
		}
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				if !known[y*w+x] && ring.Pix[y*ring.Stride+x] == 0 {
					rest.Pix[y*rest.Stride+x] = 255
				}
			}
		}

		synthesisDiffuseFill(working, ring, known)
		var err error
		working, err = synthesisHealRing(ctx, working, ring, rest, i, field)
		if err != nil {
			return nil, nil, err
		}
		if synthesisDebugPeelRing != nil {
			synthesisDebugPeelRing(i, working, ring)
		}
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				if ring.Pix[y*ring.Stride+x] != 0 {
					known[y*w+x] = true
				}
			}
		}
		remaining -= ringCount
	}
	return working, field, nil
}

func synthesisNearKnown(known []bool, w, h, x, y, radius int) bool {
	for dy := -radius; dy <= radius; dy++ {
		ny := y + dy
		if ny < 0 || ny >= h {
			continue
		}
		for dx := -radius; dx <= radius; dx++ {
			nx := x + dx
			if nx < 0 || nx >= w {
				continue
			}
			if known[ny*w+nx] {
				return true
			}
		}
	}
	return false
}

// synthesisDiffuseFill implements the push-pull ROI fill used to seed an
// onion-peel ring. The native Heal path starts with known pixels immediately
// adjacent to the selection, reduces signal and alpha with the separable
// five-tap kernel [.05 .25 .4 .25 .05], then expands while retaining the
// confidence already present at each finer level.
func synthesisDiffuseFill(img *image.NRGBA, ring *image.Alpha, known []bool) {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	inRing := func(x, y int) bool { return ring.Pix[y*ring.Stride+x] != 0 }
	type healPlane struct {
		w, h  int
		rgb   []float64
		alpha []float64
	}
	base := healPlane{w: w, h: h, rgb: make([]float64, w*h*3), alpha: make([]float64, w*h)}
	adjacent := func(x, y int) bool {
		for _, d := range [...]image.Point{{X: -1}, {X: 1}, {Y: -1}, {Y: 1}} {
			nx, ny := x+d.X, y+d.Y
			if nx >= 0 && ny >= 0 && nx < w && ny < h && inRing(nx, ny) {
				return true
			}
		}
		return false
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			id := y*w + x
			if !known[id] || !adjacent(x, y) {
				continue
			}
			si := y*img.Stride + x*4
			base.alpha[id] = 1
			for c := 0; c < 3; c++ {
				base.rgb[id*3+c] = float64(img.Pix[si+c])
			}
		}
	}

	kernel := [...]float64{0.05, 0.25, 0.4, 0.25, 0.05}
	levels := []healPlane{base}
	for levels[len(levels)-1].w > 1 || levels[len(levels)-1].h > 1 {
		fine := &levels[len(levels)-1]
		cw, ch := maxInt(1, (fine.w+1)/2), maxInt(1, (fine.h+1)/2)
		coarse := healPlane{w: cw, h: ch, rgb: make([]float64, cw*ch*3), alpha: make([]float64, cw*ch)}
		for cy := 0; cy < ch; cy++ {
			for cx := 0; cx < cw; cx++ {
				cid := cy*cw + cx
				for ky := -2; ky <= 2; ky++ {
					fy := cy*2 + ky
					if fy < 0 || fy >= fine.h {
						continue
					}
					for kx := -2; kx <= 2; kx++ {
						fx := cx*2 + kx
						if fx < 0 || fx >= fine.w {
							continue
						}
						weight := kernel[kx+2] * kernel[ky+2]
						fid := fy*fine.w + fx
						coarse.alpha[cid] += weight * fine.alpha[fid]
						for c := 0; c < 3; c++ {
							coarse.rgb[cid*3+c] += weight * fine.rgb[fid*3+c]
						}
					}
				}
			}
		}
		levels = append(levels, coarse)
	}

	coarsest := &levels[len(levels)-1]
	for id, alpha := range coarsest.alpha {
		if alpha <= 0 {
			continue
		}
		for c := 0; c < 3; c++ {
			coarsest.rgb[id*3+c] /= alpha
		}
	}
	for li := len(levels) - 2; li >= 0; li-- {
		fine, coarse := &levels[li], &levels[li+1]
		for y := 0; y < fine.h; y++ {
			for x := 0; x < fine.w; x++ {
				id := y*fine.w + x
				var expanded [3]float64
				var weightSum float64
				for cy := maxInt(0, (y-2+1)/2); cy <= minInt(coarse.h-1, (y+2)/2); cy++ {
					ky := y - cy*2
					if ky < -2 || ky > 2 {
						continue
					}
					for cx := maxInt(0, (x-2+1)/2); cx <= minInt(coarse.w-1, (x+2)/2); cx++ {
						kx := x - cx*2
						if kx < -2 || kx > 2 {
							continue
						}
						weight := kernel[kx+2] * kernel[ky+2]
						cid := cy*coarse.w + cx
						weightSum += weight
						for c := 0; c < 3; c++ {
							expanded[c] += weight * coarse.rgb[cid*3+c]
						}
					}
				}
				alpha := fine.alpha[id]
				for c := 0; c < 3; c++ {
					pull := 0.0
					if weightSum > 0 {
						pull = expanded[c] / weightSum
					}
					fine.rgb[id*3+c] += (1 - alpha) * pull
				}
				fine.alpha[id] = 1
			}
		}
	}

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if !inRing(x, y) {
				continue
			}
			id := y*w + x
			di := y*img.Stride + x*4
			for c := 0; c < 3; c++ {
				img.Pix[di+c] = byte(clampInt(int(base.rgb[id*3+c]+0.5), 0, 255))
			}
			img.Pix[di+3] = 255
		}
	}
}

// synthesisHealRing runs the short single-level solve of one ring. Sources
// exclude both the ring and the still-unfilled remainder; targets are every
// patch centre covering the current ring, including centres whose patch also
// reaches into the remainder.
func synthesisHealRing(ctx context.Context, working *image.NRGBA, ring, rest *image.Alpha, peelIndex int, field []pmPoint) (*image.NRGBA, error) {
	level := newSynthesisLevel(working, ring, rest)
	if level.painted.Empty() || len(level.sources) == 0 {
		return working, nil
	}
	defer func() {
		for y := level.active.Min.Y; y < level.active.Max.Y; y++ {
			for x := level.active.Min.X; x < level.active.Max.X; x++ {
				id := synthesisFieldID(level, x, y)
				p := level.nnf[id]
				if level.target[id] && synthesisValid(level, p) {
					field[y*level.w+x] = p
				}
			}
		}
	}()
	levelIndex := 0x100 + peelIndex
	window := peelWindowBase + peelWindowStep*peelIndex
	level.searchWindow = window
	out := working
	for round := 0; round < peelRounds; round++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		// Same round structure as the main solve: the first round searches
		// a random field, later rounds merge a refined restart field into it.
		if round == 0 {
			synthesisRandomInitialize(level, out, levelIndex, round, window, false, false)
			synthesisSearchDispatch(level, out, synthesisFineRadius, levelIndex, round, 0, nil, nil)
		} else {
			synthesisRefreshCosts(level, out)
			restart := synthesisRestartField(level, out, levelIndex, round, window, synthesisFineRadius, nil)
			synthesisSearchDispatch(level, out, synthesisSecondaryRadius, levelIndex, round, 1, restart, nil)
		}
		var err error
		out, err = synthesisVote(ctx, level, out)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}
