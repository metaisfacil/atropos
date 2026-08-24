package main

import (
	"fmt"
	"image"
	"image/color"
	"testing"

	"atropos/internal/raster"
)

func TestLoadImageFromClipboardReplacesDocumentAndResetsPipeline(t *testing.T) {
	a := NewApp()
	a.originalImage = solidSelectionTestImage(5, 5, 10)
	a.currentImage = solidSelectionTestImage(5, 5, 20)
	a.warpedImage = solidSelectionTestImage(5, 5, 30)
	a.levelsBaseImage = solidSelectionTestImage(5, 5, 40)
	a.descreenBaseImage = solidSelectionTestImage(5, 5, 50)
	a.descreenResultImage = solidSelectionTestImage(5, 5, 60)
	a.undoStack = []undoEntry{{}}
	a.loadedFilePath = "old.png"

	pasted := image.NewNRGBA(image.Rect(0, 0, 3, 2))
	pasted.SetNRGBA(1, 1, color.NRGBA{R: 11, G: 22, B: 33, A: 255})
	readCount := 0
	a.clipboardReader = func() (*image.NRGBA, string, error) {
		readCount++
		return pasted, "bmp", nil
	}

	info, err := a.LoadImageFromClipboard()
	if err != nil {
		t.Fatalf("LoadImageFromClipboard returned error: %v", err)
	}
	if readCount != 1 {
		t.Fatalf("clipboard reader called %d times, want 1", readCount)
	}
	if info.Width != 3 || info.Height != 2 || info.Format != "BMP" || info.Preview == "" {
		t.Fatalf("unexpected clipboard image info: %+v", info)
	}
	if a.originalImage != pasted {
		t.Fatal("clipboard image was not installed as the immutable source")
	}
	if a.currentImage == pasted || a.currentImage == nil {
		t.Fatal("current image must be a distinct working clone")
	}
	if got := a.currentImage.NRGBAAt(1, 1); got != (color.NRGBA{R: 11, G: 22, B: 33, A: 255}) {
		t.Fatalf("working clone pixel = %v", got)
	}
	if !a.imageLoaded || a.loadedFilePath != "" {
		t.Fatalf("clipboard document state not installed: loaded=%v path=%q", a.imageLoaded, a.loadedFilePath)
	}
	if a.warpedImage != nil || a.levelsBaseImage != nil || a.descreenBaseImage != nil || a.descreenResultImage != nil || len(a.undoStack) != 0 {
		t.Fatal("clipboard load did not clear derived image and undo state")
	}
}

func TestLoadImageFromClipboardReadFailurePreservesDocument(t *testing.T) {
	a := NewApp()
	before := solidSelectionTestImage(2, 2, 70)
	a.originalImage = before
	a.currentImage = raster.CloneNRGBA(before)
	a.imageLoaded = true
	a.clipboardReader = func() (*image.NRGBA, string, error) {
		return nil, "", fmt.Errorf("no image")
	}

	if _, err := a.LoadImageFromClipboard(); err == nil {
		t.Fatal("expected clipboard read error")
	}
	if a.originalImage != before || !a.imageLoaded {
		t.Fatal("failed clipboard read replaced the current document")
	}
}
