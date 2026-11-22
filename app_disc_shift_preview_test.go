package main

import (
	"bytes"
	"image"
	"image/color"
	"math"
	"testing"

	"atropos/internal/imageops"
	"atropos/internal/raster"
)

// composeDiscShiftPreview reproduces, at full resolution, what the UI draws
// during a Ctrl+drag: source content shifted by (dx,dy) and mapped through the
// preview geometry and turned by angle degrees, beneath the stationary
// vignette.
func composeDiscShiftPreview(src *image.NRGBA, p *DiscShiftPreview, dx, dy int, angle float64) *image.NRGBA {
	out := image.NewNRGBA(image.Rect(0, 0, p.Width, p.Height))
	mask := imageops.NewCircularMask(p.Radius, p.FeatherSize, p.CutoutRadius)
	rad := (p.Rotation + angle) * math.Pi / 180
	cos, sin := math.Cos(rad), math.Sin(rad)
	levels := func(v float64) float64 {
		if p.LevelsBlack == 0 && p.LevelsWhite == 255 {
			return v
		}
		return (v - float64(p.LevelsBlack)) * 255 / float64(p.LevelsWhite-p.LevelsBlack)
	}
	b := src.Bounds()
	sample := func(sx, sy float64, ch int) float64 {
		// Bilinear on pixel centres, clamped to the source.
		fx, fy := sx-0.5, sy-0.5
		x0, y0 := int(math.Floor(fx)), int(math.Floor(fy))
		wx, wy := fx-float64(x0), fy-float64(y0)
		at := func(x, y int) float64 {
			x = clamp(x, b.Min.X, b.Max.X-1)
			y = clamp(y, b.Min.Y, b.Max.Y-1)
			return float64(src.Pix[src.PixOffset(x, y)+ch])
		}
		return at(x0, y0)*(1-wx)*(1-wy) + at(x0+1, y0)*wx*(1-wy) +
			at(x0, y0+1)*(1-wx)*wy + at(x0+1, y0+1)*wx*wy
	}
	bg := [3]float64{float64(p.BgR), float64(p.BgG), float64(p.BgB)}
	for y := 0; y < p.Height; y++ {
		for x := 0; x < p.Width; x++ {
			px, py := float64(x)+0.5, float64(y)+0.5
			rx, ry := px-p.PivotX, py-p.PivotY
			sx := cos*rx + sin*ry + p.SourcePivotX + float64(dx)
			sy := -sin*rx + cos*ry + p.SourcePivotY + float64(dy)
			mx, my := px-p.MaskCenterX, py-p.MaskCenterY
			a := mask.Alpha(mx*mx + my*my)
			off := out.PixOffset(x, y)
			for ch := 0; ch < 3; ch++ {
				v := a*levels(sample(sx, sy, ch)) + (1-a)*bg[ch]
				out.Pix[off+ch] = uint8(math.Max(0, math.Min(255, math.Round(v))))
			}
			out.Pix[off+3] = 255
		}
	}
	return out
}

func fillDiscShiftTestPattern(img *image.NRGBA, sharp bool) {
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			var r, g, bl float64
			if sharp {
				r = float64((x*7 + y*13) % 200)
				g = float64((x*3 + y*29) % 200)
				bl = float64((x*x + y) % 200)
			} else {
				r = 100 + 90*math.Sin(float64(x)/23)
				g = 100 + 90*math.Cos(float64(y)/31)
				bl = 100 + 90*math.Sin(float64(x+y)/41)
			}
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(20 + r), G: uint8(20 + g), B: uint8(20 + bl), A: 255})
		}
	}
}

func diffStats(t *testing.T, got, want *image.NRGBA) (mean float64, maxDiff int) {
	t.Helper()
	if got.Bounds().Size() != want.Bounds().Size() {
		t.Fatalf("size mismatch: got %v want %v", got.Bounds().Size(), want.Bounds().Size())
	}
	var sum, n float64
	for y := 0; y < want.Bounds().Dy(); y++ {
		for x := 0; x < want.Bounds().Dx(); x++ {
			go1 := got.PixOffset(got.Bounds().Min.X+x, got.Bounds().Min.Y+y)
			wo := want.PixOffset(x, y)
			for ch := 0; ch < 3; ch++ {
				d := int(got.Pix[go1+ch]) - int(want.Pix[wo+ch])
				if d < 0 {
					d = -d
				}
				sum += float64(d)
				n++
				maxDiff = max(maxDiff, d)
			}
		}
	}
	return sum / n, maxDiff
}

// The live Ctrl+drag preview must match what ShiftDisc commits, so releasing
// the mouse does not visibly change the viewport.
func TestDiscShiftPreviewMatchesCommittedShift(t *testing.T) {
	cases := []struct {
		name      string
		sharp     bool
		rotate    float64
		crops     []CropRequest
		levels    [2]int
		dx, dy    int
		maxMean   float64
		maxSingle int
	}{
		{name: "plain", sharp: true, dx: 9, dy: -6, maxMean: 0.5, maxSingle: 1},
		{name: "insets+levels", sharp: true, dx: -13, dy: 4,
			crops:  []CropRequest{{Direction: "top", Amount: 7}, {Direction: "left", Amount: 11}, {Direction: "right", Amount: 3}},
			levels: [2]int{10, 240}, maxMean: 0.6, maxSingle: 3},
		{name: "rotated+insets", rotate: 17, dx: 12, dy: 8,
			crops:   []CropRequest{{Direction: "bottom", Amount: 6}, {Direction: "left", Amount: 9}},
			maxMean: 0.6, maxSingle: 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := newTestApp(320, 300)
			fillDiscShiftTestPattern(a.currentImage, tc.sharp)
			a.bgColor = color.NRGBA{R: 200, G: 40, B: 90, A: 255}
			a.featherSize = 12
			a.discCenterCutout = true
			a.discCutoutPercent = 15
			if _, err := a.DrawDisc(DiscDrawRequest{CenterX: 160, CenterY: 150, Radius: 90}); err != nil {
				t.Fatal(err)
			}
			if tc.rotate != 0 {
				if _, err := a.RotateDisc(DiscRotateRequest{Angle: tc.rotate}); err != nil {
					t.Fatal(err)
				}
			}
			for _, c := range tc.crops {
				if _, err := a.Crop(c); err != nil {
					t.Fatal(err)
				}
			}
			if tc.levels != ([2]int{}) {
				a.postDiscBlack, a.postDiscWhite = tc.levels[0], tc.levels[1]
				if _, err := a.redrawDisc(); err != nil {
					t.Fatal(err)
				}
			}

			preview, err := a.DiscShiftPreview()
			if err != nil {
				t.Fatal(err)
			}
			src, ok := a.ResolvePreviewImage(preview.Source)
			if !ok || src != a.discBaseImage {
				t.Fatal("preview source does not resolve to the disc base image")
			}
			if b := a.warpedImage.Bounds(); preview.Width != b.Dx() || preview.Height != b.Dy() {
				t.Fatalf("preview dims %dx%d, warped %v", preview.Width, preview.Height, b)
			}

			// Before any shift the preview must already match the current render.
			mean, maxD := diffStats(t, a.warpedImage, composeDiscShiftPreview(src, preview, 0, 0, 0))
			if mean > tc.maxMean || maxD > tc.maxSingle {
				t.Fatalf("unshifted preview differs from render: mean=%.3f max=%d", mean, maxD)
			}

			want := composeDiscShiftPreview(src, preview, tc.dx, tc.dy, 0)
			if _, err := a.ShiftDisc(ShiftDiscRequest{DX: tc.dx, DY: tc.dy}); err != nil {
				t.Fatal(err)
			}
			mean, maxD = diffStats(t, a.warpedImage, want)
			t.Logf("committed vs preview: mean=%.3f max=%d", mean, maxD)
			if mean > tc.maxMean || maxD > tc.maxSingle {
				t.Fatalf("shift preview differs from committed shift: mean=%.3f max=%d", mean, maxD)
			}
		})
	}
}

// The live preview is unavailable without a disc and stays available after
// post-disc pixel edits, which now live in the disc source.
func TestDiscShiftPreviewAvailability(t *testing.T) {
	a := newTestApp(200, 200)
	if _, err := a.DiscShiftPreview(); err == nil {
		t.Fatal("expected error with no disc")
	}
	if _, err := a.DrawDisc(DiscDrawRequest{CenterX: 100, CenterY: 100, Radius: 60}); err != nil {
		t.Fatal(err)
	}
	edited := raster.CloneNRGBA(a.discBaseImage)
	edited.SetNRGBA(100, 100, color.NRGBA{R: 255, A: 255})
	if _, err := a.installDiscSource(edited); err != nil {
		t.Fatal(err)
	}
	preview, err := a.DiscShiftPreview()
	if err != nil {
		t.Fatalf("preview unavailable after a post-disc edit: %v", err)
	}
	if src, ok := a.ResolvePreviewImage(preview.Source); !ok || src != edited {
		t.Fatal("preview source is not the edited disc source")
	}
}

// Every disc shift is undoable: a Ctrl+drag shift is its own step, and undo
// returns to the pre-shift crop rather than past the disc.
func TestShiftDiscUndoSteps(t *testing.T) {
	a := newTestApp(320, 300)
	fillDiscShiftTestPattern(a.currentImage, true)
	if _, err := a.DrawDisc(DiscDrawRequest{CenterX: 160, CenterY: 150, Radius: 90}); err != nil {
		t.Fatal(err)
	}
	drawn := raster.CloneNRGBA(a.warpedImage)
	steps := len(a.undoStack)

	if _, err := a.ShiftDisc(ShiftDiscRequest{DX: 12, DY: -7}); err != nil {
		t.Fatal(err)
	}
	first := raster.CloneNRGBA(a.warpedImage)
	if _, err := a.ShiftDisc(ShiftDiscRequest{DX: 5, DY: 9}); err != nil {
		t.Fatal(err)
	}
	if len(a.undoStack) != steps+2 {
		t.Fatalf("undo steps = %d, want %d", len(a.undoStack), steps+2)
	}

	res, err := a.Undo()
	if err != nil {
		t.Fatal(err)
	}
	if a.discCenter != image.Pt(172, 143) || res.DiscCenterX != 172 || res.DiscCenterY != 143 || !bytes.Equal(a.warpedImage.Pix, first.Pix) {
		t.Fatalf("undo of second drag: centre=%v result=(%d,%d)", a.discCenter, res.DiscCenterX, res.DiscCenterY)
	}
	if _, err := a.Undo(); err != nil {
		t.Fatal(err)
	}
	if a.discRadius != 90 || a.discCenter != image.Pt(160, 150) || !bytes.Equal(a.warpedImage.Pix, drawn.Pix) {
		t.Fatalf("undo of first drag did not restore the drawn crop: radius=%d centre=%v", a.discRadius, a.discCenter)
	}
	if _, err := a.Redo(); err != nil {
		t.Fatal(err)
	}
	if a.discCenter != image.Pt(172, 143) || !bytes.Equal(a.warpedImage.Pix, first.Pix) {
		t.Fatalf("redo did not re-apply the first drag: centre=%v", a.discCenter)
	}

	// A shift fully clamped at the image edge changes nothing and records nothing.
	b := newTestApp(200, 200)
	if _, err := b.DrawDisc(DiscDrawRequest{CenterX: 100, CenterY: 100, Radius: 100}); err != nil {
		t.Fatal(err)
	}
	n := len(b.undoStack)
	for _, nudge := range []bool{false, true} {
		if _, err := b.ShiftDisc(ShiftDiscRequest{DX: 30, DY: 0, Nudge: nudge}); err != nil {
			t.Fatal(err)
		}
	}
	if len(b.undoStack) != n {
		t.Fatal("no-op clamped shift added an undo step")
	}
}

// An unbroken run of keyboard nudges is one undo step; any other operation
// in between (a Ctrl+drag, a re-render such as a feather change, an undo)
// starts a new one.
func TestShiftDiscNudgesCoalesce(t *testing.T) {
	a := newTestApp(320, 300)
	fillDiscShiftTestPattern(a.currentImage, true)
	if _, err := a.DrawDisc(DiscDrawRequest{CenterX: 160, CenterY: 150, Radius: 90}); err != nil {
		t.Fatal(err)
	}
	nudge := func(dx, dy int) {
		t.Helper()
		if _, err := a.ShiftDisc(ShiftDiscRequest{DX: dx, DY: dy, Nudge: true}); err != nil {
			t.Fatal(err)
		}
	}
	base := len(a.undoStack)
	start := a.discCenter

	for i := 0; i < 30; i++ {
		nudge(5, 0)
	}
	nudge(0, -5)
	if got := len(a.undoStack); got != base+1 {
		t.Fatalf("31 consecutive nudges made %d undo steps, want 1", got-base)
	}
	if _, err := a.Undo(); err != nil {
		t.Fatal(err)
	}
	if a.discCenter != start {
		t.Fatalf("undo of nudge run: centre=%v, want %v", a.discCenter, start)
	}

	// After an undo, nudging starts a fresh step.
	nudge(5, 0)
	nudge(5, 0)
	if got := len(a.undoStack); got != base+1 {
		t.Fatalf("nudges after undo made %d steps, want 1", got-base)
	}
	// A Ctrl+drag splits the run on both sides.
	if _, err := a.ShiftDisc(ShiftDiscRequest{DX: -8, DY: 3}); err != nil {
		t.Fatal(err)
	}
	nudge(0, 5)
	if got := len(a.undoStack); got != base+3 {
		t.Fatalf("nudge / drag / nudge made %d steps, want 3", got-base)
	}
	// So does any other re-render, e.g. a feather change.
	if _, err := a.SetFeatherSize(FeatherSizeRequest{Size: 30}); err != nil {
		t.Fatal(err)
	}
	beforeFeatherRun := len(a.undoStack)
	nudge(0, 5)
	nudge(0, 5)
	if got := len(a.undoStack); got != beforeFeatherRun+1 {
		t.Fatalf("nudges after a feather change made %d steps, want 1", got-beforeFeatherRun)
	}
	centre := a.discCenter
	if _, err := a.Undo(); err != nil {
		t.Fatal(err)
	}
	if a.discCenter != centre.Sub(image.Pt(0, 10)) {
		t.Fatalf("undo of post-feather nudge run: centre=%v, want %v", a.discCenter, centre.Sub(image.Pt(0, 10)))
	}
}

// A live Shift+drag rotates the content about the preview pivot under the
// fixed vignette; that must match what RotateDisc commits, including with
// edge crops and an existing rotation.
func TestDiscShiftPreviewMatchesCommittedRotation(t *testing.T) {
	a := newTestApp(320, 300)
	fillDiscShiftTestPattern(a.currentImage, false)
	a.bgColor = color.NRGBA{R: 200, G: 40, B: 90, A: 255}
	a.featherSize = 12
	a.discCenterCutout = true
	a.discCutoutPercent = 15
	if _, err := a.DrawDisc(DiscDrawRequest{CenterX: 160, CenterY: 150, Radius: 90}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.RotateDisc(DiscRotateRequest{Angle: 9}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []CropRequest{{Direction: "top", Amount: 5}, {Direction: "right", Amount: 8}} {
		if _, err := a.Crop(c); err != nil {
			t.Fatal(err)
		}
	}
	preview, err := a.DiscShiftPreview()
	if err != nil {
		t.Fatal(err)
	}
	src, _ := a.ResolvePreviewImage(preview.Source)
	want := composeDiscShiftPreview(src, preview, 0, 0, -23.5)
	if _, err := a.RotateDisc(DiscRotateRequest{Angle: -23.5}); err != nil {
		t.Fatal(err)
	}
	mean, maxD := diffStats(t, a.warpedImage, want)
	t.Logf("committed vs preview: mean=%.3f max=%d", mean, maxD)
	if mean > 0.6 || maxD > 4 {
		t.Fatalf("rotation preview differs from committed rotation: mean=%.3f max=%d", mean, maxD)
	}
}

// Shift+drag rotations are their own undo steps; Q/E rotations are nudges
// and share one step with an unbroken run of arrow-key nudges.
func TestRotateDiscUndoSteps(t *testing.T) {
	a := newTestApp(320, 300)
	fillDiscShiftTestPattern(a.currentImage, true)
	if _, err := a.DrawDisc(DiscDrawRequest{CenterX: 160, CenterY: 150, Radius: 90}); err != nil {
		t.Fatal(err)
	}
	base := len(a.undoStack)
	for _, angle := range []float64{7.5, -2} {
		if _, err := a.RotateDisc(DiscRotateRequest{Angle: angle}); err != nil {
			t.Fatal(err)
		}
	}
	if got := len(a.undoStack); got != base+2 {
		t.Fatalf("two Shift+drag rotations made %d steps, want 2", got-base)
	}
	if _, err := a.Undo(); err != nil {
		t.Fatal(err)
	}
	if a.rotationAngle != 7.5 {
		t.Fatalf("undo of second rotation: angle=%v, want 7.5", a.rotationAngle)
	}

	// Q, E, arrow, Q: one unbroken nudge run, one step.
	before, centre := len(a.undoStack), a.discCenter
	for _, step := range []func() error{
		func() error { _, err := a.RotateDisc(DiscRotateRequest{Angle: -15, Nudge: true}); return err },
		func() error { _, err := a.RotateDisc(DiscRotateRequest{Angle: 15, Nudge: true}); return err },
		func() error { _, err := a.ShiftDisc(ShiftDiscRequest{DX: 5, Nudge: true}); return err },
		func() error { _, err := a.RotateDisc(DiscRotateRequest{Angle: -15, Nudge: true}); return err },
	} {
		if err := step(); err != nil {
			t.Fatal(err)
		}
	}
	if got := len(a.undoStack); got != before+1 {
		t.Fatalf("mixed nudge run made %d steps, want 1", got-before)
	}
	if _, err := a.Undo(); err != nil {
		t.Fatal(err)
	}
	if a.rotationAngle != 7.5 || a.discCenter != centre {
		t.Fatalf("undo of nudge run: angle=%v centre=%v, want 7.5 %v", a.rotationAngle, a.discCenter, centre)
	}
	if _, err := a.RotateDisc(DiscRotateRequest{}); err != nil {
		t.Fatal(err)
	}
	if got := len(a.undoStack); got != before {
		t.Fatal("zero rotation added an undo step")
	}
}
