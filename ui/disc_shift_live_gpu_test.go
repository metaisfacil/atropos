package ui

import (
	"image"
	"image/color"
	"math"
	"testing"

	"atropos/internal/imageops"

	"gioui.org/gpu/headless"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
)

// referenceDiscShiftPixel is the CPU model of the live preview (and, via
// TestDiscShiftPreviewMatchesCommittedShift in package main, of the committed
// ShiftDisc render) at result-image point (px, py).
func referenceDiscShiftPixel(src *image.NRGBA, g DiscShiftPreview, dx, dy int, angle, px, py float64) [3]float64 {
	mask := imageops.NewCircularMask(g.Radius, g.FeatherSize, g.CutoutRadius)
	rad := (g.Rotation + angle) * math.Pi / 180
	cos, sin := math.Cos(rad), math.Sin(rad)
	rx, ry := px-g.PivotX, py-g.PivotY
	sx := cos*rx + sin*ry + g.SourcePivotX + float64(dx) - 0.5
	sy := -sin*rx + cos*ry + g.SourcePivotY + float64(dy) - 0.5
	x0, y0 := int(math.Floor(sx)), int(math.Floor(sy))
	wx, wy := sx-float64(x0), sy-float64(y0)
	b := src.Bounds()
	at := func(x, y, ch int) float64 {
		x = clamp(x, b.Min.X, b.Max.X-1)
		y = clamp(y, b.Min.Y, b.Max.Y-1)
		return float64(src.Pix[src.PixOffset(x, y)+ch])
	}
	mx, my := px-g.MaskCenterX, py-g.MaskCenterY
	a := mask.Alpha(mx*mx + my*my)
	bg := [3]float64{float64(g.BgR), float64(g.BgG), float64(g.BgB)}
	var out [3]float64
	for ch := 0; ch < 3; ch++ {
		v := at(x0, y0, ch)*(1-wx)*(1-wy) + at(x0+1, y0, ch)*wx*(1-wy) +
			at(x0, y0+1, ch)*(1-wx)*wy + at(x0+1, y0+1, ch)*wx*wy
		out[ch] = a*v + (1-a)*bg[ch]
	}
	return out
}

// Renders the live layer on the GPU and checks it against the CPU model, which
// guards the affine chain (rotation direction, half-pixel pivots, shift sign,
// overlay placement) that unit tests of the geometry cannot see.
func TestDiscShiftLiveGPUMatchesReference(t *testing.T) {
	const vpW, vpH = 320, 300
	win, err := headless.NewWindow(vpW, vpH)
	if err != nil {
		t.Skipf("headless GPU unavailable: %v", err)
	}
	defer win.Release()

	src := image.NewNRGBA(image.Rect(0, 0, 400, 360))
	for y := 0; y < 360; y++ {
		for x := 0; x < 400; x++ {
			src.SetNRGBA(x, y, color.NRGBA{
				R: uint8(120 + 100*math.Sin(float64(x)/9)),
				G: uint8(120 + 100*math.Cos(float64(y)/13)),
				B: uint8(120 + 100*math.Sin(float64(x-y)/17)),
				A: 255,
			})
		}
	}
	// Mirrors DiscShiftPreview for a radius-110 disc at (200,180), rotated
	// 17°, with 6px top / 9px left insets (result 211x214).
	const R = 110
	cropPivot := float64(2*R)/2 + 0.5
	geo := DiscShiftPreview{
		CenterX: 200, CenterY: 180,
		SourcePivotX: float64(200-R) + cropPivot, SourcePivotY: float64(180-R) + cropPivot,
		PivotX: cropPivot - 9, PivotY: cropPivot - 6,
		MaskCenterX: cropPivot - 9, MaskCenterY: cropPivot - 6,
		Rotation: 17,
		Radius:   R, FeatherSize: 14, CutoutRadius: 15,
		BgR: 30, BgG: 200, BgB: 60,
		LevelsWhite: 255,
		Width:       2*R - 9, Height: 2*R - 6,
	}
	info := viewportLayoutInfo{
		ViewportW: vpW, ViewportH: vpH,
		StageX: 40, StageY: 30, StageWidth: float64(geo.Width), StageHeight: float64(geo.Height),
		StageDims: image.Pt(geo.Width, geo.Height),
	}
	layer := buildDiscShiftLiveLayer(geo, src, info, discShiftMaxShift(info))
	if layer == nil {
		t.Fatal("layer not built")
	}
	mask := imageops.NewCircularMask(geo.Radius, geo.FeatherSize, geo.CutoutRadius)
	measure := func(phase string) (mean, featherMean float64) {
		ops := new(op.Ops)
		paint.FillShape(ops, color.NRGBA{A: 255}, clip.Rect(image.Rect(0, 0, vpW, vpH)).Op())
		drawDiscShiftLive(ops, layer, info, image.Rect(0, 0, vpW, vpH))
		if err := win.Frame(ops); err != nil {
			t.Fatal(err)
		}
		shot := image.NewRGBA(image.Rect(0, 0, vpW, vpH))
		if err := win.Screenshot(shot); err != nil {
			t.Fatal(err)
		}
		var sum, n, featherSum, featherN, maxDiff float64
		for y := 0; y < geo.Height; y++ {
			for x := 0; x < geo.Width; x++ {
				px, py := float64(x)+0.5, float64(y)+0.5
				pose := layer.total()
				want := referenceDiscShiftPixel(src, geo, pose.shift.X, pose.shift.Y, pose.angle, px, py)
				o := shot.PixOffset(int(info.StageX)+x, int(info.StageY)+y)
				mx, my := px-geo.MaskCenterX, py-geo.MaskCenterY
				a := mask.Alpha(mx*mx + my*my)
				for ch := 0; ch < 3; ch++ {
					d := math.Abs(float64(shot.Pix[o+ch]) - want[ch])
					sum += d
					n++
					maxDiff = math.Max(maxDiff, d)
					if a > 0.05 && a < 0.95 {
						featherSum += d
						featherN++
					}
				}
			}
		}
		mean, featherMean = sum/n, featherSum/math.Max(1, featherN)
		t.Logf("%s: GPU vs reference mean=%.2f featherMean=%.2f max=%.0f", phase, mean, featherMean, maxDiff)
		return mean, featherMean
	}

	// Press: no movement yet, so the live layer must look like the render.
	if mean, fm := measure("press"); mean > 1.5 || fm > 2.5 {
		t.Fatalf("press: live layer diverges from committed look (mean=%.2f feather=%.2f)", mean, fm)
	}
	// Fine-tuning drag: content moved a little under a vignette tuned for the
	// start content stays close. (This pattern has a ~56px period, so large
	// moves decorrelate the feather completely; release fixes that up.)
	layer.gesture = discLivePose{shift: image.Pt(3, -2)}
	if mean, fm := measure("drag"); mean > 2 || fm > 5 {
		t.Fatalf("drag: live layer diverges (mean=%.2f feather=%.2f)", mean, fm)
	}
	// Fine rotation (~3px of travel at the rim) behaves the same; this also
	// checks the GPU rotation direction and pivot match the reference.
	layer.gesture = discLivePose{angle: 1.5}
	if mean, fm := measure("rotate"); mean > 2 || fm > 5 {
		t.Fatalf("rotate: live layer diverges (mean=%.2f feather=%.2f)", mean, fm)
	}
	// Release after a large move and turn: the vignette is pinned to the
	// exact committed look.
	layer.gesture = discLivePose{}
	layer.base = discLivePose{shift: image.Pt(13, -7), angle: -24}
	layer.overlay = layer.computeOverlay(layer.total(), true)
	if mean, fm := measure("release"); mean > 1.5 || fm > 2.5 {
		t.Fatalf("release: live layer diverges from committed look (mean=%.2f feather=%.2f)", mean, fm)
	}
}
