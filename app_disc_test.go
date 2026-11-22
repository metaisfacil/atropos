package main

import (
	"bytes"
	"context"
	"image/color"
	"strings"
	"testing"

	"atropos/internal/imageops"
	"atropos/internal/raster"
)

func TestDiscAutoContrastPreservesStraightenedEdgeCrops(t *testing.T) {
	a := newTestApp(240, 240)
	for y := 0; y < 240; y++ {
		for x := 0; x < 240; x++ {
			a.currentImage.SetNRGBA(x, y, color.NRGBA{R: uint8(x), G: uint8(y), B: 80, A: 255})
		}
	}
	if _, err := a.DrawDisc(DiscDrawRequest{CenterX: 120, CenterY: 120, Radius: 80}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.StraightEdgeRotate(StraightEdgeRotateRequest{AngleDeg: 11}); err != nil {
		t.Fatal(err)
	}
	for _, crop := range []CropRequest{
		{Direction: "top", Amount: 7},
		{Direction: "left", Amount: 13},
		{Direction: "bottom", Amount: 5},
		{Direction: "right", Amount: 9},
	} {
		if _, err := a.Crop(crop); err != nil {
			t.Fatal(err)
		}
	}
	before := raster.CloneNRGBA(a.warpedImage)
	center, angle := a.discCenter, a.rotationAngle
	bp, wp := imageops.AutoContrastPoints(before)
	want := imageops.ApplyLevels(before, bp, wp)
	result, err := a.AutoContrast(AutoContrastRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if got := a.warpedImage.Bounds(); got != before.Bounds() || result.Width != got.Dx() || result.Height != got.Dy() {
		t.Fatalf("auto-contrast changed cropped dimensions: before=%v after=%v result=%dx%d", before.Bounds(), got, result.Width, result.Height)
	}
	if !bytes.Equal(a.warpedImage.Pix, want.Pix) {
		t.Fatal("auto-contrast changed the cropped disc position or pixels")
	}
	if a.discCenter != center || a.rotationAngle != angle {
		t.Fatal("auto-contrast changed disc geometry")
	}
	if _, err := a.Undo(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.warpedImage.Pix, before.Pix) || a.discCropInsets != (discCropInsets{top: 7, left: 13, bottom: 5, right: 9}) {
		t.Fatal("undo did not restore cropped disc and its redraw state")
	}
	if _, err := a.Redo(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.warpedImage.Pix, want.Pix) {
		t.Fatal("redo did not restore auto-contrasted disc")
	}
	if _, err := a.ShiftDisc(ShiftDiscRequest{DX: 1, DY: 0}); err != nil {
		t.Fatal(err)
	}
	if a.warpedImage.Bounds() != before.Bounds() {
		t.Fatal("shifting after auto-contrast lost edge crops")
	}
}

func TestDiscAutoContrastPreservesCropBeforeStraightEdge(t *testing.T) {
	a := newTestApp(240, 240)
	if _, err := a.DrawDisc(DiscDrawRequest{CenterX: 120, CenterY: 120, Radius: 80}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Crop(CropRequest{Direction: "left", Amount: 12}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.StraightEdgeRotate(StraightEdgeRotateRequest{AngleDeg: 9}); err != nil {
		t.Fatal(err)
	}
	before := raster.CloneNRGBA(a.warpedImage)
	bp, wp := imageops.AutoContrastPoints(before)
	want := imageops.ApplyLevels(before, bp, wp)
	if _, err := a.AutoContrast(AutoContrastRequest{}); err != nil {
		t.Fatal(err)
	}
	if a.warpedImage.Bounds() != before.Bounds() {
		t.Fatalf("auto-contrast lost crop after straightening: before=%v after=%v", before.Bounds(), a.warpedImage.Bounds())
	}
	if !bytes.Equal(a.warpedImage.Pix, want.Pix) {
		t.Fatal("auto-contrast moved the cropped disc")
	}
}

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
	a.levelsBaseImage = raster.CloneNRGBA(a.currentImage)
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
	if !strings.HasPrefix(res.Preview, previewAssetPathPrefix) {
		t.Fatal("preview is not an asset URL")
	}
}

func TestDrawDisc_ReturnsCroppedDimensions(t *testing.T) {
	a := newTestApp(200, 200)
	res, err := a.DrawDisc(DiscDrawRequest{CenterX: 100, CenterY: 100, Radius: 50})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantWidth := a.warpedImage.Bounds().Dx()
	wantHeight := a.warpedImage.Bounds().Dy()
	if res.Width != wantWidth || res.Height != wantHeight {
		t.Fatalf("result dimensions = %dx%d, want %dx%d", res.Width, res.Height, wantWidth, wantHeight)
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
	a.levelsBaseImage = raster.CloneNRGBA(a.currentImage)
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
	if !strings.HasPrefix(res.Preview, previewAssetPathPrefix) {
		t.Fatal("preview is not an asset URL")
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
	if !strings.HasPrefix(res.Preview, previewAssetPathPrefix) {
		t.Fatal("preview is not an asset URL")
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
	if !strings.HasPrefix(res.Preview, previewAssetPathPrefix) {
		t.Fatal("preview is not an asset URL")
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

// ---- SetFeatherRadius ----

func TestSetFeatherRadius_NoDiscReturnsError(t *testing.T) {
	a := newTestApp(200, 200)
	_, err := a.SetFeatherRadius(FeatherRadiusRequest{Radius: 60})
	if err == nil {
		t.Fatal("expected error when no disc is defined")
	}
}

func TestSetFeatherRadius_UpdatesRadiusAndCropDimensions(t *testing.T) {
	a := newTestApp(300, 300)
	_, err := a.DrawDisc(DiscDrawRequest{CenterX: 150, CenterY: 150, Radius: 50})
	if err != nil {
		t.Fatalf("DrawDisc failed: %v", err)
	}

	res, err := a.SetFeatherRadius(FeatherRadiusRequest{Radius: 75})
	if err != nil {
		t.Fatalf("SetFeatherRadius failed: %v", err)
	}
	if a.discRadius != 75 || res.DiscRadius != 75 {
		t.Fatalf("radius = app:%d result:%d, want 75", a.discRadius, res.DiscRadius)
	}
	if res.Width != 150 || res.Height != 150 {
		t.Fatalf("crop dimensions = %dx%d, want 150x150", res.Width, res.Height)
	}
}

func TestSetFeatherRadius_ClampsToOne(t *testing.T) {
	a := newTestApp(200, 200)
	drawTestDisc(t, a)
	res, err := a.SetFeatherRadius(FeatherRadiusRequest{Radius: 0})
	if err != nil {
		t.Fatalf("SetFeatherRadius failed: %v", err)
	}
	if a.discRadius != 1 || res.DiscRadius != 1 {
		t.Fatalf("clamped radius = app:%d result:%d, want 1", a.discRadius, res.DiscRadius)
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

func TestSetFeatherSize_DoesNotResizeDiscCrop(t *testing.T) {
	a := newTestApp(200, 200)
	a.featherSize = 0
	res, err := a.DrawDisc(DiscDrawRequest{CenterX: 100, CenterY: 100, Radius: 50})
	if err != nil {
		t.Fatalf("DrawDisc failed: %v", err)
	}
	wantWidth, wantHeight := res.Width, res.Height

	res, err = a.SetFeatherSize(FeatherSizeRequest{Size: 25})
	if err != nil {
		t.Fatalf("SetFeatherSize failed: %v", err)
	}
	if res.Width != wantWidth || res.Height != wantHeight {
		t.Fatalf("feather size changed crop from %dx%d to %dx%d", wantWidth, wantHeight, res.Width, res.Height)
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
	if !strings.HasPrefix(res.Preview, previewAssetPathPrefix) {
		t.Fatal("preview is not an asset URL")
	}
}

func TestStraightEdgeRotate_PreservesTouchUpAndUndo(t *testing.T) {
	a := newTestApp(200, 200)
	if _, err := a.DrawDisc(DiscDrawRequest{CenterX: 100, CenterY: 100, Radius: 60}); err != nil {
		t.Fatal(err)
	}
	origCenter := a.warpedImage.NRGBAAt(60, 95)
	origBase := a.discBaseImage

	// Simulate a completed Touch-up commit: a live disc fills its source, so
	// the edit is a red patch in discBaseImage, between the centre cutout
	// and the outer feather (source (70..130, 120..140) is result
	// (30..90, 80..100), wide enough to survive the rotations below).
	edited := raster.CloneNRGBA(a.discBaseImage)
	red := color.NRGBA{R: 220, G: 20, B: 20, A: 255}
	for y := 120; y < 140; y++ {
		for x := 70; x < 130; x++ {
			edited.SetNRGBA(x, y, red)
		}
	}
	gen := registerTouchupTestOperation(a)
	if committed, _ := a.commitTouchupResult(context.Background(), gen, a.discBaseImage, edited); !committed {
		t.Fatal("expected post-disc touch-up commit to succeed")
	}
	if a.discBaseImage != edited {
		t.Fatal("expected the touch-up to become the disc source")
	}
	if got := a.warpedImage.NRGBAAt(60, 95); got != red {
		t.Fatalf("touch-up not visible in the redraw: got %+v", got)
	}

	// Applying Straight edge rotations must preserve the Touch-up edit.
	if _, err := a.StraightEdgeRotate(StraightEdgeRotateRequest{AngleDeg: 15.0}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.StraightEdgeRotate(StraightEdgeRotateRequest{AngleDeg: 5.0}); err != nil {
		t.Fatal(err)
	}
	if a.rotationAngle != -20.0 {
		t.Fatalf("rotationAngle = %.2f, want -20.0", a.rotationAngle)
	}
	afterStraight := a.warpedImage.NRGBAAt(60, 95)
	if afterStraight.R < 180 || afterStraight.G > 60 || afterStraight.B > 60 {
		t.Fatalf("touch-up center patch was lost after StraightEdgeRotate: got %+v", afterStraight)
	}

	// Undo both Straight edge steps: Touch-up must remain intact.
	if _, err := a.Undo(); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Undo(); err != nil {
		t.Fatal(err)
	}
	if a.rotationAngle != 0 {
		t.Fatalf("rotationAngle after undoing straight edges = %.2f, want 0", a.rotationAngle)
	}
	if got := a.warpedImage.NRGBAAt(60, 95); got != red {
		t.Fatalf("touch-up patch altered after undoing straight edge: got %+v, want %+v", got, red)
	}

	// The edit lives in the source, so the disc can still be shifted and the
	// patch moves with the disc art: result (50,95) is now source (100,135).
	if _, err := a.ShiftDisc(ShiftDiscRequest{DX: 10}); err != nil {
		t.Fatal(err)
	}
	if got := a.warpedImage.NRGBAAt(50, 95); got != red {
		t.Fatalf("touch-up did not move with the shifted disc: got %+v", got)
	}
	if _, err := a.Undo(); err != nil {
		t.Fatal(err)
	}

	// Undo the Touch-up step itself: the original source and pixel return.
	if _, err := a.Undo(); err != nil {
		t.Fatal(err)
	}
	if got := a.warpedImage.NRGBAAt(60, 95); got != origCenter {
		t.Fatalf("pixel after undoing touch-up = %+v, want %+v", got, origCenter)
	}
	if !bytes.Equal(a.discBaseImage.Pix, origBase.Pix) {
		t.Fatal("undoing the touch-up did not restore the disc source")
	}

	// Redo restores the Touch-up in both the source and the render.
	if _, err := a.Redo(); err != nil {
		t.Fatal(err)
	}
	if got := a.warpedImage.NRGBAAt(60, 95); got != red || a.discBaseImage.NRGBAAt(100, 135) != red {
		t.Fatalf("pixel after redoing touch-up = %+v, want %+v", got, red)
	}
}
