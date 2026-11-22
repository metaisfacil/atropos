package main

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"math"
	"testing"
	"time"

	"atropos/internal/imageops"
	"atropos/internal/raster"
)

// newEditedDiscApp draws a rotated disc with asymmetric edge crops over a
// smooth pattern, so the source/result mapping is non-trivial.
func newEditedDiscApp(t *testing.T) *App {
	t.Helper()
	// Large enough that the edit region (disc plus one radius) leaves a border.
	a := newTestApp(520, 500)
	fillDiscShiftTestPattern(a.currentImage, false)
	a.discCenterCutout = false
	if _, err := a.DrawDisc(DiscDrawRequest{CenterX: 240, CenterY: 230, Radius: 90}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.RotateDisc(DiscRotateRequest{Angle: 17}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []CropRequest{{Direction: "top", Amount: 5}, {Direction: "left", Amount: 8}} {
		if _, err := a.Crop(c); err != nil {
			t.Fatal(err)
		}
	}
	return a
}

func pixelDiff(a, b color.NRGBA) int {
	d := 0
	for _, v := range [][2]uint8{{a.R, b.R}, {a.G, b.G}, {a.B, b.B}} {
		x := int(v[0]) - int(v[1])
		if x < 0 {
			x = -x
		}
		d = max(d, x)
	}
	return d
}

func TestDiscSourceMapRoundTripsAndMatchesPreview(t *testing.T) {
	a := newEditedDiscApp(t)
	a.discScale = discScale{x: 1.5, y: 0.75}
	m := a.discSourceMapping()
	for _, p := range [][2]float64{{0, 0}, {37.25, 81.5}, {170, 12}} {
		sx, sy := m.toSource(p[0], p[1])
		rx, ry := m.toResult(sx, sy)
		if math.Abs(rx-p[0]) > 1e-9 || math.Abs(ry-p[1]) > 1e-9 {
			t.Fatalf("round trip of %v gave (%v,%v)", p, rx, ry)
		}
	}
	preview, err := a.DiscShiftPreview()
	if err != nil {
		t.Fatal(err)
	}
	if preview.SourcePivotX != m.srcPivotX || preview.PivotY != m.resPivotY || preview.Rotation != a.rotationAngle {
		t.Fatalf("preview geometry %+v disagrees with the source map %+v", preview, m)
	}
	if pre := a.discPreScaleSize(); preview.Width != pre.X || preview.Height != pre.Y {
		t.Fatalf("preview dims %dx%d, want unscaled %v", preview.Width, preview.Height, pre)
	}
}

// Selection-scoped levels edit the source pixels under the (rotated)
// selection; slider ticks don't stack, the edit follows the disc art when
// shifted, and pixels outside the selection are untouched.
func TestDiscSelectionLevelsEditTheSource(t *testing.T) {
	a := newEditedDiscApp(t)
	orig := a.discBaseImage
	before := raster.CloneNRGBA(a.warpedImage)
	steps := len(a.undoStack)
	sel := &AdjustmentSelection{X1: 40, Y1: 50, X2: 100, Y2: 110}
	if _, err := a.SetLevels(SetLevelsRequest{Black: 60, White: 200, Selection: sel}); err != nil {
		t.Fatal(err)
	}
	first := a.discBaseImage
	if first == orig {
		t.Fatal("selection levels did not edit the disc source")
	}
	// Inside: result pixel is the stretched original; outside: unchanged.
	m := a.discSourceMapping()
	sx, sy := m.toSource(70.5, 80.5)
	want := imageops.ApplyLevels(raster.CropNRGBA(orig, image.Rect(int(sx), int(sy), int(sx)+1, int(sy)+1)), 60, 200).NRGBAAt(0, 0)
	if got := first.NRGBAAt(int(sx), int(sy)); got != want {
		t.Fatalf("source pixel under selection = %+v, want %+v", got, want)
	}
	if d := pixelDiff(a.warpedImage.NRGBAAt(70, 80), imageops.ApplyLevels(raster.CropNRGBA(before, image.Rect(70, 80, 71, 81)), 60, 200).NRGBAAt(0, 0)); d > 3 {
		t.Fatalf("result inside the selection is off by %d", d)
	}
	if d := pixelDiff(a.warpedImage.NRGBAAt(140, 140), before.NRGBAAt(140, 140)); d > 1 {
		t.Fatalf("result outside the selection changed by %d", d)
	}
	if got, ref := first.NRGBAAt(10, 10), orig.NRGBAAt(10, 10); got != ref {
		t.Fatal("source outside the selection changed")
	}

	// A second tick re-applies against the session base instead of stacking.
	if _, err := a.SetLevels(SetLevelsRequest{Black: 60, White: 200, Selection: sel}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.discBaseImage.Pix, first.Pix) {
		t.Fatal("repeated slider tick stacked the levels")
	}
	if len(a.undoStack) != steps {
		t.Fatal("slider ticks recorded undo steps")
	}

	// The disc stays live and the edit stays on the source.
	edited := a.discBaseImage
	if _, err := a.ShiftDisc(ShiftDiscRequest{DX: 12, DY: -4}); err != nil {
		t.Fatal(err)
	}
	if a.discBaseImage != edited || a.discCenter != image.Pt(252, 226) {
		t.Fatal("shift after a source edit lost the edit or did not move the disc")
	}
}

// Descreen edits the disc's edit region of the source; re-running with new
// parameters re-filters the same base (one undo step, no DescreenReset).
func TestDiscDescreenSessionEditsTheSource(t *testing.T) {
	a := newEditedDiscApp(t)
	orig := a.discBaseImage
	steps := len(a.undoStack)
	req := DescreenRequest{Thresh: 93, Radius: 6, Middle: 6, Highlight: 50, Fast: true}
	res, err := a.Descreen(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.DescreenReset {
		t.Fatal("descreen reported DescreenReset for its own session")
	}
	first := raster.CloneNRGBA(a.discBaseImage)
	if _, err := a.Descreen(req); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.discBaseImage.Pix, first.Pix) {
		t.Fatal("re-running descreen stacked on its own output")
	}
	if len(a.undoStack) != steps+1 {
		t.Fatalf("descreen session made %d undo steps, want 1", len(a.undoStack)-steps)
	}
	region := a.discEditRegion()
	if region == a.discBaseImage.Bounds() {
		t.Fatal("test scan too small to check the edit region")
	}
	// Outside the edit region the source is untouched.
	for _, p := range []image.Point{{region.Min.X - 1, region.Min.Y - 1}, {region.Max.X, region.Max.Y}} {
		if p.In(orig.Bounds()) && a.discBaseImage.NRGBAAt(p.X, p.Y) != orig.NRGBAAt(p.X, p.Y) {
			t.Fatalf("descreen changed source pixel %v outside the edit region", p)
		}
	}
	if _, err := a.Undo(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.discBaseImage.Pix, orig.Pix) {
		t.Fatal("undo did not restore the pre-descreen source")
	}
}

// Dust removal repairs specks in the source under the disc, so the repair
// survives shifting; a scan without dust records nothing.
func TestDiscDustRemovalEditsTheSource(t *testing.T) {
	a := newTestApp(360, 340)
	for i := range a.currentImage.Pix {
		if i%4 != 3 {
			a.currentImage.Pix[i] = 120
		}
	}
	a.discCenterCutout = false
	if _, err := a.DrawDisc(DiscDrawRequest{CenterX: 180, CenterY: 170, Radius: 90}); err != nil {
		t.Fatal(err)
	}
	steps := len(a.undoStack)
	res, err := a.DustRemoval(DustRemovalRequest{Level: "high", DPI: 600})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || len(a.undoStack) != steps {
		t.Fatal("dust removal on a clean scan recorded a change")
	}

	specked := raster.CloneNRGBA(a.discBaseImage)
	for _, p := range []image.Point{{170, 160}, {200, 190}} {
		for y := p.Y; y < p.Y+2; y++ {
			for x := p.X; x < p.X+2; x++ {
				specked.SetNRGBA(x, y, color.NRGBA{R: 250, G: 250, B: 250, A: 255})
			}
		}
	}
	if _, err := a.installDiscSource(specked); err != nil {
		t.Fatal(err)
	}
	res, err = a.DustRemoval(DustRemovalRequest{Level: "high", DPI: 600})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || len(a.undoStack) != steps+1 {
		t.Fatalf("dust removal did not record its repair (changed=%v steps=%d)", res.Changed, len(a.undoStack)-steps)
	}
	if got := a.discBaseImage.NRGBAAt(170, 160); got.R > 200 {
		t.Fatalf("speck in the source not repaired: %+v", got)
	}
	if _, err := a.ShiftDisc(ShiftDiscRequest{DX: 20}); err != nil {
		t.Fatal(err)
	}
	if got := a.discBaseImage.NRGBAAt(200, 190); got.R > 200 {
		t.Fatalf("repair lost after shifting: %+v", got)
	}
}

// 90° rotation, Trim borders and Resize are disc parameters now, so the disc
// stays live (shiftable) after each, and undo restores them.
func TestDiscRotate90TrimAndResizeStayLive(t *testing.T) {
	a := newEditedDiscApp(t)
	before := raster.CloneNRGBA(a.warpedImage)
	angle, insets := a.rotationAngle, a.discCropInsets
	if _, err := a.Rotate(RotateRequest{FlipCode: 1}); err != nil {
		t.Fatal(err)
	}
	if a.rotationAngle != angle+90 {
		t.Fatalf("rotationAngle = %v, want %v", a.rotationAngle, angle+90)
	}
	if want := (discCropInsets{top: insets.left, right: insets.top, bottom: insets.right, left: insets.bottom}); a.discCropInsets != want {
		t.Fatalf("insets after CW = %+v, want %+v", a.discCropInsets, want)
	}
	// imageops.Rotate pivots on pixel index w/2 rather than the true centre,
	// so the disc rotation can land one pixel off an exact 90° flip.
	flipped := imageops.Rotate90(before, 1)
	best := math.Inf(1)
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			best = math.Min(best, shiftedMeanDiff(a.warpedImage, flipped, dx, dy))
		}
	}
	if best > 1 {
		t.Fatalf("disc rotate 90 differs from rotating the render: best mean=%.2f", best)
	}

	// Trim borders: the white corners become insets.
	b := a.warpedImage.Bounds()
	if _, err := a.TrimBorders(); err != nil {
		t.Fatal(err)
	}
	trimmed := a.warpedImage.Bounds()
	if trimmed.Dx() >= b.Dx() && trimmed.Dy() >= b.Dy() {
		t.Fatal("trim borders found nothing to trim")
	}
	if _, err := a.ShiftDisc(ShiftDiscRequest{DX: 6}); err != nil {
		t.Fatal(err)
	}
	if a.warpedImage.Bounds() != trimmed {
		t.Fatalf("shift after trim changed the size: %v, want %v", a.warpedImage.Bounds(), trimmed)
	}

	// Resize: an output scale that survives shifts and converts crop amounts.
	w, h := 2*trimmed.Dx(), trimmed.Dy()+30
	if _, err := a.ResizeImage(ResizeRequest{Width: w, Height: h}); err != nil {
		t.Fatal(err)
	}
	if got := a.warpedImage.Bounds().Size(); got != image.Pt(w, h) {
		t.Fatalf("resized to %v, want %dx%d", got, w, h)
	}
	if _, err := a.ShiftDisc(ShiftDiscRequest{DY: 5}); err != nil {
		t.Fatal(err)
	}
	if got := a.warpedImage.Bounds().Size(); got != image.Pt(w, h) {
		t.Fatalf("shift after resize gave %v, want %dx%d", got, w, h)
	}
	if _, err := a.Crop(CropRequest{Direction: "left", Amount: 20}); err != nil {
		t.Fatal(err)
	}
	if got := a.warpedImage.Bounds().Dx(); got < w-21 || got > w-19 {
		t.Fatalf("crop of 20 output px left width %d, want ~%d", got, w-20)
	}
	for i := 0; i < 3; i++ { // crop, shift, resize
		if _, err := a.Undo(); err != nil {
			t.Fatal(err)
		}
	}
	if !a.discScale.identity() || a.warpedImage.Bounds() != trimmed {
		t.Fatalf("undo did not restore the unscaled disc: scale=%+v size=%v", a.discScale, a.warpedImage.Bounds())
	}
}

type touchupEventHost struct {
	saveDialogStubHost
	done chan touchUpDoneEvent
}

func (h *touchupEventHost) EmitTouchupDone(ev touchUpDoneEvent) { h.done <- ev }

// A touch-up stroke drawn on the rotated, cropped result fills the matching
// source pixels, so the repair follows the disc art.
func TestDiscTouchUpStrokesFillTheSource(t *testing.T) {
	green := color.NRGBA{G: 255, A: 255}
	prevFill := patchMatchFill
	patchMatchFill = func(_ context.Context, src *image.NRGBA, mask *image.Alpha, _, _ int) (*image.NRGBA, error) {
		out := raster.CloneNRGBA(src)
		b := mask.Bounds()
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				if mask.AlphaAt(x, y).A > 0 {
					out.SetNRGBA(x, y, green)
				}
			}
		}
		return out, nil
	}
	defer func() { patchMatchFill = prevFill }()

	a := newEditedDiscApp(t)
	host := &touchupEventHost{done: make(chan touchUpDoneEvent, 1)}
	a.host = host
	a.touchupBackend = "patchmatch"
	orig := a.discBaseImage
	at := TouchUpPoint{X: 60, Y: 75}
	if _, err := a.TouchUpApplyStrokes(TouchUpStrokeRequest{Points: []TouchUpPoint{at}, BrushSize: 12, PatchSize: 7, Iterations: 3}); err != nil {
		t.Fatal(err)
	}
	var ev touchUpDoneEvent
	select {
	case ev = <-host.done:
	case <-time.After(10 * time.Second):
		t.Fatal("touch-up did not finish")
	}
	if ev.Error != "" || ev.Cancelled || ev.Preview == "" {
		t.Fatalf("touch-up event %+v", ev)
	}
	if b := a.warpedImage.Bounds(); ev.Width != b.Dx() || ev.Height != b.Dy() {
		t.Fatalf("event dims %dx%d, want the result's %v", ev.Width, ev.Height, b.Size())
	}
	if a.discBaseImage == orig {
		t.Fatal("touch-up did not edit the disc source")
	}
	m := a.discSourceMapping()
	sx, sy := m.toSource(at.X+0.5, at.Y+0.5)
	if got := a.discBaseImage.NRGBAAt(int(sx), int(sy)); got != green {
		t.Fatalf("source under the stroke = %+v, want green", got)
	}
	if d := pixelDiff(a.warpedImage.NRGBAAt(int(at.X), int(at.Y)), green); d > 2 {
		t.Fatalf("result under the stroke is off by %d", d)
	}
	if got, ref := a.discBaseImage.NRGBAAt(int(sx)+20, int(sy)), orig.NRGBAAt(int(sx)+20, int(sy)); got != ref {
		t.Fatal("touch-up changed source pixels away from the stroke")
	}
}

// shiftedMeanDiff compares got against want offset by (dx, dy) over their
// overlap, ignoring a 2px border.
func shiftedMeanDiff(got, want *image.NRGBA, dx, dy int) float64 {
	b := got.Bounds().Intersect(want.Bounds().Add(image.Pt(dx, dy))).Inset(2)
	var sum, n float64
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			sum += float64(pixelDiff(got.NRGBAAt(x, y), want.NRGBAAt(x-dx, y-dy)))
			n++
		}
	}
	return sum / math.Max(1, n)
}
