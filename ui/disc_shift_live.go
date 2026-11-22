package ui

import (
	"image"
	"math"
	"runtime"
	"sync"
	"time"

	"atropos/internal/imageops"

	"gioui.org/f32"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
)

// Raster caps for the Ctrl+drag content layer. It must cover the visible disc
// plus the full shift range, so it is allowed to be larger than a regular
// viewport tile.
const (
	discShiftLiveMaxDim    = 4096
	discShiftLiveMaxPixels = 12 * 1024 * 1024
	// discShiftLivePadFraction is the maximum Ctrl+drag distance per gesture,
	// as a fraction of the larger viewport dimension.
	discShiftLivePadFraction = 0.5
	// discShiftWarmDelay debounces background prewarming after the disc
	// render or viewport layout changes (e.g. while a slider is dragged).
	discShiftWarmDelay = 200 * time.Millisecond
)

// discShiftLiveLayer is a ready-to-draw Ctrl+drag preview: unmasked disc
// source content that follows the pointer beneath a vignette that stays fixed
// in result-image space, so releasing the mouse does not change the picture.
//
// One layer serves a whole chain of drags (Ctrl+drag shifts and Shift+drag
// rotations). The content raster depends on neither the disc centre nor the
// rotation, so each committed change just accumulates into base and the layer
// stays on screen until the last commit of the chain is presented. The
// vignette is rotationally symmetric about the rotation pivot, so it stays
// fixed for both.
type discShiftLiveLayer struct {
	// Immutable after build; overlay goroutines read only these.
	geo         DiscShiftPreview
	key         discShiftLiveKey
	content     paint.ImageOp // downsampled copy of contentRect (source coords)
	contentImg  *image.NRGBA
	contentRect image.Rectangle
	contentSize image.Point
	overlayRect image.Rectangle // viewport px, for the stage at build time
	visible     [4]float64      // visible result-space rect at build (x0,y0,x1,y1)
	srcBounds   image.Rectangle
	buildStageX float64
	buildStageY float64
	buildStageW float64
	buildStageH float64

	// UI-thread state.
	overlay      paint.ImageOp
	overlayExact bool           // overlay is the opaque exact variant
	overlayGen   uint64         // invalidates in-flight overlay renders
	next         *paint.ImageOp // translucent overlay prepared for base

	base     discLivePose // changes released (committed or committing) since build
	settled  discLivePose // part of base confirmed by completed commits
	gesture  discLivePose // current gesture on top of base
	dragging bool
	showing  bool // drawn instead of the regular raster
	pending  int  // in-flight ShiftDisc commits from this chain

	validSource string          // presented render the layer currently matches
	own         map[string]bool // renders produced by this chain
	stale       bool            // a foreign change was seen; drop once settled
}

// discLivePose is a disc change relative to a layer's geometry: a centre
// shift in source px and an extra rotation in degrees.
type discLivePose struct {
	shift image.Point
	angle float64
}

func (p discLivePose) add(q discLivePose) discLivePose {
	return discLivePose{shift: p.shift.Add(q.shift), angle: p.angle + q.angle}
}

func (p discLivePose) isZero() bool { return p.shift == (image.Point{}) && p.angle == 0 }

// total is the disc change currently shown, relative to geo.
func (l *discShiftLiveLayer) total() discLivePose { return l.base.add(l.gesture) }

// rotation is the effective disc rotation (degrees) for pose p.
func (l *discShiftLiveLayer) rotation(p discLivePose) float64 { return l.geo.Rotation + p.angle }

// toSource maps result-image point (px, py) to source coords for pose p,
// mirroring the affine chain in drawDiscShiftLive.
func (l *discShiftLiveLayer) toSource(px, py float64, p discLivePose) (float64, float64) {
	g := l.geo
	rad := l.rotation(p) * math.Pi / 180
	cos, sin := math.Cos(rad), math.Sin(rad)
	rx, ry := px-g.PivotX, py-g.PivotY
	return cos*rx + sin*ry + g.SourcePivotX + float64(p.shift.X),
		-sin*rx + cos*ry + g.SourcePivotY + float64(p.shift.Y)
}

// covers reports whether the content raster holds every source pixel the
// visible part of the disc needs at pose p (relative to geo).
func (l *discShiftLiveLayer) covers(p discLivePose) bool {
	g := l.geo
	if math.Hypot(float64(p.shift.X), float64(p.shift.Y)) > l.key.maxShift+0.5 {
		return false
	}
	x0, y0, x1, y1 := l.visible[0], l.visible[1], l.visible[2], l.visible[3]
	cx, cy, r := g.MaskCenterX, g.MaskCenterY, float64(g.Radius)
	ok := func(px, py float64) bool {
		// Only the disc is visible (the vignette is opaque outside it), so
		// points beyond the rim are pulled onto it.
		if dx, dy := px-cx, py-cy; dx*dx+dy*dy > r*r {
			d := math.Hypot(dx, dy)
			px, py = cx+dx*r/d, cy+dy*r/d
		}
		sx, sy := l.toSource(px, py, p)
		if sx < float64(l.srcBounds.Min.X) || sy < float64(l.srcBounds.Min.Y) ||
			sx > float64(l.srcBounds.Max.X) || sy > float64(l.srcBounds.Max.Y) {
			return true // nothing exists there in the committed render either
		}
		cr := l.contentRect
		return sx >= float64(cr.Min.X)-1 && sy >= float64(cr.Min.Y)-1 &&
			sx <= float64(cr.Max.X)+1 && sy <= float64(cr.Max.Y)+1
	}
	const n = 24
	for i := 0; i <= n; i++ {
		t := float64(i) / n
		if !ok(x0+(x1-x0)*t, y0) || !ok(x0+(x1-x0)*t, y1) || !ok(x0, y0+(y1-y0)*t) || !ok(x1, y0+(y1-y0)*t) {
			return false
		}
	}
	for i := 0; i < 4*n; i++ {
		a := float64(i) * 2 * math.Pi / (4 * n)
		px, py := cx+r*math.Cos(a), cy+r*math.Sin(a)
		if px >= x0 && px <= x1 && py >= y0 && py <= y1 && !ok(px, py) {
			return false
		}
	}
	return true
}

// limitGesture returns the gesture closest to want (on the way from the
// current gesture) that keeps the total pose inside the content raster.
func (l *discShiftLiveLayer) limitGesture(want discLivePose) discLivePose {
	// Shifts are bounded by magnitude first (cheap and direction-preserving).
	tx, ty := float64(l.base.shift.X+want.shift.X), float64(l.base.shift.Y+want.shift.Y)
	if m := l.key.maxShift; m > 0 {
		if mag := math.Hypot(tx, ty); mag > m {
			tx, ty = math.Trunc(tx*m/mag), math.Trunc(ty*m/mag)
		}
	}
	want.shift = image.Pt(int(tx)-l.base.shift.X, int(ty)-l.base.shift.Y)
	if l.covers(l.base.add(want)) {
		return want
	}
	from := l.gesture
	lerp := func(t float64) discLivePose {
		return discLivePose{
			shift: image.Pt(
				from.shift.X+int(math.Round(float64(want.shift.X-from.shift.X)*t)),
				from.shift.Y+int(math.Round(float64(want.shift.Y-from.shift.Y)*t)),
			),
			angle: from.angle + (want.angle-from.angle)*t,
		}
	}
	lo, hi := 0.0, 1.0
	for i := 0; i < 12; i++ {
		mid := (lo + hi) / 2
		if l.covers(l.base.add(lerp(mid))) {
			lo = mid
		} else {
			hi = mid
		}
	}
	return lerp(lo)
}

// coverageLow reports that a good part of the movement range has been used
// up, so a freshly centred layer is worth building.
func (l *discShiftLiveLayer) coverageLow() bool {
	b := l.base
	if math.Hypot(float64(b.shift.X), float64(b.shift.Y)) > l.key.maxShift/2 {
		return true
	}
	for _, d := range []float64{-10, 10} {
		if !l.covers(discLivePose{shift: b.shift, angle: b.angle + d}) {
			return true
		}
	}
	return false
}

// discShiftMaxShift bounds a single Ctrl+drag gesture (in source pixels) so
// the content layer built at press time always covers the disc.
func discShiftMaxShift(info viewportLayoutInfo) float64 {
	if info.StageWidth <= 0 || info.StageDims.X <= 0 {
		return 0
	}
	scale := info.StageWidth / float64(info.StageDims.X) // screen px per source px
	return discShiftLivePadFraction * float64(max(info.ViewportW, info.ViewportH)) / scale
}

// buildDiscShiftLiveLayer prepares the content and vignette rasters for geo
// as laid out in info. maxShift is the gesture bound from discShiftMaxShift.
func buildDiscShiftLiveLayer(geo DiscShiftPreview, src *image.NRGBA, info viewportLayoutInfo, maxShift float64) *discShiftLiveLayer {
	if src == nil || geo.Width <= 0 || geo.Height <= 0 || geo.Radius <= 0 ||
		info.StageWidth <= 0 || info.StageHeight <= 0 {
		return nil
	}
	scaleX := info.StageWidth / float64(geo.Width)
	scaleY := info.StageHeight / float64(geo.Height)

	// Visible part of the result image, in result coords.
	vx0 := math.Max(0, -info.StageX) / scaleX
	vy0 := math.Max(0, -info.StageY) / scaleY
	vx1 := math.Min(info.StageWidth, float64(info.ViewportW)-info.StageX) / scaleX
	vy1 := math.Min(info.StageHeight, float64(info.ViewportH)-info.StageY) / scaleY
	if vx1 <= vx0 || vy1 <= vy0 {
		return nil
	}

	// Map it back into source coords (inverse rotation) and grow by the
	// gesture bound so any permitted shift stays covered.
	rad := geo.Rotation * math.Pi / 180
	cos, sin := math.Cos(rad), math.Sin(rad)
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for _, p := range [4][2]float64{{vx0, vy0}, {vx1, vy0}, {vx0, vy1}, {vx1, vy1}} {
		rx, ry := p[0]-geo.PivotX, p[1]-geo.PivotY
		sx := cos*rx + sin*ry + geo.SourcePivotX
		sy := -sin*rx + cos*ry + geo.SourcePivotY
		minX, maxX = math.Min(minX, sx), math.Max(maxX, sx)
		minY, maxY = math.Min(minY, sy), math.Max(maxY, sy)
	}
	reach := float64(geo.Radius) + maxShift
	region := image.Rect(
		int(math.Floor(math.Max(minX-maxShift, float64(geo.CenterX)-reach))),
		int(math.Floor(math.Max(minY-maxShift, float64(geo.CenterY)-reach))),
		int(math.Ceil(math.Min(maxX+maxShift, float64(geo.CenterX)+reach))),
		int(math.Ceil(math.Min(maxY+maxShift, float64(geo.CenterY)+reach))),
	).Intersect(src.Bounds())
	if region.Empty() {
		return nil
	}

	dstW, dstH := boundedDestSizeCap(region, scaleX, discShiftLiveMaxDim, discShiftLiveMaxPixels)
	content := downsampleViewportRegion(src, region, dstW, dstH)
	if geo.LevelsBlack != 0 || geo.LevelsWhite != 255 {
		content = imageops.ApplyLevels(content, geo.LevelsBlack, geo.LevelsWhite)
	}
	contentOp := paint.NewImageOp(gpuRGBA(content))
	contentOp.Filter = paint.FilterLinear

	overlayRect := image.Rect(
		int(math.Floor(math.Max(0, info.StageX))),
		int(math.Floor(math.Max(0, info.StageY))),
		int(math.Ceil(math.Min(float64(info.ViewportW), info.StageX+info.StageWidth))),
		int(math.Ceil(math.Min(float64(info.ViewportH), info.StageY+info.StageHeight))),
	)
	if overlayRect.Empty() {
		return nil
	}
	l := &discShiftLiveLayer{
		geo:         geo,
		key:         discShiftLiveKey{info: info, maxShift: maxShift},
		content:     contentOp,
		contentImg:  content,
		contentRect: region,
		contentSize: content.Bounds().Size(),
		overlayRect: overlayRect,
		visible:     [4]float64{vx0, vy0, vx1, vy1},
		srcBounds:   src.Bounds(),
		buildStageX: info.StageX,
		buildStageY: info.StageY,
		buildStageW: info.StageWidth,
		buildStageH: info.StageHeight,
	}
	l.overlay = l.computeOverlay(discLivePose{}, false)
	return l
}

// computeOverlay renders the vignette for a total pose. With exact set,
// feather pixels are opaque with the precise committed colour for the content
// under them, which is only valid while content is static (after release).
// Otherwise they are translucent and tuned for that content (see
// vignetteTranslucent). It reads only immutable fields, so it may run on any
// goroutine.
func (l *discShiftLiveLayer) computeOverlay(total discLivePose, exact bool) paint.ImageOp {
	g := l.geo
	scaleX := l.buildStageW / float64(g.Width)
	scaleY := l.buildStageH / float64(g.Height)
	sample := func(px, py float64) [3]float64 { return l.sampleContent(px, py, total) }
	img := renderDiscVignette(g, l.overlayRect, l.buildStageX, l.buildStageY, scaleX, scaleY, sample, exact)
	op := paint.NewImageOp(img)
	op.Filter = paint.FilterLinear
	return op
}

// sampleContent returns the (levels-adjusted, gamma-encoded 0..255) content
// colour the live layer shows at result-image point (px, py) for pose p.
func (l *discShiftLiveLayer) sampleContent(px, py float64, p discLivePose) [3]float64 {
	sx, sy := l.toSource(px, py, p)
	cr, img := l.contentRect, l.contentImg
	u := (sx-float64(cr.Min.X))*float64(l.contentSize.X)/float64(cr.Dx()) - 0.5
	v := (sy-float64(cr.Min.Y))*float64(l.contentSize.Y)/float64(cr.Dy()) - 0.5
	x0, y0 := int(math.Floor(u)), int(math.Floor(v))
	wx, wy := u-float64(x0), v-float64(y0)
	maxX, maxY := l.contentSize.X-1, l.contentSize.Y-1
	o00 := img.PixOffset(clamp(x0, 0, maxX), clamp(y0, 0, maxY))
	o10 := img.PixOffset(clamp(x0+1, 0, maxX), clamp(y0, 0, maxY))
	o01 := img.PixOffset(clamp(x0, 0, maxX), clamp(y0+1, 0, maxY))
	o11 := img.PixOffset(clamp(x0+1, 0, maxX), clamp(y0+1, 0, maxY))
	var out [3]float64
	for ch := 0; ch < 3; ch++ {
		out[ch] = float64(img.Pix[o00+ch])*(1-wx)*(1-wy) + float64(img.Pix[o10+ch])*wx*(1-wy) +
			float64(img.Pix[o01+ch])*(1-wx)*wy + float64(img.Pix[o11+ch])*wx*wy
	}
	return out
}

// renderDiscVignette renders the disc mask for the screen pixels in rect.
//
// The committed render blends mask*src + (1-mask)*bg in gamma-encoded sRGB,
// but Gio composites in linear light, so a plain alpha = 1-mask overlay would
// visibly change the feather's tone on press and release. Solid regions are
// exact either way (alpha 0 or 1); feather pixels use the content sampled
// under them: opaque exact colour when exact is set, otherwise a translucent
// first-order match (vignetteTranslucent).
//
// Pixels near a mask transition are supersampled so thin feathers keep the
// same anti-aliased edge the box-filtered committed preview shows. The result
// is premultiplied (gamma-space, as Gio expects) so it uploads without a copy.
func renderDiscVignette(geo DiscShiftPreview, rect image.Rectangle, stageX, stageY, scaleX, scaleY float64, sample func(px, py float64) [3]float64, exact bool) *image.RGBA {
	w, h := rect.Dx(), rect.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	mask := imageops.NewCircularMask(geo.Radius, geo.FeatherSize, geo.CutoutRadius)
	bg := [3]float64{float64(geo.BgR), float64(geo.BgG), float64(geo.BgB)}

	// Transition bands (in source px) where alpha is not constant.
	outerHi := float64(geo.Radius)
	outerLo := math.Max(0, outerHi-float64(min(geo.FeatherSize, geo.Radius)))
	cutLo := float64(geo.CutoutRadius)
	cutHi := cutLo + float64(geo.FeatherSize)
	hasCutout := geo.CutoutRadius > 0
	pxX, pxY := 1/scaleX, 1/scaleY // one screen pixel in source px
	halfDiag := 0.5 * math.Hypot(pxX, pxY)
	sub := 1
	if s := math.Max(pxX, pxY); s > 1 {
		sub = min(4, int(math.Ceil(s)))
	}
	nearEdge := func(d float64) bool {
		if d >= outerLo-halfDiag && d <= outerHi+halfDiag {
			return true
		}
		return hasCutout && d >= cutLo-halfDiag && d <= cutHi+halfDiag
	}

	workers := max(1, min(runtime.NumCPU(), h))
	rows := (h + workers - 1) / workers
	var wg sync.WaitGroup
	for y0 := 0; y0 < h; y0 += rows {
		y1 := min(h, y0+rows)
		wg.Add(1)
		go func(y0, y1 int) {
			defer wg.Done()
			for y := y0; y < y1; y++ {
				off := y * dst.Stride
				py := (float64(rect.Min.Y+y) + 0.5 - stageY) / scaleY
				for x := 0; x < w; x++ {
					px := (float64(rect.Min.X+x) + 0.5 - stageX) / scaleX
					dx, dy := px-geo.MaskCenterX, py-geo.MaskCenterY
					d2 := dx*dx + dy*dy
					var m float64
					if sub > 1 && nearEdge(math.Sqrt(d2)) {
						for j := 0; j < sub; j++ {
							oy := ((float64(j)+0.5)/float64(sub) - 0.5) * pxY
							for i := 0; i < sub; i++ {
								ox := ((float64(i)+0.5)/float64(sub) - 0.5) * pxX
								ex, ey := dx+ox, dy+oy
								m += mask.Alpha(ex*ex + ey*ey)
							}
						}
						m /= float64(sub * sub)
					} else {
						m = mask.Alpha(d2)
					}
					p := dst.Pix[off : off+4 : off+4]
					switch {
					case m >= 1:
						// Fully content: leave transparent.
					case m <= 0 || sample == nil:
						a := 1 - math.Max(0, m)
						for ch := 0; ch < 3; ch++ {
							p[ch] = uint8(math.Round(bg[ch] * a))
						}
						p[3] = uint8(math.Round(255 * a))
					case exact:
						c := sample(px, py)
						for ch := 0; ch < 3; ch++ {
							p[ch] = uint8(math.Round(clampF64(m*c[ch]+(1-m)*bg[ch], 0, 255)))
						}
						p[3] = 255
					default:
						col, a := vignetteTranslucent(m, sample(px, py), bg)
						p[0], p[1], p[2], p[3] = col[0], col[1], col[2], a
					}
					off += 4
				}
			}
		}(y0, y1)
	}
	wg.Wait()
	return dst
}

// vignetteTranslucent picks an overlay colour and alpha for one feather pixel
// so that Gio's compositing of the overlay onto content c reproduces the
// committed gamma-space blend m*c + (1-m)*bg exactly for c, and as closely as
// possible for other content as it moves underneath. Inputs are
// gamma-encoded 0..255; the result is the premultiplied colour and alpha.
//
// Gio treats premultiplied RGBA as gamma-encoded and blends in linear light:
// out = dec(P) + (1-a)*dec(c). Exactness for c fixes dec(P); the free
// "keep" = 1-a is ideally d out/d c (first-order match for moving content),
// backed off until the required P is representable (0 <= P <= a).
func vignetteTranslucent(m float64, c, bg [3]float64) ([3]uint8, uint8) {
	var target, lin [3]float64
	slope := 0.0
	for ch := 0; ch < 3; ch++ {
		cg := c[ch] / 255
		y := m*cg + (1-m)*bg[ch]/255
		target[ch] = srgbDecode(y)
		lin[ch] = srgbDecode(cg)
		// d target / d content, both in linear light.
		slope += m * srgbDecodeDeriv(y) / srgbDecodeDeriv(cg)
	}
	const step = 1.0 / 64
	for keep := clampF64(slope/3, 0, 1); ; keep = math.Max(0, keep-step) {
		a8 := math.Round(255 * (1 - keep))
		a := a8 / 255
		var col [3]uint8
		ok := true
		for ch := 0; ch < 3 && ok; ch++ {
			// Linear contribution the premultiplied overlay must add.
			p := target[ch] - (1-a)*lin[ch]
			pg := srgbEncode(math.Max(0, p)) // premultiplied, gamma space
			if p < -0.5/255 || pg > a+0.5/255 {
				ok = false
				break
			}
			col[ch] = uint8(math.Min(a8, math.Round(255*pg)))
		}
		if ok || keep <= 0 {
			return col, uint8(a8)
		}
	}
}

func srgbDecode(v float64) float64 {
	if v <= 0.04045 {
		return v / 12.92
	}
	return math.Pow((v+0.055)/1.055, 2.4)
}

func srgbDecodeDeriv(v float64) float64 {
	if v <= 0.04045 {
		return 1 / 12.92
	}
	return 2.4 / 1.055 * math.Pow((v+0.055)/1.055, 1.4)
}

func srgbEncode(l float64) float64 {
	if l <= 0.0031308 {
		return 12.92 * l
	}
	return 1.055*math.Pow(l, 1/2.4) - 0.055
}

// drawDiscShiftLive paints the live content (at pose l.total()) and the
// stationary vignette, clipped to the visible stage.
func drawDiscShiftLive(ops *op.Ops, l *discShiftLiveLayer, info viewportLayoutInfo, vpRect image.Rectangle) {
	g := l.geo
	if g.Width <= 0 || g.Height <= 0 || l.contentSize.X <= 0 || l.contentSize.Y <= 0 || l.buildStageW <= 0 {
		return
	}
	stageRect := image.Rect(
		int(math.Floor(info.StageX)),
		int(math.Floor(info.StageY)),
		int(math.Ceil(info.StageX+info.StageWidth)),
		int(math.Ceil(info.StageY+info.StageHeight)),
	).Intersect(vpRect)
	if stageRect.Empty() {
		return
	}
	stageClip := clip.Rect(stageRect).Push(ops)
	defer stageClip.Pop()

	scaleX := float32(info.StageWidth / float64(g.Width))
	scaleY := float32(info.StageHeight / float64(g.Height))
	cr := l.contentRect
	pose := l.total()
	shift := pose.shift
	// raster px → source px → disc-relative (with pending shift) → rotate →
	// result px → stage px.
	contentAff := f32.AffineId().
		Scale(f32.Point{}, f32.Pt(float32(cr.Dx())/float32(l.contentSize.X), float32(cr.Dy())/float32(l.contentSize.Y))).
		Offset(f32.Pt(
			float32(float64(cr.Min.X)-g.SourcePivotX-float64(shift.X)),
			float32(float64(cr.Min.Y)-g.SourcePivotY-float64(shift.Y)),
		)).
		Rotate(f32.Point{}, float32(l.rotation(pose)*math.Pi/180)).
		Offset(f32.Pt(float32(g.PivotX), float32(g.PivotY))).
		Scale(f32.Point{}, f32.Pt(scaleX, scaleY)).
		Offset(f32.Pt(float32(info.StageX), float32(info.StageY)))
	st := op.Affine(contentAff).Push(ops)
	cl := clip.Rect(image.Rectangle{Max: l.contentSize}).Push(ops)
	l.content.Add(ops)
	paint.PaintOp{}.Add(ops)
	cl.Pop()
	st.Pop()

	// The vignette was rendered for the stage at build time; follow any
	// zoom/scroll that happened since.
	k := float32(info.StageWidth / l.buildStageW)
	overlayAff := f32.AffineId().
		Offset(f32.Pt(float32(float64(l.overlayRect.Min.X)-l.buildStageX), float32(float64(l.overlayRect.Min.Y)-l.buildStageY))).
		Scale(f32.Point{}, f32.Pt(k, k)).
		Offset(f32.Pt(float32(info.StageX), float32(info.StageY)))
	st = op.Affine(overlayAff).Push(ops)
	cl = clip.Rect(image.Rectangle{Max: l.overlayRect.Size()}).Push(ops)
	l.overlay.Add(ops)
	paint.PaintOp{}.Add(ops)
	cl.Pop()
	st.Pop()
}
