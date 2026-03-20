package main

import (
	"image"
	"strings"
	"testing"
)

// newLoadedTestApp is like newTestApp but also sets imageLoaded=true so that
// methods that guard on that field (DetectCorners, ClickCorner) work correctly.
func newLoadedTestApp(w, h int) *App {
	a := newTestApp(w, h)
	a.imageLoaded = true
	return a
}

// clickFourCorners is a helper that sends 4 custom corner clicks forming a
// rectangle inside the image. It returns the final ClickCornerResult.
func clickFourCorners(t *testing.T, a *App) *ClickCornerResult {
	t.Helper()
	coords := []ClickCornerRequest{
		{X: 10, Y: 10, Custom: true},
		{X: 110, Y: 10, Custom: true},
		{X: 10, Y: 90, Custom: true},
		{X: 110, Y: 90, Custom: true},
	}
	var res *ClickCornerResult
	for i, c := range coords {
		var err error
		res, err = a.ClickCorner(c)
		if err != nil {
			t.Fatalf("click %d: unexpected error: %v", i+1, err)
		}
	}
	return res
}

// ---- ClickCorner ----

func TestClickCorner_NoImage(t *testing.T) {
	a := NewApp()
	_, err := a.ClickCorner(ClickCornerRequest{X: 10, Y: 10, Custom: true})
	if err == nil {
		t.Fatal("expected error when no image loaded")
	}
}

func TestClickCorner_FirstThreeReturnNoPreview(t *testing.T) {
	a := newLoadedTestApp(200, 200)
	for i := 1; i <= 3; i++ {
		res, err := a.ClickCorner(ClickCornerRequest{X: i * 20, Y: i * 20, Custom: true})
		if err != nil {
			t.Fatalf("click %d: unexpected error: %v", i, err)
		}
		if res.Preview != "" {
			t.Fatalf("click %d: expected no preview, got non-empty preview", i)
		}
		if res.Done {
			t.Fatalf("click %d: expected Done=false", i)
		}
	}
}

func TestClickCorner_CountIncrements(t *testing.T) {
	a := newLoadedTestApp(200, 200)
	for i := 1; i <= 3; i++ {
		res, _ := a.ClickCorner(ClickCornerRequest{X: i * 20, Y: i * 20, Custom: true})
		if res.Count != i {
			t.Fatalf("expected Count=%d, got %d", i, res.Count)
		}
	}
}

func TestClickCorner_FirstThreeReturnSnappedCoords(t *testing.T) {
	a := newLoadedTestApp(200, 200)
	res, err := a.ClickCorner(ClickCornerRequest{X: 42, Y: 17, Custom: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.SnappedX != 42 || res.SnappedY != 17 {
		t.Fatalf("expected snapped (42,17), got (%d,%d)", res.SnappedX, res.SnappedY)
	}
}

func TestClickCorner_SnapsToNearestDetectedCorner(t *testing.T) {
	a := newLoadedTestApp(200, 200)
	a.detectedCorners = []image.Point{{50, 50}, {150, 150}}
	// Click near (52,48) — closest detected corner is (50,50).
	res, err := a.ClickCorner(ClickCornerRequest{X: 52, Y: 48, Custom: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.SnappedX != 50 || res.SnappedY != 50 {
		t.Fatalf("expected snap to (50,50), got (%d,%d)", res.SnappedX, res.SnappedY)
	}
}

func TestClickCorner_CustomIgnoresDetectedCorners(t *testing.T) {
	a := newLoadedTestApp(200, 200)
	a.detectedCorners = []image.Point{{50, 50}}
	// Custom=true: raw coordinate must be used even though (50,50) is nearby.
	res, err := a.ClickCorner(ClickCornerRequest{X: 10, Y: 10, Custom: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.SnappedX != 10 || res.SnappedY != 10 {
		t.Fatalf("expected (10,10) with custom mode, got (%d,%d)", res.SnappedX, res.SnappedY)
	}
}

func TestClickCorner_FourthClickDoneIsTrue(t *testing.T) {
	a := newLoadedTestApp(200, 200)
	res := clickFourCorners(t, a)
	if !res.Done {
		t.Fatal("expected Done=true after 4th click")
	}
}

func TestClickCorner_FourthClickReturnsPreview(t *testing.T) {
	a := newLoadedTestApp(200, 200)
	res := clickFourCorners(t, a)
	if !strings.HasPrefix(res.Preview, "data:image/") {
		t.Fatalf("expected data URI preview after 4th click, got: %q", res.Preview[:min(len(res.Preview), 50)])
	}
}

func TestClickCorner_FourthClickSetsWarpedImage(t *testing.T) {
	a := newLoadedTestApp(200, 200)
	clickFourCorners(t, a)
	if a.warpedImage == nil {
		t.Fatal("warpedImage should be set after 4th click")
	}
}

func TestClickCorner_FourthClickSavesUndo(t *testing.T) {
	a := newLoadedTestApp(200, 200)
	before := len(a.undoStack)
	clickFourCorners(t, a)
	if len(a.undoStack) != before+1 {
		t.Fatalf("expected undo stack +1 after warp, got %d→%d", before, len(a.undoStack))
	}
}

func TestClickCorner_FourthClickClearsSelectedCorners(t *testing.T) {
	a := newLoadedTestApp(200, 200)
	clickFourCorners(t, a)
	if len(a.selectedCorners) != 0 {
		t.Fatalf("selectedCorners should be cleared after warp, got %d", len(a.selectedCorners))
	}
}

func TestClickCorner_FourthClickReturnsNonZeroDims(t *testing.T) {
	a := newLoadedTestApp(200, 200)
	res := clickFourCorners(t, a)
	if res.Width <= 0 || res.Height <= 0 {
		t.Fatalf("expected positive Width/Height after warp, got %d×%d", res.Width, res.Height)
	}
}

// ---- ResetCorners ----

func TestResetCorners_ClearsSelectedCorners(t *testing.T) {
	a := newLoadedTestApp(200, 200)
	a.selectedCorners = []image.Point{{10, 10}, {50, 50}}
	a.ResetCorners()
	if len(a.selectedCorners) != 0 {
		t.Fatal("selectedCorners should be nil after ResetCorners")
	}
}

func TestResetCorners_ClearsWarpedImage(t *testing.T) {
	a := newLoadedTestApp(200, 200)
	a.warpedImage = cloneImage(a.currentImage)
	a.ResetCorners()
	if a.warpedImage != nil {
		t.Fatal("warpedImage should be nil after ResetCorners")
	}
}

func TestResetCorners_PreservesDetectedCorners(t *testing.T) {
	a := newLoadedTestApp(200, 200)
	a.detectedCorners = []image.Point{{10, 10}, {50, 50}, {80, 80}}
	a.ResetCorners()
	if len(a.detectedCorners) != 3 {
		t.Fatalf("detectedCorners should be preserved, got %d", len(a.detectedCorners))
	}
}

func TestResetCorners_ReturnsCurrentImageDims(t *testing.T) {
	a := newLoadedTestApp(200, 150)
	res, err := a.ResetCorners()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Width != 200 || res.Height != 150 {
		t.Fatalf("expected 200×150, got %d×%d", res.Width, res.Height)
	}
}

func TestResetCorners_PreviewIsDataURI(t *testing.T) {
	a := newLoadedTestApp(100, 80)
	res, err := a.ResetCorners()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(res.Preview, "data:image/") {
		t.Fatal("preview is not a data URI")
	}
}

func TestResetCorners_ReturnsDetectedCornersInResult(t *testing.T) {
	a := newLoadedTestApp(100, 100)
	a.detectedCorners = []image.Point{{20, 20}, {80, 80}}
	res, _ := a.ResetCorners()
	if len(res.Corners) != 2 {
		t.Fatalf("expected 2 corners in result, got %d", len(res.Corners))
	}
}

func TestResizeImage_NoImage(t *testing.T) {
	a := NewApp()
	_, err := a.ResizeImage(ResizeRequest{Width: 100, Height: 100})
	if err == nil {
		t.Fatal("expected error when no image loaded")
	}
}

func TestResizeImage_ResizesCurrentImage(t *testing.T) {
	a := newLoadedTestApp(120, 80)
	res, err := a.ResizeImage(ResizeRequest{Width: 60, Height: 40})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Width != 60 || res.Height != 40 {
		t.Fatalf("unexpected dimensions %dx%d", res.Width, res.Height)
	}
	if a.warpedImage == nil {
		t.Fatal("warpedImage should be set after ResizeImage")
	}
	if a.warpedImage.Bounds().Dx() != 60 || a.warpedImage.Bounds().Dy() != 40 {
		t.Fatalf("warpedImage dimensions mismatch %dx%d", a.warpedImage.Bounds().Dx(), a.warpedImage.Bounds().Dy())
	}
}

// ---- RestoreCornerOverlay ----

func TestRestoreCornerOverlay_NoCachedCorners(t *testing.T) {
	a := newLoadedTestApp(100, 100)
	_, err := a.RestoreCornerOverlay(RestoreCornerOverlayRequest{DotRadius: 5})
	if err == nil {
		t.Fatal("expected error when no cached corners")
	}
}

func TestRestoreCornerOverlay_ReturnsCachedCorners(t *testing.T) {
	a := newLoadedTestApp(100, 100)
	a.detectedCorners = []image.Point{{10, 10}, {90, 10}, {90, 90}, {10, 90}}
	res, err := a.RestoreCornerOverlay(RestoreCornerOverlayRequest{DotRadius: 5})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Corners) != 4 {
		t.Fatalf("expected 4 corners, got %d", len(res.Corners))
	}
}

func TestRestoreCornerOverlay_PreviewIsDataURI(t *testing.T) {
	a := newLoadedTestApp(100, 100)
	a.detectedCorners = []image.Point{{10, 10}}
	res, err := a.RestoreCornerOverlay(RestoreCornerOverlayRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(res.Preview, "data:image/") {
		t.Fatal("preview is not a data URI")
	}
}

func TestRestoreCornerOverlay_MessageMentionsCount(t *testing.T) {
	a := newLoadedTestApp(100, 100)
	a.detectedCorners = []image.Point{{10, 10}, {90, 90}}
	res, _ := a.RestoreCornerOverlay(RestoreCornerOverlayRequest{})
	if !strings.Contains(res.Message, "2") {
		t.Fatalf("message should mention corner count 2, got: %q", res.Message)
	}
}

func TestRestoreCornerOverlay_ReturnsDims(t *testing.T) {
	a := newLoadedTestApp(120, 90)
	a.detectedCorners = []image.Point{{10, 10}}
	res, _ := a.RestoreCornerOverlay(RestoreCornerOverlayRequest{})
	if res.Width != 120 || res.Height != 90 {
		t.Fatalf("expected 120×90, got %d×%d", res.Width, res.Height)
	}
}

// ---- SkipCrop ----

func TestSkipCrop_NoImage(t *testing.T) {
	a := NewApp()
	_, err := a.SkipCrop()
	if err == nil {
		t.Fatal("expected error when no image loaded")
	}
}

func TestSkipCrop_SetsWarpedImage(t *testing.T) {
	a := newTestApp(100, 80)
	_, err := a.SkipCrop()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.warpedImage == nil {
		t.Fatal("warpedImage should be set after SkipCrop")
	}
}

func TestSkipCrop_WarpedImageIsCloneNotSamePointer(t *testing.T) {
	a := newTestApp(100, 80)
	a.SkipCrop()
	if a.warpedImage == a.currentImage {
		t.Fatal("warpedImage should be a clone, not the same pointer as currentImage")
	}
}

func TestSkipCrop_ReturnsDims(t *testing.T) {
	a := newTestApp(100, 80)
	res, err := a.SkipCrop()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Width != 100 || res.Height != 80 {
		t.Fatalf("expected 100×80, got %d×%d", res.Width, res.Height)
	}
}

func TestSkipCrop_ClearsSelectedCorners(t *testing.T) {
	a := newTestApp(100, 100)
	a.selectedCorners = []image.Point{{10, 10}, {50, 50}}
	a.SkipCrop()
	if len(a.selectedCorners) != 0 {
		t.Fatal("selectedCorners should be cleared after SkipCrop")
	}
}

func TestSkipCrop_Message(t *testing.T) {
	a := newTestApp(100, 80)
	res, _ := a.SkipCrop()
	if res.Message != "Crop skipped — image ready to save" {
		t.Fatalf("unexpected message: %q", res.Message)
	}
}
