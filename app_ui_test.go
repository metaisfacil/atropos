package main

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"atropos/ui"
)

func writeUIIntegrationScanPNG(t *testing.T, name string, w, h int) string {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	bg := color.NRGBA{R: 20, G: 20, B: 20, A: 255}
	fg := color.NRGBA{R: 235, G: 230, B: 220, A: 255}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if x >= 24 && x < w-24 && y >= 20 && y < h-20 {
				img.SetNRGBA(x, y, fg)
			} else {
				img.SetNRGBA(x, y, bg)
			}
		}
	}
	p := filepath.Join(t.TempDir(), name)
	f, err := os.Create(p)
	if err != nil {
		t.Fatalf("create test png: %v", err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatalf("encode test png: %v", err)
	}
	return p
}

func newTestEditorWithBackend(t *testing.T) (*App, *ui.EditorUI) {
	t.Helper()
	t.Setenv("AppData", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	a := NewApp()
	editor := ui.NewEditorUI(a, nil, ui.Config{AppVersion: AppVersion})
	editor.SetSyncJobs(true)
	editor.InitOnStartup()
	return a, editor
}

func TestEditorUILoadResetAndModeSwitch(t *testing.T) {
	a, editor := newTestEditorWithBackend(t)
	scan1 := writeUIIntegrationScanPNG(t, "scan1.png", 240, 180)
	scan2 := writeUIIntegrationScanPNG(t, "scan2.png", 200, 160)

	editor.LoadFile(scan1)
	st := editor.State()
	if !st.ImageLoaded {
		t.Fatal("expected ImageLoaded=true after LoadFile")
	}
	if st.RealImageDims != image.Pt(240, 180) {
		t.Fatalf("RealImageDims = %v, want (240,180)", st.RealImageDims)
	}
	if st.Preview == "" || !st.HasCornerCache {
		t.Fatal("expected Preview and cached cornerEntry after load in corner mode")
	}
	if a.originalImage == nil || a.currentImage == nil {
		t.Fatal("backend images missing after load")
	}

	editor.SetDescreenTool(true)
	editor.SwitchMode("disc")
	st = editor.State()
	if st.Mode != "disc" {
		t.Fatalf("Mode = %q, want disc", st.Mode)
	}
	if st.UseDescreenTool {
		t.Fatal("expected mode switch to clear active tool toggles")
	}
	if st.CornersDetected {
		t.Fatal("expected corner detection overlay to clear when leaving corner mode")
	}

	editor.SwitchMode("corner")
	st = editor.State()
	if st.Mode != "corner" {
		t.Fatalf("Mode = %q, want corner", st.Mode)
	}
	if !st.CornersDetected || !st.HasCornerCache {
		t.Fatal("expected cached corner overlay to restore when switching back to corner mode")
	}

	editor.LoadFile(scan2)
	st = editor.State()
	if st.RealImageDims != image.Pt(200, 160) {
		t.Fatalf("second load dims = %v, want (200,160)", st.RealImageDims)
	}
	if !st.HasCornerCache || st.CornerCacheWidth != 200 {
		t.Fatalf("expected corner cache refreshed for new scan (width 200), got hasCache=%v width=%d", st.HasCornerCache, st.CornerCacheWidth)
	}
}

func TestEditorUISkipCropNormalCropAndUndoRedo(t *testing.T) {
	_, editor := newTestEditorWithBackend(t)
	scan := writeUIIntegrationScanPNG(t, "normal.png", 240, 180)
	editor.SwitchMode("normal")
	editor.LoadFile(scan)

	if editor.State().CanSave {
		t.Fatal("expected CanSave=false before crop or skip crop")
	}
	editor.ApplyNormalCrop(20, 20, 200, 150)
	st := editor.State()
	if st.RealImageDims != image.Pt(180, 130) {
		t.Fatalf("post-crop dims = %v, want (180,130)", st.RealImageDims)
	}
	if !st.CanSave || !st.CanRecrop || !st.UnsavedChanges {
		t.Fatal("expected CanSave, CanRecrop, UnsavedChanges all true after NormalCrop")
	}

	editor.Undo()
	st = editor.State()
	if st.RealImageDims != image.Pt(240, 180) || st.CanRecrop {
		t.Fatalf("after Undo: dims=%v CanRecrop=%v, want (240,180) false", st.RealImageDims, st.CanRecrop)
	}

	editor.Redo()
	st = editor.State()
	if st.RealImageDims != image.Pt(180, 130) || !st.CanRecrop {
		t.Fatalf("after Redo: dims=%v CanRecrop=%v, want (180,130) true", st.RealImageDims, st.CanRecrop)
	}

	editor.SwitchMode("corner")
	editor.SkipCrop()
	st = editor.State()
	if !st.CropSkipped || !st.CanSave {
		t.Fatal("expected CropSkipped=true and CanSave=true after SkipCrop")
	}
}

func TestCalibrationIsolationWithBackend(t *testing.T) {
	a, editor := newTestEditorWithBackend(t)
	scan1 := writeUIIntegrationScanPNG(t, "main.png", 240, 180)
	scan2 := writeUIIntegrationScanPNG(t, "calib.png", 160, 120)
	editor.SwitchMode("normal")
	editor.LoadFile(scan1)
	editor.SkipCrop()

	mainOrig := a.originalImage
	mainPrev := editor.State().Preview

	editor.OpenCalibration()
	editor.CalibrationAddPaths([]string{scan2})
	st := editor.State()
	if st.CalibrationPreview == "" || st.CalibrationDims != image.Pt(160, 120) {
		t.Fatalf("calibration preview missing or wrong dims: url=%q dims=%v", st.CalibrationPreview, st.CalibrationDims)
	}
	if a.originalImage != mainOrig || st.Preview != mainPrev {
		t.Fatal("calibration load mutated main editor state")
	}
	editor.CloseCalibration()
	if editor.State().CalibrationOpen {
		t.Fatal("expected CalibrationOpen=false after CloseCalibration")
	}
}

func TestModeSwitchResetsBackendAndUIStatePreAndPostCrop(t *testing.T) {
	a, editor := newTestEditorWithBackend(t)
	scan := writeUIIntegrationScanPNG(t, "modeswitch.png", 240, 180)
	editor.LoadFile(scan)
	st := editor.State()
	if !st.ImageLoaded || st.RealImageDims != image.Pt(240, 180) {
		t.Fatalf("unexpected load state: loaded=%v dims=%v", st.ImageLoaded, st.RealImageDims)
	}

	// 1. Pre-crop switch from Corner (with selected corner) -> Disc clears backend selection.
	editor.ClickCorner(image.Pt(30, 30), true)
	if len(a.selectedCorners) != 1 || editor.State().CornerCount != 1 {
		t.Fatalf("expected 1 selected corner before switch, got backend=%d ui=%d", len(a.selectedCorners), editor.State().CornerCount)
	}
	editor.SwitchMode("disc")
	if len(a.selectedCorners) != 0 || a.warpedImage != nil || editor.State().RealImageDims != image.Pt(240, 180) {
		t.Fatalf("Corner->Disc did not reset pre-crop state cleanly: sel=%d warped=%v dims=%v",
			len(a.selectedCorners), a.warpedImage != nil, editor.State().RealImageDims)
	}

	// 2. SkipCrop in Disc -> Normal clears warpedImage and cropSkipped.
	editor.SkipCrop()
	if a.warpedImage == nil || !editor.State().CropSkipped {
		t.Fatalf("expected SkipCrop active before switch: warped=%v skip=%v", a.warpedImage != nil, editor.State().CropSkipped)
	}
	editor.SwitchMode("normal")
	if a.warpedImage != nil || a.cropSkipped || editor.State().CropSkipped || editor.State().RealImageDims != image.Pt(240, 180) {
		t.Fatalf("Disc(skip)->Normal left stale state: warped=%v appSkip=%v uiSkip=%v dims=%v",
			a.warpedImage != nil, a.cropSkipped, editor.State().CropSkipped, editor.State().RealImageDims)
	}

	// 3. Post-crop Disc -> Normal resets warpedImage and disc state directly (without visiting Corner).
	editor.SwitchMode("disc")
	editor.ApplyDrawDisc(120, 90, 50)
	st = editor.State()
	if a.warpedImage == nil || a.discRadius != 50 || st.RealImageDims != image.Pt(100, 100) || !st.DiscActive {
		t.Fatalf("expected active 100x100 disc crop, got warped=%v rad=%d dims=%v active=%v",
			a.warpedImage != nil, a.discRadius, st.RealImageDims, st.DiscActive)
	}
	editor.SwitchMode("normal")
	st = editor.State()
	if a.warpedImage != nil || a.discRadius != 0 || a.discBaseImage != nil || st.DiscActive || st.RealImageDims != image.Pt(240, 180) {
		t.Fatalf("Disc->Normal did not reset post-crop state: warped=%v rad=%d discBase=%v discActive=%v dims=%v",
			a.warpedImage != nil, a.discRadius, a.discBaseImage != nil, st.DiscActive, st.RealImageDims)
	}

	// 4. Post-crop Normal -> Corner -> Disc must clear warpedImage and never restore Normal's cropped preview.
	editor.ApplyNormalCrop(20, 20, 120, 100)
	if a.warpedImage == nil || editor.State().RealImageDims != image.Pt(100, 80) {
		t.Fatalf("expected 100x80 normal crop, got warped=%v dims=%v", a.warpedImage != nil, editor.State().RealImageDims)
	}
	editor.SwitchMode("corner")
	st = editor.State()
	if a.warpedImage != nil || st.RealImageDims != image.Pt(240, 180) || !st.CornersDetected {
		t.Fatalf("Normal->Corner did not clear warpedImage or restore corners: warped=%v dims=%v corners=%v",
			a.warpedImage != nil, st.RealImageDims, st.CornersDetected)
	}
	editor.SwitchMode("disc")
	if a.warpedImage != nil || editor.State().RealImageDims != image.Pt(240, 180) {
		t.Fatalf("Corner->Disc restored stale post-crop state: warped=%v dims=%v",
			a.warpedImage != nil, editor.State().RealImageDims)
	}

	// 5. Post-crop Line -> Normal clears warpedImage, backend lines, and linesProcessed.
	editor.SwitchMode("line")
	editor.ApplyAddLine(image.Pt(20, 20), image.Pt(210, 20))
	editor.ApplyAddLine(image.Pt(210, 20), image.Pt(210, 150))
	editor.ApplyAddLine(image.Pt(210, 150), image.Pt(20, 150))
	editor.ApplyAddLine(image.Pt(20, 150), image.Pt(20, 20))
	if a.warpedImage == nil || !editor.State().LinesProcessed {
		t.Fatalf("expected line perspective crop complete, got warped=%v processed=%v", a.warpedImage != nil, editor.State().LinesProcessed)
	}
	editor.SwitchMode("normal")
	st = editor.State()
	if a.warpedImage != nil || len(a.lines) != 0 || st.LinesProcessed || st.RealImageDims != image.Pt(240, 180) {
		t.Fatalf("Line->Normal did not reset post-crop state: warped=%v lines=%d processed=%v dims=%v",
			a.warpedImage != nil, len(a.lines), st.LinesProcessed, st.RealImageDims)
	}
}
