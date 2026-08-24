package main

import (
	"image"
	"image/color"
	"testing"
)

func solidSelectionTestImage(width, height int, value uint8) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: value, G: value, B: value, A: 255})
		}
	}
	return img
}

func TestAutoContrastOnlyChangesSelection(t *testing.T) {
	a := NewApp()
	img := solidSelectionTestImage(4, 2, 100)
	for y := 0; y < 2; y++ {
		img.SetNRGBA(1, y, color.NRGBA{R: 50, G: 50, B: 50, A: 255})
		img.SetNRGBA(2, y, color.NRGBA{R: 200, G: 200, B: 200, A: 255})
	}
	a.currentImage = img

	result, err := a.AutoContrast(AutoContrastRequest{Selection: &AdjustmentSelection{X1: 1, Y1: 0, X2: 3, Y2: 2}})
	if err != nil {
		t.Fatalf("AutoContrast returned error: %v", err)
	}
	if result.Black != 50 || result.White != 200 {
		t.Fatalf("contrast points = (%d,%d), want (50,200)", result.Black, result.White)
	}
	got := a.workingImage()
	if got.NRGBAAt(0, 0).R != 100 || got.NRGBAAt(3, 0).R != 100 {
		t.Fatalf("pixels outside selection changed: left=%d right=%d", got.NRGBAAt(0, 0).R, got.NRGBAAt(3, 0).R)
	}
	if got.NRGBAAt(1, 0).R != 0 || got.NRGBAAt(2, 0).R != 255 {
		t.Fatalf("selected pixels = (%d,%d), want (0,255)", got.NRGBAAt(1, 0).R, got.NRGBAAt(2, 0).R)
	}
}

func TestSetLevelsOnlyChangesSelection(t *testing.T) {
	a := NewApp()
	a.currentImage = solidSelectionTestImage(4, 2, 100)

	_, err := a.SetLevels(SetLevelsRequest{
		Black:     50,
		White:     150,
		Selection: &AdjustmentSelection{X1: 1, Y1: 0, X2: 3, Y2: 2},
	})
	if err != nil {
		t.Fatalf("SetLevels returned error: %v", err)
	}
	got := a.workingImage()
	if got.NRGBAAt(0, 0).R != 100 || got.NRGBAAt(3, 0).R != 100 {
		t.Fatalf("pixels outside selection changed: left=%d right=%d", got.NRGBAAt(0, 0).R, got.NRGBAAt(3, 0).R)
	}
	if got.NRGBAAt(1, 0).R != 127 || got.NRGBAAt(2, 0).R != 127 {
		t.Fatalf("selected pixels = (%d,%d), want (127,127)", got.NRGBAAt(1, 0).R, got.NRGBAAt(2, 0).R)
	}
}

func TestClipAlphaToAdjustmentSelection(t *testing.T) {
	mask := image.NewAlpha(image.Rect(0, 0, 6, 6))
	for y := 0; y < 6; y++ {
		for x := 0; x < 6; x++ {
			mask.SetAlpha(x, y, color.Alpha{A: 255})
		}
	}
	key := adjustmentSelectionKey{Rect: image.Rect(2, 1, 5, 4), Active: true}
	clipped, err := clipAlphaToAdjustmentSelection(mask, key)
	if err != nil {
		t.Fatalf("clipAlphaToAdjustmentSelection returned error: %v", err)
	}
	if clipped.Bounds() != key.Rect {
		t.Fatalf("clipped bounds = %v, want %v", clipped.Bounds(), key.Rect)
	}
}

func TestCopySelectionToClipboardUsesWorkingImageWithoutMutation(t *testing.T) {
	a := NewApp()
	a.currentImage = solidSelectionTestImage(6, 5, 20)
	a.warpedImage = solidSelectionTestImage(4, 3, 200)
	workingBefore := a.warpedImage
	undoBefore := len(a.undoStack)
	var copiedSource *image.NRGBA
	var copiedRect image.Rectangle
	a.clipboardWriter = func(src *image.NRGBA, rect image.Rectangle) error {
		copiedSource = src
		copiedRect = rect
		return nil
	}

	message, err := a.CopySelectionToClipboard(AdjustmentSelection{X1: 3, Y1: 2, X2: 1, Y2: 0})
	if err != nil {
		t.Fatalf("CopySelectionToClipboard returned error: %v", err)
	}
	if message != "Copied 2×2 selection to clipboard" {
		t.Fatalf("unexpected copy message: %q", message)
	}
	if copiedSource != workingBefore {
		t.Fatal("clipboard writer did not receive warped working image")
	}
	if copiedRect != image.Rect(1, 0, 3, 2) {
		t.Fatalf("clipboard rect = %v, want (1,0)-(3,2)", copiedRect)
	}
	if a.warpedImage != workingBefore || len(a.undoStack) != undoBefore {
		t.Fatal("copying a selection mutated working image or undo state")
	}
}

func TestCopySelectionToClipboardRejectsMissingImage(t *testing.T) {
	if _, err := NewApp().CopySelectionToClipboard(AdjustmentSelection{X1: 0, Y1: 0, X2: 1, Y2: 1}); err == nil {
		t.Fatal("expected an error with no image loaded")
	}
}
