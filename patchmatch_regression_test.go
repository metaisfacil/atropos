package main

import (
	"context"
	"image"
	"testing"
)

func TestSeedFromSolutionUpsamplesDisplacement(t *testing.T) {
	parentLevel := &pmLevel{w: 8, h: 8}
	parentNNF := make([]pmPoint, 64)
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			parentNNF[y*8+x] = pmPoint{x: int32(x + 2), y: int32(y + 1)}
		}
	}
	seed := &pmSolution{level: parentLevel, nnf: parentNNF}
	child := &pmLevel{w: 16, h: 16}
	for y := 2; y < 14; y++ {
		for x := 2; x < 14; x++ {
			q, ok := pmSeedFromSolution(child, seed, x, y)
			if !ok {
				t.Fatalf("seed failed at %d,%d", x, y)
			}
			if dx, dy := int(q.x)-x, int(q.y)-y; dx != 4 || dy != 2 {
				t.Fatalf("child %d,%d displacement=(%d,%d), want (4,2)", x, y, dx, dy)
			}
		}
	}
}

func TestPreparePMLevelNeverFallsBackToCenterOnlyValidity(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 9, 9))
	targetMask := image.NewAlpha(image.Rect(0, 0, 9, 9))
	sourceMask := image.NewAlpha(image.Rect(0, 0, 9, 9))
	// With a 7x7 patch, this central exclusion intersects every legal source
	// patch although several patch centers themselves remain unmasked.
	sourceMask.Pix[4*sourceMask.Stride+4] = 255
	level := preparePMLevel(src, targetMask, sourceMask, 7)
	if len(level.sources) != 0 {
		t.Fatalf("got %d legal sources; expected none", len(level.sources))
	}
}

func TestPyramidSeparatesTargetCoverageFromSourceExclusion(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	mask := image.NewAlpha(image.Rect(0, 0, 64, 64))
	mask.Pix[2*mask.Stride+2] = 64
	_, targetMasks, sourceMasks := buildPatchPyramid(src, mask, 3)
	if len(targetMasks) < 2 {
		t.Fatal("expected at least two pyramid levels")
	}
	var targetNonZero, sourceNonZero int
	var targetHasPartial bool
	for _, v := range targetMasks[1].Pix {
		if v != 0 {
			targetNonZero++
			if v != 255 {
				targetHasPartial = true
			}
		}
	}
	for _, v := range sourceMasks[1].Pix {
		if v != 0 {
			sourceNonZero++
			if v != 255 {
				t.Fatalf("source exclusion contains non-binary value %d", v)
			}
		}
	}
	if targetNonZero == 0 || !targetHasPartial {
		t.Fatalf("target coverage was not preserved: nonzero=%d partial=%v", targetNonZero, targetHasPartial)
	}
	if sourceNonZero == 0 {
		t.Fatal("source exclusion lost covered fine pixel")
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
	out, err := PatchMatchFill(context.Background(), src, mask, 7, 4)
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
	local, work, err := PatchMatchFillROI(context.Background(), src, mask, dirty, 7, 2)
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
	auto, err := PatchMatchFill(context.Background(), src, mask, 7, 3)
	if err != nil {
		t.Fatal(err)
	}
	hinted, err := PatchMatchFillBounds(context.Background(), src, mask, dirty, 7, 3)
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
