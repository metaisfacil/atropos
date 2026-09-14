package main

import (
	"bytes"
	"context"
	"errors"
	"image"
	"testing"
)

func TestCornerDetectionOwnership(t *testing.T) {
	a := newTestApp(100, 80)
	first, firstGen, finishFirst := a.beginCornerDetection()
	second, secondGen, finishSecond := a.beginCornerDetection()
	defer finishSecond()
	finishFirst()
	if !errors.Is(first.Err(), context.Canceled) {
		t.Fatal("new detection did not cancel previous one")
	}
	if _, err := a.finishCornerDetection(first, firstGen, a.currentImage, []image.Point{{1, 1}}); !errors.Is(err, context.Canceled) {
		t.Fatal("stale detector published")
	}
	if _, err := a.finishCornerDetection(second, secondGen, a.currentImage, []image.Point{{2, 2}}); err != nil {
		t.Fatal(err)
	}
	a.CancelCornerDetect()
	if !errors.Is(second.Err(), context.Canceled) {
		t.Fatal("old cleanup removed newest cancellation handle")
	}
	if _, err := a.finishCornerDetection(second, secondGen, a.currentImage, nil); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled detector published")
	}
	if len(a.detectedCorners) != 1 || a.detectedCorners[0] != image.Pt(2, 2) {
		t.Fatal("stale detector changed corners")
	}
}

func TestCompositorLoadUsesCompleteReset(t *testing.T) {
	a := newTestApp(100, 80)
	a.compositorResult = solidTouchupTestImage(30)
	a.warpedImage, a.levelsBaseImage = a.currentImage, a.currentImage
	a.descreenBaseImage, a.descreenResultImage = a.currentImage, a.currentImage
	a.discBaseImage, a.discWorkingCrop, a.discNoMaskPreview = a.currentImage, a.currentImage, "/old"
	a.undoStack, a.redoStack = []undoEntry{{}}, []undoEntry{{}}
	ctx, _, finish := a.beginCornerDetection()
	defer finish()
	registerTouchupTestOperation(a)
	before, _ := a.imagePreviewURL(a.currentImage)
	res, err := a.CompositorLoadResult(CompositorLoadResultRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if a.descreenBaseImage != nil || a.descreenResultImage != nil || a.levelsBaseImage != nil || a.warpedImage != nil {
		t.Fatal("stale image session survived load")
	}
	if a.discBaseImage != nil || a.discWorkingCrop != nil || a.discNoMaskPreview != "" || len(a.undoStack)+len(a.redoStack) != 0 {
		t.Fatal("stale disc/history survived load")
	}
	if a.touchupCancel != nil || ctx.Err() == nil {
		t.Fatal("load left asynchronous work active")
	}
	if before == res.Preview || a.originalImage == a.currentImage {
		t.Fatal("load reused preview or mutable original")
	}
}

func TestCropUsesCurrentBoundsThroughUndoAndRedo(t *testing.T) {
	a := newTestApp(8, 5)
	_, _ = a.SkipCrop()
	for _, width := range []int{5, 2, 1} {
		res, err := a.Crop(CropRequest{Direction: "left"})
		if err != nil || res.Width != width {
			t.Fatalf("crop got %v, %v; expected width %d", res, err, width)
		}
	}
	_, _ = a.Undo()
	if _, err := a.Crop(CropRequest{Direction: "invalid"}); err == nil || len(a.redoStack) != 1 {
		t.Fatal("invalid crop modified history")
	}
	_, _ = a.Redo()
	before := len(a.undoStack)
	_, _ = a.Crop(CropRequest{Direction: "left"})
	if len(a.undoStack) != before {
		t.Fatal("one-pixel crop created a no-op undo")
	}
}

func TestCropUsesRequestedPixelAmount(t *testing.T) {
	a := newTestApp(20, 12)
	_, _ = a.SkipCrop()

	res, err := a.Crop(CropRequest{Direction: "left", Amount: 7})
	if err != nil {
		t.Fatal(err)
	}
	if res.Width != 13 || res.Height != 12 {
		t.Fatalf("crop dimensions = %dx%d, want 13x12", res.Width, res.Height)
	}
}

func TestFailedFourthCornerPreservesHistoryAndSelection(t *testing.T) {
	a := newLoadedTestApp(100, 80)
	a.selectedCorners = []image.Point{{1, 1}, {2, 1}, {2, 2}}
	want := append([]byte(nil), a.currentImage.Pix...)
	_, err := a.ClickCorner(ClickCornerRequest{X: 1, Y: 2, Custom: true})
	if err == nil {
		t.Fatal("expected tiny crop error")
	}
	if len(a.undoStack) != 0 || len(a.selectedCorners) != 3 || a.warpedImage != nil || !bytes.Equal(want, a.currentImage.Pix) {
		t.Fatal("failed warp changed image/history or kept phantom fourth pick")
	}
}
