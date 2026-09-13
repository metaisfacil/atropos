package main

import (
	"bytes"
	"context"
	"image"
	"testing"
)

func TestHistoryPreservesSkipCropPhase(t *testing.T) {
	a := newTestApp(100, 80)
	if _, err := a.SkipCrop(); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AutoContrast(AutoContrastRequest{}); err != nil {
		t.Fatal(err)
	}
	for cycle := 0; cycle < 2; cycle++ {
		for _, step := range []func() (*ProcessResult, error){a.Undo, a.Redo} {
			res, err := step()
			if err != nil {
				t.Fatal(err)
			}
			if !res.CropSkipped || !a.cropSkipped || res.Uncropped {
				t.Fatal("history lost Skip Crop phase")
			}
		}
	}
	if _, err := a.NormalCrop(NormalCropRequest{X2: 50, Y2: 40}); err != nil {
		t.Fatal(err)
	}
	if a.cropSkipped {
		t.Fatal("explicit crop retained Skip Crop phase")
	}
	res, err := a.Undo()
	if err != nil || !res.CropSkipped {
		t.Fatal("undo did not restore skipped phase before geometric crop")
	}
	res, err = a.Redo()
	if err != nil || res.CropSkipped {
		t.Fatal("redo did not restore geometric crop phase")
	}
	if _, err := a.ResetNormal(); err != nil {
		t.Fatal(err)
	}
	if a.cropSkipped {
		t.Fatal("reset retained skipped phase")
	}
	_, _ = a.SkipCrop()
	a.resetPipelineState()
	if a.cropSkipped {
		t.Fatal("new document retained skipped phase")
	}
}

func TestRedoRoundTripAcrossCropAndResize(t *testing.T) {
	a := newTestApp(100, 80)
	if _, err := a.NormalCrop(NormalCropRequest{X1: 5, Y1: 6, X2: 70, Y2: 60}); err != nil {
		t.Fatal(err)
	}
	crop := snapshotUndoImage(a.workingImage(), nil)
	if _, err := a.ResizeImage(ResizeRequest{Width: 30, Height: 20}); err != nil {
		t.Fatal(err)
	}
	resized := snapshotUndoImage(a.workingImage(), nil)
	for cycle := 0; cycle < 3; cycle++ {
		for i := 0; i < 2; i++ {
			if _, err := a.Undo(); err != nil {
				t.Fatal(err)
			}
		}
		if a.warpedImage != nil || len(a.redoStack) != 2 {
			t.Fatal("undo did not return to pre-crop phase")
		}
		for _, want := range []*undoImage{crop, resized} {
			res, err := a.Redo()
			if err != nil {
				t.Fatal(err)
			}
			if res.Uncropped || !res.Changed || a.warpedImage.Bounds() != want.rect || !bytes.Equal(a.warpedImage.Pix, want.restore().Pix) {
				t.Fatal("redo changed pixels or phase")
			}
		}
		if len(a.undoStack) != 2 || len(a.redoStack) != 0 {
			t.Fatal("history depth drifted")
		}
	}
}

func TestRedoDiscEntryRestoresSourceForSubsequentRenders(t *testing.T) {
	a := newTestApp(200, 200)
	drawTestDisc(t, a)
	if _, err := a.RotateDisc(DiscRotateRequest{Angle: 15}); err != nil {
		t.Fatal(err)
	}
	want := snapshotUndoImage(a.warpedImage, nil)
	if _, err := a.Undo(); err != nil {
		t.Fatal(err)
	}
	if a.discBaseImage != nil {
		t.Fatal("undo kept disc source live")
	}
	res, err := a.Redo()
	if err != nil {
		t.Fatal(err)
	}
	if a.discBaseImage == nil || a.discRadius != 50 || a.rotationAngle != 15 || res.UnmaskedPreview == "" {
		t.Fatal("redo lost disc render state")
	}
	if !bytes.Equal(a.warpedImage.Pix, want.restore().Pix) {
		t.Fatal("redo changed disc pixels")
	}
	if _, err := a.ShiftDisc(ShiftDiscRequest{}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.warpedImage.Pix, want.restore().Pix) {
		t.Fatal("subsequent redraw changed restored disc")
	}
}

func TestRedoInvalidatedByNewEditAndReset(t *testing.T) {
	for name, edit := range map[string]func(*App){
		"commit":       func(a *App) { a.saveUndo() },
		"levels":       func(a *App) { _, _ = a.SetLevels(SetLevelsRequest{Black: 10, White: 230}) },
		"load reset":   func(a *App) { a.resetPipelineState() },
		"normal reset": func(a *App) { _, _ = a.ResetNormal() },
		"corner reset": func(a *App) { _, _ = a.ResetCorners() },
		"disc reset":   func(a *App) { _, _ = a.ResetDisc() },
		"line reset":   func(a *App) { _, _ = a.ClearLines() },
	} {
		t.Run(name, func(t *testing.T) {
			a := newTestApp(100, 80)
			_, _ = a.NormalCrop(NormalCropRequest{X2: 50, Y2: 40})
			_, _ = a.Undo()
			if len(a.redoStack) != 1 {
				t.Fatal("missing redo setup")
			}
			edit(a)
			if len(a.redoStack) != 0 {
				t.Fatal("new edit retained redo")
			}
			res, err := a.Redo()
			if err != nil || res.Changed {
				t.Fatal("empty redo changed document")
			}
		})
	}
}

func TestRedoTouchupDoesNotRerunWorkerAndCancelsPendingWork(t *testing.T) {
	a := newTestApp(8, 6)
	a.warpedImage = solidTouchupTestImage(10)
	gen := registerTouchupTestOperation(a)
	if ok, _ := a.commitTouchupResult(context.Background(), gen, a.warpedImage, solidTouchupTestImage(20)); !ok {
		t.Fatal("commit failed")
	}
	_, _ = a.Undo()
	gen = registerTouchupTestOperation(a)
	source := a.workingImage()
	res, err := a.Redo()
	if err != nil || res.Changed || len(a.redoStack) != 1 {
		t.Fatal("pending touch-up consumed redo")
	}
	if ok, _ := a.commitTouchupResult(context.Background(), gen, source, solidTouchupTestImage(30)); ok {
		t.Fatal("cancelled worker committed")
	}
	res, err = a.Redo()
	if err != nil || !res.Changed || a.workingImage().Pix[0] != 20 {
		t.Fatal("redo did not restore completed touch-up")
	}
}

func TestRedoBudgetCountsBothStacksAndReleasesPoppedEntries(t *testing.T) {
	a := NewApp()
	a.undoMemoryLimit = 140000
	a.warpedImage = image.NewNRGBA(image.Rect(0, 0, 128, 128))
	for i := 0; i < 4; i++ {
		a.saveUndo()
		a.warpedImage.Pix[0]++
	}
	_, _ = a.Undo()
	if len(a.undoStack) != 1 || len(a.redoStack) != 1 || a.undoBytes() > a.undoMemoryLimit {
		t.Fatal("budget not shared across undo and redo")
	}
	backing := a.redoStack[:cap(a.redoStack)]
	_, _ = a.Redo()
	if backing[0].image != nil {
		t.Fatal("redo retained popped image")
	}
	if a.workingImage().Pix[0] != 4 {
		t.Fatal("budget eviction broke redo")
	}
}
