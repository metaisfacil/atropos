package main

import (
	"bytes"
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
	src, mask := makeFilledSrc(800, 600)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_, err := PatchMatchFill(ctx, src, mask, 7, 500)
	elapsed := time.Since(start)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if elapsed > 2*time.Second {
		t.Errorf("cancellation took %v; expected well under 2 s", elapsed)
	}
}

// TestPatchMatchChunkedFill verifies that patchMatchChunkedFill crops to the
// mask bounding box + margin and fills only masked pixels, returning a full-size result.
func TestPatchMatchChunkedFill(t *testing.T) {
	w, h := 2000, 1500
	src := image.NewNRGBA(image.Rect(0, 0, w, h))
	// Fill background with a solid colour
	draw.Draw(src, src.Bounds(), &image.Uniform{color.NRGBA{180, 120, 80, 255}}, image.Point{}, draw.Src)

	// Create a mask in the centre with a small region to fill
	mask := image.NewAlpha(src.Bounds())
	maskCx, maskCy := w/2, h/2
	maskRadius := 30
	for y := maskCy - maskRadius; y < maskCy+maskRadius; y++ {
		for x := maskCx - maskRadius; x < maskCx+maskRadius; x++ {
			if x >= 0 && x < w && y >= 0 && y < h {
				mask.Pix[y*mask.Stride+x] = 255
				// Zero out the source to simulate missing pixels
				src.Pix[(y*w+x)*4+0] = 0
				src.Pix[(y*w+x)*4+1] = 0
				src.Pix[(y*w+x)*4+2] = 0
				src.Pix[(y*w+x)*4+3] = 255
			}
		}
	}

	out, err := patchMatchChunkedFill(context.Background(), src, mask, 7, 4)
	if err != nil {
		t.Fatalf("patchMatchChunkedFill returned error: %v", err)
	}
	if out == nil {
		t.Fatal("patchMatchChunkedFill returned nil")
	}

	// Verify result is full-size
	if out.Bounds() != src.Bounds() {
		t.Errorf("expected result bounds %v, got %v", src.Bounds(), out.Bounds())
	}

	// Verify at least one masked pixel was filled (changed from black)
	changed := false
	for y := maskCy - maskRadius; y < maskCy+maskRadius && !changed; y++ {
		for x := maskCx - maskRadius; x < maskCx+maskRadius; x++ {
			if x >= 0 && x < w && y >= 0 && y < h {
				idx := (y*w + x) * 4
				if out.Pix[idx] != 0 || out.Pix[idx+1] != 0 || out.Pix[idx+2] != 0 {
					changed = true
					break
				}
			}
		}
	}
	if !changed {
		t.Fatalf("expected at least one masked pixel to be filled")
	}

	// Verify that unmasked pixels in the unmasked region remain unchanged
	unchanged := true
	unmaskX, unmaskY := 10, 10
	if unmaskX < maskCx-maskRadius-1 && unmaskY < maskCy-maskRadius-1 {
		origIdx := (unmaskY*w + unmaskX) * 4
		outIdx := (unmaskY*w + unmaskX) * 4
		if out.Pix[outIdx+0] != src.Pix[origIdx+0] ||
			out.Pix[outIdx+1] != src.Pix[origIdx+1] ||
			out.Pix[outIdx+2] != src.Pix[origIdx+2] {
			unchanged = false
		}
	}
	if !unchanged {
		t.Fatalf("expected unmasked pixels to remain unchanged")
	}
}

func TestPatchMatchRegionsSplitDistantStrokes(t *testing.T) {
	bounds := image.Rect(0, 0, 1600, 400)
	mask := image.NewAlpha(bounds)
	mask.SetAlpha(100, 200, color.Alpha{A: 255})
	mask.SetAlpha(1450, 200, color.Alpha{A: 255})

	regions, err := patchMatchRegions(context.Background(), mask, 256, bounds)
	if err != nil {
		t.Fatal(err)
	}
	if len(regions) != 2 {
		t.Fatalf("distant strokes produced %d jobs, want 2: %v", len(regions), regions)
	}
}

func TestPatchMatchRegionsMergeOverlappingContext(t *testing.T) {
	bounds := image.Rect(0, 0, 800, 400)
	mask := image.NewAlpha(bounds)
	mask.SetAlpha(100, 200, color.Alpha{A: 255})
	mask.SetAlpha(500, 200, color.Alpha{A: 255})

	regions, err := patchMatchRegions(context.Background(), mask, 256, bounds)
	if err != nil {
		t.Fatal(err)
	}
	if len(regions) != 1 {
		t.Fatalf("overlapping context windows produced %d jobs, want 1: %v", len(regions), regions)
	}
}

func TestPatchMatchFillCompletesLargeHole(t *testing.T) {
	const w, h = 192, 144
	want := color.NRGBA{R: 176, G: 118, B: 73, A: 255}
	src := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.Draw(src, src.Bounds(), &image.Uniform{C: want}, image.Point{}, draw.Src)
	mask := image.NewAlpha(src.Bounds())

	hole := image.Rect(48, 36, 144, 108)
	for y := hole.Min.Y; y < hole.Max.Y; y++ {
		for x := hole.Min.X; x < hole.Max.X; x++ {
			mask.Pix[y*mask.Stride+x] = 255
			i := y*src.Stride + x*4
			src.Pix[i] = 0
			src.Pix[i+1] = 0
			src.Pix[i+2] = 0
		}
	}

	out, err := PatchMatchFill(context.Background(), src, mask, 7, 3)
	if err != nil {
		t.Fatal(err)
	}
	for y := hole.Min.Y; y < hole.Max.Y; y++ {
		for x := hole.Min.X; x < hole.Max.X; x++ {
			got := out.NRGBAAt(x, y)
			if channelDelta(got.R, want.R) > 2 ||
				channelDelta(got.G, want.G) > 2 ||
				channelDelta(got.B, want.B) > 2 {
				t.Fatalf("large-hole pixel (%d,%d) was not reconstructed: got %v, want %v", x, y, got, want)
			}
		}
	}
}

func TestPatchMatchFillSoftMaskBlendsRGBA(t *testing.T) {
	const w, h = 48, 40
	background := color.NRGBA{R: 200, G: 100, B: 50, A: 255}
	src := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.Draw(src, src.Bounds(), &image.Uniform{C: background}, image.Point{}, draw.Src)
	mask := image.NewAlpha(src.Bounds())

	x, y := w/2, h/2
	i := y*src.Stride + x*4
	src.Pix[i] = 0
	src.Pix[i+1] = 0
	src.Pix[i+2] = 0
	src.Pix[i+3] = 64
	mask.Pix[y*mask.Stride+x] = 128

	out, err := PatchMatchFill(context.Background(), src, mask, 7, 3)
	if err != nil {
		t.Fatal(err)
	}
	got := out.NRGBAAt(x, y)
	if got.R < 95 || got.R > 105 || got.G < 47 || got.G > 53 || got.B < 22 || got.B > 28 {
		t.Fatalf("soft-mask RGB was not blended: got %v", got)
	}
	if got.A < 158 || got.A > 161 {
		t.Fatalf("soft-mask alpha was not blended: got %d, want about 160", got.A)
	}
}

func TestPatchMatchFillDeterministic(t *testing.T) {
	src, mask := makeFilledSrc(120, 96)
	first, err := PatchMatchFill(context.Background(), src, mask, 7, 4)
	if err != nil {
		t.Fatal(err)
	}
	second, err := PatchMatchFill(context.Background(), src, mask, 7, 4)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.Pix, second.Pix) {
		t.Fatal("PatchMatchFill produced different results for identical inputs")
	}
}

func channelDelta(a, b uint8) int {
	if a > b {
		return int(a - b)
	}
	return int(b - a)
}

func BenchmarkPatchMatchFill(b *testing.B) {
	src, mask := makeFilledSrc(512, 384)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := PatchMatchFill(context.Background(), src, mask, 7, 4); err != nil {
			b.Fatal(err)
		}
	}
}
