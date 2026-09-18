package patchmatch

import (
	"context"
	"image"
	"testing"
)

func pmTestHash(x, y, salt uint32) uint32 {
	value := x*0x9e3779b1 ^ y*0x85ebca77 ^ salt*0xc2b2ae3d ^ 0x27d4eb2f
	value ^= value >> 16
	value *= 0x7feb352d
	value ^= value >> 15
	value *= 0x846ca68b
	return value ^ (value >> 16)
}

func TestSynthesisRNGKnownVector(t *testing.T) {
	rng := newSynthesisRNG(181, 186, 0, 7)
	want := [4]uint32{0x0c3fbdf9, 0xb2bcce5a, 0x6e684c26, 0x28c9e3d6}
	for i, expected := range want {
		if got := rng.next(); got != expected {
			t.Fatalf("word %d: got %08x, want %08x", i, got, expected)
		}
	}
}

func TestPatchMatchFillSimpleDefect(t *testing.T) {
	const w, h = 64, 48
	src := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := y*src.Stride + x*4
			// A mild print-like gradient with repeating fine variation.
			base := 170 + x/8 + ((x+y)%3 - 1)
			src.Pix[i] = byte(base)
			src.Pix[i+1] = byte(base + 3)
			src.Pix[i+2] = byte(base - 2)
			src.Pix[i+3] = 255
		}
	}
	mask := image.NewAlpha(image.Rect(0, 0, w, h))
	for y := 19; y < 29; y++ {
		for x := 27; x < 37; x++ {
			mask.Pix[y*mask.Stride+x] = 255
			i := y*src.Stride + x*4
			src.Pix[i], src.Pix[i+1], src.Pix[i+2] = 20, 20, 20
		}
	}
	out, err := Fill(context.Background(), src, mask, 7, 4)
	if err != nil {
		t.Fatal(err)
	}
	var mean int
	for y := 21; y < 27; y++ {
		for x := 29; x < 35; x++ {
			mean += int(out.Pix[y*out.Stride+x*4])
		}
	}
	mean /= 36
	if mean < 130 {
		t.Fatalf("filled center remained defect-like: mean=%d", mean)
	}
}

func makeFilledSrc(w, h int) (*image.NRGBA, *image.Alpha) {
	src := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := y*src.Stride + x*4
			src.Pix[i], src.Pix[i+1], src.Pix[i+2], src.Pix[i+3] = 180, 176, 168, 255
		}
	}
	mask := image.NewAlpha(image.Rect(0, 0, w, h))
	minX, maxX := w/2-12, w/2+12
	minY, maxY := h/2-12, h/2+12
	for y := maxInt(0, minY); y < minInt(h, maxY); y++ {
		for x := maxInt(0, minX); x < minInt(w, maxX); x++ {
			mask.Pix[y*mask.Stride+x] = 255
		}
	}
	return src, mask
}

func TestPatchMatchFillROIUsesLocalWorkingSet(t *testing.T) {
	const w, h = 1600, 1200
	src := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := y*src.Stride + x*4
			v := byte(80 + (x+y)%17)
			src.Pix[i], src.Pix[i+1], src.Pix[i+2], src.Pix[i+3] = v, v, v, 255
		}
	}
	mask := image.NewAlpha(src.Bounds())
	dirty := image.Rect(790, 590, 810, 610)
	for y := dirty.Min.Y; y < dirty.Max.Y; y++ {
		for x := dirty.Min.X; x < dirty.Max.X; x++ {
			mask.Pix[y*mask.Stride+x] = 255
		}
	}
	local, work, err := FillROI(context.Background(), src, mask, dirty, 7, 2)
	if err != nil {
		t.Fatal(err)
	}
	if local == nil || work.Empty() {
		t.Fatal("expected a local fill result")
	}
	if !dirty.In(work) {
		t.Fatalf("working ROI %v does not contain dirty bounds %v", work, dirty)
	}
	if work.Dx() >= w || work.Dy() >= h {
		t.Fatalf("small brush unexpectedly used full document ROI %v in %dx%d image", work, w, h)
	}
	if local.Bounds().Dx() != work.Dx() || local.Bounds().Dy() != work.Dy() {
		t.Fatalf("local result %v does not match work bounds %v", local.Bounds(), work)
	}
}

func TestPatchMatchFillBoundsMatchesAutoBounds(t *testing.T) {
	const w, h = 128, 96
	src := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := y*src.Stride + x*4
			v := byte(120 + (x*3+y*5)%23)
			src.Pix[i], src.Pix[i+1], src.Pix[i+2], src.Pix[i+3] = v, v+2, v-2, 255
		}
	}
	mask := image.NewAlpha(src.Bounds())
	dirty := image.Rect(54, 38, 72, 56)
	for y := dirty.Min.Y; y < dirty.Max.Y; y++ {
		for x := dirty.Min.X; x < dirty.Max.X; x++ {
			mask.Pix[y*mask.Stride+x] = 255
		}
	}
	auto, err := Fill(context.Background(), src, mask, 7, 3)
	if err != nil {
		t.Fatal(err)
	}
	hinted, err := FillBounds(context.Background(), src, mask, dirty, 7, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(auto.Pix) != len(hinted.Pix) {
		t.Fatal("result sizes differ")
	}
	for i := range auto.Pix {
		if auto.Pix[i] != hinted.Pix[i] {
			t.Fatalf("dirty-bound hint changed deterministic result at byte %d: %d != %d", i, auto.Pix[i], hinted.Pix[i])
		}
	}
}
