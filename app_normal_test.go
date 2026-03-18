package main

import (
	"image"
	"image/color"
	"strings"
	"testing"
)

// newTestApp returns an App with a w×h solid-colour image pre-loaded into
// currentImage and originalImage. The pixels are set to a known colour so
// that individual pixels can be verified in tests.
func newTestApp(w, h int) *App {
	a := NewApp()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: 128, G: 64, B: 32, A: 255})
		}
	}
	a.currentImage = img
	a.originalImage = cloneImage(img)
	return a
}

// ---- NormalCrop ----

func TestNormalCrop_NoImage(t *testing.T) {
	a := NewApp()
	_, err := a.NormalCrop(NormalCropRequest{X1: 0, Y1: 0, X2: 10, Y2: 10})
	if err == nil {
		t.Fatal("expected error when no image is loaded")
	}
}

func TestNormalCrop_BasicCrop(t *testing.T) {
	a := newTestApp(100, 80)
	res, err := a.NormalCrop(NormalCropRequest{X1: 10, Y1: 10, X2: 60, Y2: 50})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Width != 50 || res.Height != 40 {
		t.Fatalf("expected 50×40, got %d×%d", res.Width, res.Height)
	}
}

func TestNormalCrop_ResultMessage(t *testing.T) {
	a := newTestApp(100, 80)
	res, err := a.NormalCrop(NormalCropRequest{X1: 0, Y1: 0, X2: 40, Y2: 30})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Message != "Cropped to 40×30" {
		t.Fatalf("unexpected message: %q", res.Message)
	}
}

func TestNormalCrop_ResultPreview(t *testing.T) {
	a := newTestApp(100, 80)
	res, err := a.NormalCrop(NormalCropRequest{X1: 0, Y1: 0, X2: 50, Y2: 50})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(res.Preview, "data:image/") {
		t.Fatalf("preview is not a data URI: %q", res.Preview[:min(len(res.Preview), 50)])
	}
}

func TestNormalCrop_NormalisesCoords(t *testing.T) {
	a := newTestApp(100, 80)
	// Provide reversed coordinates: x1>x2 and y1>y2.
	res, err := a.NormalCrop(NormalCropRequest{X1: 60, Y1: 50, X2: 10, Y2: 10})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Width != 50 || res.Height != 40 {
		t.Fatalf("expected 50×40 after coordinate normalisation, got %d×%d", res.Width, res.Height)
	}
}

func TestNormalCrop_ClampsToImageBounds(t *testing.T) {
	a := newTestApp(100, 80)
	// Request a region that extends well outside the image.
	res, err := a.NormalCrop(NormalCropRequest{X1: -50, Y1: -50, X2: 300, Y2: 300})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// After clamping the result should equal the full image size.
	if res.Width != 100 || res.Height != 80 {
		t.Fatalf("expected 100×80 after clamping, got %d×%d", res.Width, res.Height)
	}
}

func TestNormalCrop_EmptyRegion_SamePoint(t *testing.T) {
	a := newTestApp(100, 80)
	_, err := a.NormalCrop(NormalCropRequest{X1: 20, Y1: 20, X2: 20, Y2: 40})
	if err == nil {
		t.Fatal("expected error for zero-width region")
	}
}

func TestNormalCrop_EmptyRegion_OutsideBounds(t *testing.T) {
	a := newTestApp(100, 80)
	// Both x values are beyond the right edge; after clamping they collapse.
	_, err := a.NormalCrop(NormalCropRequest{X1: 200, Y1: 0, X2: 300, Y2: 80})
	if err == nil {
		t.Fatal("expected error when region collapses to empty after clamping")
	}
}

func TestNormalCrop_SetsWarpedImage(t *testing.T) {
	a := newTestApp(100, 80)
	if a.warpedImage != nil {
		t.Fatal("warpedImage should be nil before first crop")
	}
	_, err := a.NormalCrop(NormalCropRequest{X1: 0, Y1: 0, X2: 50, Y2: 50})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.warpedImage == nil {
		t.Fatal("warpedImage should be set after NormalCrop")
	}
}

func TestNormalCrop_SavesUndo(t *testing.T) {
	a := newTestApp(100, 80)
	before := len(a.undoStack)
	_, err := a.NormalCrop(NormalCropRequest{X1: 0, Y1: 0, X2: 50, Y2: 50})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(a.undoStack) != before+1 {
		t.Fatalf("expected undo stack depth %d, got %d", before+1, len(a.undoStack))
	}
}

func TestNormalCrop_WorksOnCurrentImage(t *testing.T) {
	// When warpedImage is nil, NormalCrop should crop from currentImage.
	a := newTestApp(100, 80)
	res, err := a.NormalCrop(NormalCropRequest{X1: 5, Y1: 5, X2: 55, Y2: 45})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Width != 50 || res.Height != 40 {
		t.Fatalf("expected 50×40, got %d×%d", res.Width, res.Height)
	}
}

func TestNormalCrop_WorksOnWarpedImage(t *testing.T) {
	// When warpedImage is already set (e.g. from a previous operation), the
	// next NormalCrop must crop from warpedImage, not currentImage.
	a := newTestApp(100, 80)

	// Manually install a warpedImage that is smaller than currentImage.
	warped := image.NewNRGBA(image.Rect(0, 0, 60, 50))
	for y := 0; y < 50; y++ {
		for x := 0; x < 60; x++ {
			warped.SetNRGBA(x, y, color.NRGBA{R: 200, G: 100, B: 50, A: 255})
		}
	}
	a.warpedImage = warped

	res, err := a.NormalCrop(NormalCropRequest{X1: 0, Y1: 0, X2: 30, Y2: 25})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Width != 30 || res.Height != 25 {
		t.Fatalf("expected 30×25, got %d×%d", res.Width, res.Height)
	}
}

func TestNormalCrop_SequentialCropsEachSaveUndo(t *testing.T) {
	a := newTestApp(200, 200)
	for i := 0; i < 3; i++ {
		_, err := a.NormalCrop(NormalCropRequest{X1: 0, Y1: 0, X2: 180 - i*20, Y2: 180 - i*20})
		if err != nil {
			t.Fatalf("crop %d failed: %v", i, err)
		}
	}
	if len(a.undoStack) != 3 {
		t.Fatalf("expected 3 undo entries after 3 crops, got %d", len(a.undoStack))
	}
}

func TestNormalCrop_SequentialCropsOperateOnResult(t *testing.T) {
	// Each subsequent NormalCrop operates on the already-cropped warpedImage.
	a := newTestApp(200, 200)

	_, err := a.NormalCrop(NormalCropRequest{X1: 0, Y1: 0, X2: 100, Y2: 100})
	if err != nil {
		t.Fatalf("first crop failed: %v", err)
	}
	// Now warpedImage is 100×100. Crop that to 50×50.
	res, err := a.NormalCrop(NormalCropRequest{X1: 0, Y1: 0, X2: 50, Y2: 50})
	if err != nil {
		t.Fatalf("second crop failed: %v", err)
	}
	if res.Width != 50 || res.Height != 50 {
		t.Fatalf("expected 50×50 after second crop, got %d×%d", res.Width, res.Height)
	}
}

func TestNormalCrop_UndoRestoresPreCropImage(t *testing.T) {
	a := newTestApp(100, 80)

	// Crop down to 50×40.
	_, err := a.NormalCrop(NormalCropRequest{X1: 0, Y1: 0, X2: 50, Y2: 40})
	if err != nil {
		t.Fatalf("crop failed: %v", err)
	}
	if a.warpedImage.Bounds().Dx() != 50 {
		t.Fatalf("expected 50-wide warpedImage after crop, got %d", a.warpedImage.Bounds().Dx())
	}

	// Undo should restore the state before the crop.
	undoRes, err := a.Undo()
	if err != nil {
		t.Fatalf("undo failed: %v", err)
	}
	// The undo entry was saved from currentImage (no prior warpedImage),
	// so Undo restores the full 100×80 image into warpedImage.
	if undoRes.Width != 100 || undoRes.Height != 80 {
		t.Fatalf("expected 100×80 after undo, got %d×%d", undoRes.Width, undoRes.Height)
	}
}

func TestNormalCrop_MultipleUndos(t *testing.T) {
	a := newTestApp(200, 200)

	_, _ = a.NormalCrop(NormalCropRequest{X1: 0, Y1: 0, X2: 150, Y2: 150})
	_, _ = a.NormalCrop(NormalCropRequest{X1: 0, Y1: 0, X2: 100, Y2: 100})

	if len(a.undoStack) != 2 {
		t.Fatalf("expected 2 undo entries, got %d", len(a.undoStack))
	}

	// First undo: 150×150
	r1, err := a.Undo()
	if err != nil {
		t.Fatalf("first undo failed: %v", err)
	}
	if r1.Width != 150 || r1.Height != 150 {
		t.Fatalf("expected 150×150 after first undo, got %d×%d", r1.Width, r1.Height)
	}

	// Second undo: 200×200 (original currentImage)
	r2, err := a.Undo()
	if err != nil {
		t.Fatalf("second undo failed: %v", err)
	}
	if r2.Width != 200 || r2.Height != 200 {
		t.Fatalf("expected 200×200 after second undo, got %d×%d", r2.Width, r2.Height)
	}
}

// ---- ResetNormal ----

func TestResetNormal_NoImage(t *testing.T) {
	a := NewApp()
	_, err := a.ResetNormal()
	if err == nil {
		t.Fatal("expected error when no image is loaded")
	}
}

func TestResetNormal_ClearsWarpedImage(t *testing.T) {
	a := newTestApp(100, 80)
	_, _ = a.NormalCrop(NormalCropRequest{X1: 0, Y1: 0, X2: 50, Y2: 50})
	if a.warpedImage == nil {
		t.Fatal("warpedImage should be set before reset")
	}
	_, err := a.ResetNormal()
	if err != nil {
		t.Fatalf("ResetNormal failed: %v", err)
	}
	if a.warpedImage != nil {
		t.Fatal("warpedImage should be nil after ResetNormal")
	}
}

func TestResetNormal_Message(t *testing.T) {
	a := newTestApp(100, 80)
	res, err := a.ResetNormal()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Message != "Normal crop reset" {
		t.Fatalf("unexpected message: %q", res.Message)
	}
}

func TestResetNormal_ReturnsDimsOfCurrentImage(t *testing.T) {
	a := newTestApp(100, 80)
	// Apply a crop so that warpedImage is smaller than currentImage.
	_, _ = a.NormalCrop(NormalCropRequest{X1: 0, Y1: 0, X2: 50, Y2: 40})
	res, err := a.ResetNormal()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// After reset, workingImage() falls back to currentImage which is 100×80.
	if res.Width != 100 || res.Height != 80 {
		t.Fatalf("expected 100×80, got %d×%d", res.Width, res.Height)
	}
}

func TestResetNormal_PreviewIsDataURI(t *testing.T) {
	a := newTestApp(100, 80)
	res, err := a.ResetNormal()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(res.Preview, "data:image/") {
		t.Fatalf("preview is not a data URI: %q", res.Preview[:min(len(res.Preview), 50)])
	}
}

func TestResetNormal_AfterCropRestoresOriginalDims(t *testing.T) {
	a := newTestApp(200, 150)
	_, _ = a.NormalCrop(NormalCropRequest{X1: 0, Y1: 0, X2: 80, Y2: 60})
	res, err := a.ResetNormal()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Width != 200 || res.Height != 150 {
		t.Fatalf("expected 200×150 after reset, got %d×%d", res.Width, res.Height)
	}
}

