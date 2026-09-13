package main

import (
	"bytes"
	"image"
	"testing"
)

func TestUndoTilesRoundTripStridedImage(t *testing.T) {
	parent := image.NewNRGBA(image.Rect(-20, -30, 300, 310))
	for i := range parent.Pix {
		parent.Pix[i] = byte(i*37 + i/73)
	}
	src := parent.SubImage(image.Rect(-7, 9, 270, 280)).(*image.NRGBA)
	snapshot := snapshotUndoImage(src, nil)
	restored := snapshot.restore()
	if restored.Bounds() != src.Bounds() {
		t.Fatal("bounds changed")
	}
	for y := src.Rect.Min.Y; y < src.Rect.Max.Y; y++ {
		o, r := src.PixOffset(src.Rect.Min.X, y), restored.PixOffset(src.Rect.Min.X, y)
		if !bytes.Equal(src.Pix[o:o+src.Rect.Dx()*4], restored.Pix[r:r+src.Rect.Dx()*4]) {
			t.Fatalf("row %d changed", y)
		}
	}
	want := append([]byte(nil), restored.Pix...)
	clear(parent.Pix)
	clear(restored.Pix)
	if !bytes.Equal(snapshot.restore().Pix, want) {
		t.Fatal("history aliases live pixels")
	}
}

func TestUndoTilesShareOnlyUnchangedRegions(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 300, 260))
	first := snapshotUndoImage(src, nil)
	src.Pix[src.PixOffset(140, 140)] = 77
	second := snapshotUndoImage(src, first)
	for i := range first.tiles {
		if (first.tiles[i] != second.tiles[i]) != (i == 4) {
			t.Fatalf("unexpected tile sharing at %d", i)
		}
	}
	if first.restore().NRGBAAt(140, 140).R != 0 || second.restore().NRGBAAt(140, 140).R != 77 {
		t.Fatal("wrong restored pixels")
	}
	resized := image.NewNRGBA(image.Rect(0, 0, 31, 11))
	if got := snapshotUndoImage(resized, second).restore(); got.Bounds() != resized.Bounds() || !bytes.Equal(got.Pix, resized.Pix) {
		t.Fatal("resize round trip failed")
	}
}

func TestUndoHistoryHundredLocalEdits(t *testing.T) {
	a := NewApp()
	a.warpedImage = image.NewNRGBA(image.Rect(0, 0, 1024, 1024))
	for i := 0; i < 120; i++ {
		a.saveUndo()
		a.warpedImage.Pix[0] = byte(i + 1)
	}
	if len(a.undoStack) != 100 {
		t.Fatalf("depth = %d", len(a.undoStack))
	}
	if size := a.undoBytes(); size >= 12<<20 {
		t.Fatalf("local edits used %d bytes; expected under 12 MiB", size)
	}
	for want := 119; want >= 20; want-- {
		if _, err := a.Undo(); err != nil {
			t.Fatal(err)
		}
		if got := int(a.workingImage().Pix[0]); got != want {
			t.Fatalf("got %d, want %d", got, want)
		}
	}
	// A new edit after undo must snapshot restored pixels, not discarded tiles.
	a.saveUndo()
	a.warpedImage.Pix[0] = 255
	if _, err := a.Undo(); err != nil {
		t.Fatal(err)
	}
	if a.workingImage().Pix[0] != 20 {
		t.Fatal("branch restored wrong image")
	}
}

func TestUndoBudgetEvictionAndRelease(t *testing.T) {
	a := NewApp()
	a.undoMemoryLimit = 70000
	a.warpedImage = image.NewNRGBA(image.Rect(0, 0, 128, 128))
	a.saveUndo()
	a.warpedImage.Pix[0] = 1
	a.saveUndo()
	if len(a.undoStack) != 1 || a.undoBytes() > a.undoMemoryLimit {
		t.Fatal("budget not enforced")
	}
	backing := a.undoStack[:cap(a.undoStack)]
	if _, err := a.Undo(); err != nil {
		t.Fatal(err)
	}
	if backing[0].image != nil {
		t.Fatal("popped entry retains pixels")
	}
	if a.workingImage().Pix[0] != 1 {
		t.Fatal("eviction broke latest snapshot")
	}
	a.undoMemoryLimit = 1
	a.saveUndo()
	if len(a.undoStack) != 1 {
		t.Fatal("oversize image must retain one undo")
	}
}

func TestUndoClearsAdjustmentSessionsAndRestoresDiscMetadata(t *testing.T) {
	a := NewApp()
	a.warpedImage = solidTouchupTestImage(20)
	a.rotationAngle, a.postDiscBlack, a.postDiscWhite = 12, 10, 230
	a.saveUndo()
	a.rotationAngle, a.postDiscBlack, a.postDiscWhite = 70, 30, 190
	a.levelsBaseImage = solidTouchupTestImage(99)
	a.descreenBaseImage, a.descreenResultImage = a.warpedImage, a.warpedImage
	result, err := a.Undo()
	if err != nil {
		t.Fatal(err)
	}
	if a.levelsBaseImage != nil || a.descreenBaseImage != nil || a.descreenResultImage != nil || !result.DescreenReset {
		t.Fatal("stale adjustment session after undo")
	}
	if a.rotationAngle != 12 || a.postDiscBlack != 10 || a.postDiscWhite != 230 {
		t.Fatal("disc metadata not restored")
	}
}

func BenchmarkUndoLocalEdit(b *testing.B) {
	a := NewApp()
	a.warpedImage = image.NewNRGBA(image.Rect(0, 0, 4000, 4000))
	a.saveUndo()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		a.warpedImage.Pix[0]++
		a.saveUndo()
	}
}
