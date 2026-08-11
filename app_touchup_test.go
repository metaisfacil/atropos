package main

import (
	"context"
	"image"
	"testing"
)

func solidTouchupTestImage(value uint8) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, 8, 6))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i] = value
		img.Pix[i+1] = value
		img.Pix[i+2] = value
		img.Pix[i+3] = 255
	}
	return img
}

func registerTouchupTestOperation(a *App) uint64 {
	a.touchupMu.Lock()
	defer a.touchupMu.Unlock()
	a.touchupGen++
	a.touchupCancel = func() {}
	return a.touchupGen
}

func TestTouchupUndoTouchupUndoKeepsHistoryConsistent(t *testing.T) {
	a := NewApp()
	base := solidTouchupTestImage(10)
	a.currentImage = cloneImage(base)
	a.warpedImage = cloneImage(base)

	firstSource := a.workingImage()
	firstGeneration := registerTouchupTestOperation(a)
	if committed, _ := a.commitTouchupResult(context.Background(), firstGeneration, firstSource, solidTouchupTestImage(20)); !committed {
		t.Fatal("first touch-up did not commit")
	}
	if len(a.undoStack) != 1 {
		t.Fatalf("first touch-up undo depth = %d, want 1", len(a.undoStack))
	}
	if _, err := a.Undo(); err != nil {
		t.Fatal(err)
	}
	if got := a.workingImage().NRGBAAt(0, 0).R; got != 10 {
		t.Fatalf("first undo restored pixel %d, want 10", got)
	}

	secondSource := a.workingImage()
	secondGeneration := registerTouchupTestOperation(a)
	if committed, _ := a.commitTouchupResult(context.Background(), secondGeneration, secondSource, solidTouchupTestImage(30)); !committed {
		t.Fatal("second touch-up did not commit")
	}
	if len(a.undoStack) != 1 {
		t.Fatalf("second touch-up undo depth = %d, want 1", len(a.undoStack))
	}
	if _, err := a.Undo(); err != nil {
		t.Fatal(err)
	}
	if got := a.workingImage().NRGBAAt(0, 0).R; got != 10 {
		t.Fatalf("second undo restored pixel %d, want 10", got)
	}
	if len(a.undoStack) != 0 {
		t.Fatalf("final undo depth = %d, want 0", len(a.undoStack))
	}
}

func TestTouchupCommitRejectsStaleSourceWithoutChangingUndo(t *testing.T) {
	a := NewApp()
	oldSource := solidTouchupTestImage(10)
	a.currentImage = cloneImage(oldSource)
	a.warpedImage = oldSource
	generation := registerTouchupTestOperation(a)

	newerImage := solidTouchupTestImage(20)
	a.warpedImage = newerImage
	committed, _ := a.commitTouchupResult(context.Background(), generation, oldSource, solidTouchupTestImage(30))
	if committed {
		t.Fatal("touch-up based on a stale source was committed")
	}
	if a.workingImage() != newerImage {
		t.Fatal("stale touch-up replaced the newer working image")
	}
	if len(a.undoStack) != 0 {
		t.Fatalf("stale touch-up changed undo depth to %d", len(a.undoStack))
	}
}

func TestUndoCancelsInFlightTouchupWithoutPoppingHistory(t *testing.T) {
	a := NewApp()
	a.currentImage = solidTouchupTestImage(10)
	a.warpedImage = solidTouchupTestImage(20)
	a.undoStack = append(a.undoStack, undoEntry{image: solidTouchupTestImage(5)})
	registerTouchupTestOperation(a)

	res, err := a.Undo()
	if err != nil {
		t.Fatal(err)
	}
	if res.Message != "Touch-up cancelled" {
		t.Fatalf("message = %q, want touch-up cancellation", res.Message)
	}
	if len(a.undoStack) != 1 {
		t.Fatalf("undo depth = %d, want existing entry preserved", len(a.undoStack))
	}
	if got := a.workingImage().NRGBAAt(0, 0).R; got != 20 {
		t.Fatalf("working image changed to %d while cancelling touch-up", got)
	}
}

func TestBuildStrokeMaskUsesBoundedGlobalCoordinates(t *testing.T) {
	mask, err := buildStrokeMask(
		image.Rect(0, 0, 4000, 3000),
		[]TouchUpPoint{{X: 100, Y: 200}, {X: 120, Y: 210}},
		40,
	)
	if err != nil {
		t.Fatalf("buildStrokeMask: %v", err)
	}

	if got, want := mask.Bounds(), image.Rect(79, 179, 141, 231); got != want {
		t.Fatalf("bounds = %v, want %v", got, want)
	}
	if len(mask.Pix) >= 4000*3000 {
		t.Fatalf("mask allocated %d bytes; expected bounded allocation", len(mask.Pix))
	}
	if mask.AlphaAt(110, 205).A != 255 {
		t.Fatal("stroke center was not fully covered")
	}
	if mask.AlphaAt(0, 0).A != 0 {
		t.Fatal("pixel outside bounded mask was covered")
	}
}

func TestBuildStrokeMaskSinglePointAndClipping(t *testing.T) {
	mask, err := buildStrokeMask(
		image.Rect(0, 0, 100, 80),
		[]TouchUpPoint{{X: 1, Y: 1}},
		20,
	)
	if err != nil {
		t.Fatalf("buildStrokeMask: %v", err)
	}

	if mask.Bounds().Min != (image.Point{}) {
		t.Fatalf("mask was not clipped to source bounds: %v", mask.Bounds())
	}
	if mask.AlphaAt(1, 1).A != 255 {
		t.Fatal("single click did not produce a round brush mark")
	}
}

func TestBuildStrokeMaskRejectsInvalidInput(t *testing.T) {
	if _, err := buildStrokeMask(image.Rect(0, 0, 10, 10), nil, 10); err == nil {
		t.Fatal("empty stroke was accepted")
	}
	if _, err := buildStrokeMask(image.Rect(0, 0, 10, 10), []TouchUpPoint{{X: 5, Y: 5}}, 0); err == nil {
		t.Fatal("zero brush size was accepted")
	}
	if _, err := buildStrokeMask(image.Rect(0, 0, 10, 10), []TouchUpPoint{{X: 50, Y: 50}}, 5); err == nil {
		t.Fatal("off-image stroke was accepted")
	}
}

func BenchmarkBuildStrokeMask(b *testing.B) {
	points := make([]TouchUpPoint, 0, 168)
	for x := 500.0; x <= 1000; x += 3 {
		points = append(points, TouchUpPoint{X: x, Y: 750 + float64(int(x)%31)})
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := buildStrokeMask(image.Rect(0, 0, 6000, 4000), points, 40); err != nil {
			b.Fatal(err)
		}
	}
}
