package main

import (
	"strings"
	"testing"
)

// drawTestDisc is a convenience helper that calls DrawDisc on a loaded app
// with a sensible centre and radius that fits comfortably within the image.
func drawTestDisc(t *testing.T, a *App) {
	t.Helper()
	_, err := a.DrawDisc(DiscDrawRequest{CenterX: 100, CenterY: 100, Radius: 50})
	if err != nil {
		t.Fatalf("DrawDisc failed: %v", err)
	}
}

// ---- DrawDisc ----

func TestDrawDisc_NoImage(t *testing.T) {
	a := NewApp()
	_, err := a.DrawDisc(DiscDrawRequest{CenterX: 50, CenterY: 50, Radius: 20})
	if err == nil {
		t.Fatal("expected error when no image loaded")
	}
}

func TestDrawDisc_SetsDiscCenter(t *testing.T) {
	a := newTestApp(200, 200)
	_, err := a.DrawDisc(DiscDrawRequest{CenterX: 80, CenterY: 60, Radius: 40})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.discCenter.X != 80 || a.discCenter.Y != 60 {
		t.Fatalf("discCenter not set correctly: %v", a.discCenter)
	}
}

func TestDrawDisc_SetsDiscRadius(t *testing.T) {
	a := newTestApp(200, 200)
	_, err := a.DrawDisc(DiscDrawRequest{CenterX: 50, CenterY: 50, Radius: 35})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.discRadius != 35 {
		t.Fatalf("discRadius should be 35, got %d", a.discRadius)
	}
}

func TestDrawDisc_SnapshotsDiscBaseImage(t *testing.T) {
	a := newTestApp(200, 200)
	_, err := a.DrawDisc(DiscDrawRequest{CenterX: 100, CenterY: 100, Radius: 50})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.discBaseImage == nil {
		t.Fatal("discBaseImage should be set after DrawDisc")
	}
}

func TestDrawDisc_ResetsRotationAngle(t *testing.T) {
	a := newTestApp(200, 200)
	a.rotationAngle = 45.0
	_, err := a.DrawDisc(DiscDrawRequest{CenterX: 100, CenterY: 100, Radius: 50})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.rotationAngle != 0 {
		t.Fatalf("rotationAngle should be reset to 0, got %f", a.rotationAngle)
	}
}

func TestDrawDisc_ResetsPostDiscLevels(t *testing.T) {
	a := newTestApp(200, 200)
	a.postDiscBlack = 50
	a.postDiscWhite = 200
	_, err := a.DrawDisc(DiscDrawRequest{CenterX: 100, CenterY: 100, Radius: 50})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.postDiscBlack != 0 || a.postDiscWhite != 255 {
		t.Fatalf("post-disc levels should reset to (0,255), got (%d,%d)", a.postDiscBlack, a.postDiscWhite)
	}
}

func TestDrawDisc_ResetsLevelsBaseImage(t *testing.T) {
	a := newTestApp(200, 200)
	a.levelsBaseImage = cloneImage(a.currentImage)
	_, err := a.DrawDisc(DiscDrawRequest{CenterX: 100, CenterY: 100, Radius: 50})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.levelsBaseImage != nil {
		t.Fatal("levelsBaseImage should be nil after DrawDisc")
	}
}

func TestDrawDisc_SetsWarpedImage(t *testing.T) {
	a := newTestApp(200, 200)
	_, err := a.DrawDisc(DiscDrawRequest{CenterX: 100, CenterY: 100, Radius: 50})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.warpedImage == nil {
		t.Fatal("warpedImage should be set after DrawDisc")
	}
}

func TestDrawDisc_PreviewIsDataURI(t *testing.T) {
	a := newTestApp(200, 200)
	res, err := a.DrawDisc(DiscDrawRequest{CenterX: 100, CenterY: 100, Radius: 50})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(res.Preview, "data:image/") {
		t.Fatal("preview is not a data URI")
	}
}

func TestDrawDisc_SavesUndo(t *testing.T) {
	a := newTestApp(200, 200)
	before := len(a.undoStack)
	_, err := a.DrawDisc(DiscDrawRequest{CenterX: 100, CenterY: 100, Radius: 50})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(a.undoStack) != before+1 {
		t.Fatalf("expected undo stack +1, got %d→%d", before, len(a.undoStack))
	}
}

// ---- ResetDisc ----

func TestResetDisc_NoImage(t *testing.T) {
	a := NewApp()
	_, err := a.ResetDisc()
	if err == nil {
		t.Fatal("expected error when no image loaded")
	}
}

func TestResetDisc_ClearsDiscRadius(t *testing.T) {
	a := newTestApp(200, 200)
	drawTestDisc(t, a)
	_, err := a.ResetDisc()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.discRadius != 0 {
		t.Fatalf("discRadius should be 0 after ResetDisc, got %d", a.discRadius)
	}
}

func TestResetDisc_ClearsDiscBaseImage(t *testing.T) {
	a := newTestApp(200, 200)
	drawTestDisc(t, a)
	_, err := a.ResetDisc()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.discBaseImage != nil {
		t.Fatal("discBaseImage should be nil after ResetDisc")
	}
}

func TestResetDisc_ClearsRotationAngle(t *testing.T) {
	a := newTestApp(200, 200)
	drawTestDisc(t, a)
	a.rotationAngle = 15.0
	_, err := a.ResetDisc()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.rotationAngle != 0 {
		t.Fatalf("rotationAngle should be 0 after ResetDisc, got %f", a.rotationAngle)
	}
}

func TestResetDisc_ClearsWarpedImage(t *testing.T) {
	a := newTestApp(200, 200)
	drawTestDisc(t, a)
	_, err := a.ResetDisc()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.warpedImage != nil {
		t.Fatal("warpedImage should be nil after ResetDisc")
	}
}

func TestResetDisc_ClearsLevelsBaseImage(t *testing.T) {
	a := newTestApp(200, 200)
	drawTestDisc(t, a)
	a.levelsBaseImage = cloneImage(a.currentImage)
	_, err := a.ResetDisc()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.levelsBaseImage != nil {
		t.Fatal("levelsBaseImage should be nil after ResetDisc")
	}
}

func TestResetDisc_ResetsPostDiscLevels(t *testing.T) {
	a := newTestApp(200, 200)
	drawTestDisc(t, a)
	a.postDiscBlack = 50
	a.postDiscWhite = 200
	_, err := a.ResetDisc()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.postDiscBlack != 0 || a.postDiscWhite != 255 {
		t.Fatalf("post-disc levels should reset to (0,255), got (%d,%d)", a.postDiscBlack, a.postDiscWhite)
	}
}

func TestResetDisc_ReturnsDims(t *testing.T) {
	a := newTestApp(100, 80)
	res, err := a.ResetDisc()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Width != 100 || res.Height != 80 {
		t.Fatalf("expected 100×80, got %d×%d", res.Width, res.Height)
	}
}

func TestResetDisc_Message(t *testing.T) {
	a := newTestApp(200, 200)
	res, _ := a.ResetDisc()
	if res.Message == "" {
		t.Fatal("expected non-empty message")
	}
}

func TestResetDisc_PreviewIsDataURI(t *testing.T) {
	a := newTestApp(200, 200)
	res, err := a.ResetDisc()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(res.Preview, "data:image/") {
		t.Fatal("preview is not a data URI")
	}
}

// ---- RotateDisc ----

func TestRotateDisc_NoDisc(t *testing.T) {
	a := newTestApp(200, 200)
	_, err := a.RotateDisc(DiscRotateRequest{Angle: 10})
	if err == nil {
		t.Fatal("expected error when no disc defined")
	}
}

func TestRotateDisc_AccumulatesAngle(t *testing.T) {
	a := newTestApp(200, 200)
	drawTestDisc(t, a)
	_, err := a.RotateDisc(DiscRotateRequest{Angle: 15.0})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, err = a.RotateDisc(DiscRotateRequest{Angle: 10.0})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.rotationAngle != 25.0 {
		t.Fatalf("expected rotationAngle=25.0, got %f", a.rotationAngle)
	}
}

func TestRotateDisc_ReturnsPreview(t *testing.T) {
	a := newTestApp(200, 200)
	drawTestDisc(t, a)
	res, err := a.RotateDisc(DiscRotateRequest{Angle: 5.0})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(res.Preview, "data:image/") {
		t.Fatal("preview is not a data URI")
	}
}

func TestRotateDisc_UpdatesWarpedImage(t *testing.T) {
	a := newTestApp(200, 200)
	drawTestDisc(t, a)
	before := a.warpedImage
	_, err := a.RotateDisc(DiscRotateRequest{Angle: 5.0})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// redrawDisc always allocates a new image; the pointer must differ.
	if a.warpedImage == before {
		t.Fatal("warpedImage should be a new allocation after RotateDisc")
	}
}

// ---- ShiftDisc ----

func TestShiftDisc_NoDisc(t *testing.T) {
	a := newTestApp(200, 200)
	_, err := a.ShiftDisc(ShiftDiscRequest{DX: 5, DY: 5})
	if err == nil {
		t.Fatal("expected error when no disc defined")
	}
}

func TestShiftDisc_MovesCenter(t *testing.T) {
	a := newTestApp(200, 200)
	_, err := a.DrawDisc(DiscDrawRequest{CenterX: 100, CenterY: 100, Radius: 30})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, err = a.ShiftDisc(ShiftDiscRequest{DX: 10, DY: -5})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.discCenter.X != 110 || a.discCenter.Y != 95 {
		t.Fatalf("expected center (110,95), got %v", a.discCenter)
	}
}

func TestShiftDisc_ReturnsPreview(t *testing.T) {
	a := newTestApp(200, 200)
	drawTestDisc(t, a)
	res, err := a.ShiftDisc(ShiftDiscRequest{DX: 5, DY: 5})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(res.Preview, "data:image/") {
		t.Fatal("preview is not a data URI")
	}
}

func TestShiftDisc_AccumulatesOffset(t *testing.T) {
	a := newTestApp(200, 200)
	_, err := a.DrawDisc(DiscDrawRequest{CenterX: 100, CenterY: 100, Radius: 30})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, err = a.ShiftDisc(ShiftDiscRequest{DX: 5, DY: 3})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, err = a.ShiftDisc(ShiftDiscRequest{DX: -2, DY: 7})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.discCenter.X != 103 || a.discCenter.Y != 110 {
		t.Fatalf("expected center (103,110), got %v", a.discCenter)
	}
}

// ---- SetFeatherSize ----

func TestSetFeatherSize_NegativeClampsToZero(t *testing.T) {
	a := newTestApp(200, 200)
	_, err := a.SetFeatherSize(FeatherSizeRequest{Size: -10})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.featherSize != 0 {
		t.Fatalf("negative feather should clamp to 0, got %d", a.featherSize)
	}
}

func TestSetFeatherSize_UpdatesValue(t *testing.T) {
	a := newTestApp(200, 200)
	_, err := a.SetFeatherSize(FeatherSizeRequest{Size: 25})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.featherSize != 25 {
		t.Fatalf("expected featherSize=25, got %d", a.featherSize)
	}
}

func TestSetFeatherSize_NoDiscDoesNotError(t *testing.T) {
	a := newTestApp(200, 200)
	_, err := a.SetFeatherSize(FeatherSizeRequest{Size: 10})
	if err != nil {
		t.Fatalf("SetFeatherSize should not error with no active disc: %v", err)
	}
}

func TestSetFeatherSize_WithDiscRedrawsDisc(t *testing.T) {
	a := newTestApp(200, 200)
	drawTestDisc(t, a)
	before := a.warpedImage
	_, err := a.SetFeatherSize(FeatherSizeRequest{Size: 5})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.warpedImage == before {
		t.Fatal("warpedImage should be updated after SetFeatherSize with active disc")
	}
}

// ---- GetPixelColor ----

func TestGetPixelColor_NoImage(t *testing.T) {
	a := NewApp()
	_, err := a.GetPixelColor(PixelColorRequest{X: 0, Y: 0})
	if err == nil {
		t.Fatal("expected error when no image loaded")
	}
}

func TestGetPixelColor_SetsBgColor(t *testing.T) {
	// newTestApp fills every pixel with (128,64,32,255).
	a := newTestApp(200, 200)
	_, err := a.GetPixelColor(PixelColorRequest{X: 10, Y: 10})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.bgColor.R != 128 || a.bgColor.G != 64 || a.bgColor.B != 32 {
		t.Fatalf("bgColor not set correctly: %v", a.bgColor)
	}
}

func TestGetPixelColor_ClampsOutOfBoundsCoords(t *testing.T) {
	a := newTestApp(100, 100)
	// Far-out-of-bounds coordinates should be clamped, not error.
	_, err := a.GetPixelColor(PixelColorRequest{X: 9999, Y: 9999})
	if err != nil {
		t.Fatalf("GetPixelColor should clamp coords rather than error: %v", err)
	}
}

func TestGetPixelColor_SamplesFromDiscBaseImageWhenDiscActive(t *testing.T) {
	// discBaseImage is a clone of currentImage (both filled with 128,64,32).
	a := newTestApp(200, 200)
	drawTestDisc(t, a)
	_, err := a.GetPixelColor(PixelColorRequest{X: 10, Y: 10})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.bgColor.R != 128 || a.bgColor.G != 64 || a.bgColor.B != 32 {
		t.Fatalf("bgColor should sample from discBaseImage, got %v", a.bgColor)
	}
}

func TestGetPixelColor_WithDiscRedrawsDisc(t *testing.T) {
	a := newTestApp(200, 200)
	drawTestDisc(t, a)
	before := a.warpedImage
	_, err := a.GetPixelColor(PixelColorRequest{X: 10, Y: 10})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.warpedImage == before {
		t.Fatal("warpedImage should be updated after GetPixelColor with active disc")
	}
}

// ---- StraightEdgeRotate ----

func TestStraightEdgeRotate_NoDisc(t *testing.T) {
	a := newTestApp(200, 200)
	_, err := a.StraightEdgeRotate(StraightEdgeRotateRequest{AngleDeg: 5})
	if err == nil {
		t.Fatal("expected error when no disc defined")
	}
}

func TestStraightEdgeRotate_SubtractsAngleFromRotation(t *testing.T) {
	a := newTestApp(200, 200)
	drawTestDisc(t, a)
	a.rotationAngle = 0
	_, err := a.StraightEdgeRotate(StraightEdgeRotateRequest{AngleDeg: 10.0})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.rotationAngle != -10.0 {
		t.Fatalf("expected rotationAngle=-10.0, got %f", a.rotationAngle)
	}
}

func TestStraightEdgeRotate_AccumulatesWithExistingAngle(t *testing.T) {
	a := newTestApp(200, 200)
	drawTestDisc(t, a)
	a.rotationAngle = 5.0
	_, err := a.StraightEdgeRotate(StraightEdgeRotateRequest{AngleDeg: 3.0})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.rotationAngle != 2.0 {
		t.Fatalf("expected rotationAngle=2.0, got %f", a.rotationAngle)
	}
}

func TestStraightEdgeRotate_SavesDiscRotationUndo(t *testing.T) {
	a := newTestApp(200, 200)
	drawTestDisc(t, a)
	// Clear the undo entry left by DrawDisc for a clean count.
	a.undoStack = nil
	_, err := a.StraightEdgeRotate(StraightEdgeRotateRequest{AngleDeg: 5.0})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(a.undoStack) != 1 {
		t.Fatalf("expected 1 undo entry after StraightEdgeRotate, got %d", len(a.undoStack))
	}
	// The entry must carry a non-nil rotationAngle so Undo can restore it.
	if a.undoStack[0].rotationAngle == nil {
		t.Fatal("undo entry should include rotationAngle for StraightEdgeRotate")
	}
}

func TestStraightEdgeRotate_ReturnsPreview(t *testing.T) {
	a := newTestApp(200, 200)
	drawTestDisc(t, a)
	res, err := a.StraightEdgeRotate(StraightEdgeRotateRequest{AngleDeg: 2.0})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(res.Preview, "data:image/") {
		t.Fatal("preview is not a data URI")
	}
}
