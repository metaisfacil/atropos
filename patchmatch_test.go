package main

import (
	"context"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"testing"
	"time"
)

func TestPatchMatchFillRuns(t *testing.T) {
	w, h := 40, 30
	src := image.NewNRGBA(image.Rect(0, 0, w, h))
	// fill background grey
	draw.Draw(src, src.Bounds(), &image.Uniform{color.NRGBA{200, 200, 200, 255}}, image.Point{}, draw.Src)

	// draw a red rectangle that will be used as surrounding texture
	for y := 5; y < 25; y++ {
		for x := 5; x < 35; x++ {
			src.Pix[(y*w+x)*4+0] = 200
			src.Pix[(y*w+x)*4+1] = 60
			src.Pix[(y*w+x)*4+2] = 60
			src.Pix[(y*w+x)*4+3] = 255
		}
	}

	// mask a small hole in the centre
	mask := image.NewAlpha(src.Bounds())
	for y := 12; y < 18; y++ {
		for x := 16; x < 24; x++ {
			mask.Pix[y*mask.Stride+x] = 255
			// zero out src to simulate missing pixels
			src.Pix[(y*w+x)*4+0] = 0
			src.Pix[(y*w+x)*4+1] = 0
			src.Pix[(y*w+x)*4+2] = 0
			src.Pix[(y*w+x)*4+3] = 255
		}
	}

	out, err := PatchMatchFill(context.Background(), src, mask, 7, 4)
	if err != nil {
		t.Fatalf("PatchMatchFill returned error: %v", err)
	}
	if out == nil {
		t.Fatal("PatchMatchFill returned nil")
	}

	// Ensure at least one masked pixel was changed from pure black
	changed := false
	for y := 12; y < 18 && !changed; y++ {
		for x := 16; x < 24; x++ {
			idx := (y*w + x) * 4
			if out.Pix[idx] != 0 || out.Pix[idx+1] != 0 || out.Pix[idx+2] != 0 {
				changed = true
				break
			}
		}
	}
	if !changed {
		t.Fatalf("expected at least one masked pixel to be filled")
	}
}

// makeFilledSrc returns an NRGBA image filled with a solid colour and an Alpha
// mask that marks a central rectangle. The masked pixels are zeroed in src so
// they look "missing". Large enough that initialization loops take non-trivial
// time if the context check is missing.
func makeFilledSrc(w, h int) (src *image.NRGBA, mask *image.Alpha) {
	src = image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.Draw(src, src.Bounds(), &image.Uniform{color.NRGBA{180, 120, 80, 255}}, image.Point{}, draw.Src)
	mask = image.NewAlpha(src.Bounds())
	cx, cy := w/2, h/2
	for y := cy - 20; y < cy+20; y++ {
		for x := cx - 20; x < cx+20; x++ {
			mask.Pix[y*mask.Stride+x] = 255
			src.Pix[(y*w+x)*4+0] = 0
			src.Pix[(y*w+x)*4+1] = 0
			src.Pix[(y*w+x)*4+2] = 0
			src.Pix[(y*w+x)*4+3] = 255
		}
	}
	return
}

// TestPatchMatchFillPreCancelledContext verifies that PatchMatchFill returns
// context.Canceled immediately when the context is already cancelled on entry.
// Before the fix this took ~30 s on a large image because the O(w×h×patch²)
// source-building loops ran to completion before any cancellation check.
func TestPatchMatchFillPreCancelledContext(t *testing.T) {
	src, mask := makeFilledSrc(800, 600)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before calling

	start := time.Now()
	out, err := PatchMatchFill(ctx, src, mask, 7, 5)
	elapsed := time.Since(start)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got err=%v (out nil: %v)", err, out == nil)
	}
	if elapsed > 200*time.Millisecond {
		t.Errorf("PatchMatchFill took %v with a pre-cancelled context; expected near-instant return", elapsed)
	}
}

// TestPatchMatchFillMidOperationCancel verifies that PatchMatchFill stops
// promptly when the context is cancelled while it is running. The context is
// cancelled after a short delay; with many iterations PatchMatch would
// otherwise take several seconds to finish.
func TestPatchMatchFillMidOperationCancel(t *testing.T) {
	src, mask := makeFilledSrc(400, 300)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_, err := PatchMatchFill(ctx, src, mask, 7, 50) // 50 iterations would take many seconds
	elapsed := time.Since(start)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if elapsed > 2*time.Second {
		t.Errorf("cancellation took %v; expected well under 2 s", elapsed)
	}
}
