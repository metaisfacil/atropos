package main

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"math"
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

func TestPatchMatchFillPreservesMicrotextureEnergy(t *testing.T) {
	const w, h = 128, 96
	original := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			// A deterministic 16x16 stochastic tile gives PatchMatch several
			// exact source realizations while retaining photographic high
			// frequencies that incoherent averaging would suppress.
			hash := pmHash(uint32(x&15), uint32(y&15), 91)
			grain := int(hash%37) - 18
			base := 132 + x/16
			i := y*original.Stride + x*4
			original.Pix[i] = byte(clampInt(base+grain, 0, 255))
			original.Pix[i+1] = byte(clampInt(base+grain/2, 0, 255))
			original.Pix[i+2] = byte(clampInt(base-grain/3, 0, 255))
			original.Pix[i+3] = 255
		}
	}

	src := cloneNRGBA(original)
	mask := image.NewAlpha(src.Bounds())
	hole := image.Rect(57, 41, 71, 55)
	for y := hole.Min.Y; y < hole.Max.Y; y++ {
		for x := hole.Min.X; x < hole.Max.X; x++ {
			mask.Pix[y*mask.Stride+x] = 255
			i := y*src.Stride + x*4
			src.Pix[i], src.Pix[i+1], src.Pix[i+2] = 8, 8, 8
		}
	}

	out, err := PatchMatchFill(context.Background(), src, mask, 9, 5)
	if err != nil {
		t.Fatal(err)
	}
	expectedEnergy := patchHighFrequencyEnergy(original, hole)
	actualEnergy := patchHighFrequencyEnergy(out, hole)
	if actualEnergy < expectedEnergy*0.70 {
		t.Fatalf("microtexture energy collapsed: got %.2f, want at least 70%% of %.2f", actualEnergy, expectedEnergy)
	}
	expectedDeviation := patchLumaDeviation(original, hole)
	actualDeviation := patchLumaDeviation(out, hole)
	if actualDeviation < expectedDeviation*0.65 {
		t.Fatalf("local texture variance collapsed: got %.2f, want at least 65%% of %.2f", actualDeviation, expectedDeviation)
	}
}

func TestPatchMatchFillFollowsLowFrequencyGradient(t *testing.T) {
	const w, h = 120, 80
	original := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			value := 54 + x
			i := y*original.Stride + x*4
			original.Pix[i] = byte(value)
			original.Pix[i+1] = byte(value - 5)
			original.Pix[i+2] = byte(value - 10)
			original.Pix[i+3] = 255
		}
	}
	src := cloneNRGBA(original)
	mask := image.NewAlpha(src.Bounds())
	hole := image.Rect(54, 34, 66, 46)
	paintTestHole(src, mask, hole, color.NRGBA{R: 5, G: 5, B: 5, A: 255})

	out, err := PatchMatchFill(context.Background(), src, mask, 9, 5)
	if err != nil {
		t.Fatal(err)
	}
	if mae := patchLumaMAE(out, original, hole); mae > 12 {
		t.Fatalf("low-frequency gradient did not blend through fill: MAE %.2f, want <= 12", mae)
	}
}

func TestPMPatchCostUsesAppearanceGuideInsideUnsupportedHole(t *testing.T) {
	const w, h = 80, 48
	src := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			value := byte(64)
			if x >= w/2 {
				value = 196
			}
			i := y*src.Stride + x*4
			src.Pix[i], src.Pix[i+1], src.Pix[i+2], src.Pix[i+3] = value, value, value, 255
		}
	}
	mask := image.NewAlpha(src.Bounds())
	hole := image.Rect(20, 14, 36, 34)
	paintTestHole(src, mask, hole, color.NRGBA{R: 8, G: 8, B: 8, A: 255})

	level := preparePMLevel(src, mask, 9)
	working := pmHealedWorking(src, mask)
	level.targetPlanes = packPMPixelsInto(working, level.targetPlanes)
	level.targetFeature = buildPMFeaturesPacked(&level.targetPlanes, w, h, level.targetFeature)
	level.targetStats, level.statsScratch = buildPMPatchStats(
		working, level.targetFeature, level.confidence, level.confStride, level.patchSize,
		level.targetStats, level.statsScratch,
	)

	tx, ty := 28, 24 // complete 9x9 target patch is inside the hard mask
	matching := pmPoint{x: 10, y: 24}
	distractor := pmPoint{x: 60, y: 24}
	matchingCost := pmPatchCost(level, &level.targetPlanes, level.targetStats, tx, ty, matching, float32(math.Inf(1)))
	distractorCost := pmPatchCost(level, &level.targetPlanes, level.targetStats, tx, ty, distractor, float32(math.Inf(1)))
	if matchingCost >= distractorCost*0.60 {
		t.Fatalf("appearance guide did not reject incompatible source tone: matching=%.2f distractor=%.2f", matchingCost, distractorCost)
	}
}

func TestPatchMatchFillDoesNotStampDistractorTone(t *testing.T) {
	const w, h = 160, 96
	original := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			base := 58 + x/12 + y/24
			if x >= 105 {
				base += 105 // plentiful but locally incompatible source material
			}
			grain := int(pmHash(uint32(x), uint32(y), 177)%9) - 4
			i := y*original.Stride + x*4
			original.Pix[i] = byte(clampInt(base+grain, 0, 255))
			original.Pix[i+1] = byte(clampInt(base+2+grain, 0, 255))
			original.Pix[i+2] = byte(clampInt(base+5+grain, 0, 255))
			original.Pix[i+3] = 255
		}
	}
	src := cloneNRGBA(original)
	mask := image.NewAlpha(src.Bounds())
	hole := image.Rect(43, 37, 61, 55)
	paintTestHole(src, mask, hole, color.NRGBA{R: 4, G: 4, B: 4, A: 255})

	out, err := PatchMatchFill(context.Background(), src, mask, 9, 5)
	if err != nil {
		t.Fatal(err)
	}
	if meanError := math.Abs(patchMeanLuma(out, hole) - patchMeanLuma(original, hole)); meanError > 9 {
		t.Fatalf("fill retained a visible low-frequency stamp: mean luma error %.2f, want <= 9", meanError)
	}
	if mae := patchLumaMAE(out, original, hole); mae > 14 {
		t.Fatalf("fill did not follow local appearance: MAE %.2f, want <= 14", mae)
	}
}

func TestPMHealedWorkingDoesNotExposePyramidBlocks(t *testing.T) {
	const w, h = 128, 64
	src := image.NewNRGBA(image.Rect(0, 0, w, h))
	mask := image.NewAlpha(src.Bounds())
	hole := image.Rect(24, 12, 104, 52)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			value := byte(48 + x)
			i := y*src.Stride + x*4
			src.Pix[i], src.Pix[i+1], src.Pix[i+2], src.Pix[i+3] = value, value, value, 255
			if image.Pt(x, y).In(hole) {
				mask.Pix[y*mask.Stride+x] = 255
				src.Pix[i], src.Pix[i+1], src.Pix[i+2] = 0, 0, 0
			}
		}
	}

	healed := pmHealedWorking(src, mask)
	maximumStep := 0
	y := h / 2
	for x := hole.Min.X + 1; x < hole.Max.X; x++ {
		step := absInt(int(healed.NRGBAAt(x, y).R) - int(healed.NRGBAAt(x-1, y).R))
		maximumStep = maxInt(maximumStep, step)
	}
	if maximumStep > 5 {
		t.Fatalf("pull/push guide retained a visible pyramid block boundary: maximum step %d", maximumStep)
	}
}

func TestPMHealedWorkingFollowsBoundaryPastNearbyBrightFeature(t *testing.T) {
	const w, h = 120, 84
	original := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			base := 34 + y/3
			i := y*original.Stride + x*4
			original.Pix[i] = byte(base + 3)
			original.Pix[i+1] = byte(base)
			original.Pix[i+2] = byte(base + 7)
			original.Pix[i+3] = 255
		}
	}
	// A bright object immediately above the repair is the failure mode that an
	// isotropic pull/push guide gets wrong: it drags the object into the hole.
	for y := 20; y < 30; y++ {
		for x := 42; x < 78; x++ {
			i := y*original.Stride + x*4
			original.Pix[i], original.Pix[i+1], original.Pix[i+2] = 205, 188, 112
		}
	}

	src := cloneNRGBA(original)
	mask := image.NewAlpha(src.Bounds())
	hole := image.Rect(45, 31, 75, 63)
	paintTestHole(src, mask, hole, color.NRGBA{R: 240, G: 240, B: 240, A: 255})
	healed := pmHealedWorking(src, mask)
	if mae := patchLumaMAE(healed, original, hole); mae > 3 {
		t.Fatalf("directional guide pulled nearby bright feature into dark band: MAE %.2f, want <= 3", mae)
	}
}

func TestPMSmoothedDirectionalGuideReducesAxisBanding(t *testing.T) {
	const w, h = 96, 72
	src := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			grain := int(pmHash(uint32(x), uint32(y), 911)%13) - 6
			i := y*src.Stride + x*4
			src.Pix[i] = byte(92 + grain)
			src.Pix[i+1] = byte(87 + grain)
			src.Pix[i+2] = byte(99 + grain)
			src.Pix[i+3] = 255
		}
	}
	mask := image.NewAlpha(src.Bounds())
	hole := image.Rect(30, 20, 66, 54)
	for y := hole.Min.Y; y < hole.Max.Y; y++ {
		for x := hole.Min.X; x < hole.Max.X; x++ {
			mask.Pix[y*mask.Stride+x] = 255
		}
	}

	guide := pmHealedWorking(src, mask)
	smoothed := pmSmoothedDirectionalGuide(guide, mask)
	before := pmTestAxisBandEnergy(guide, hole.Inset(2))
	after := pmTestAxisBandEnergy(smoothed, hole.Inset(2))
	if after >= before*0.72 {
		t.Fatalf("low-frequency guide retained scanline bands: before=%.3f after=%.3f", before, after)
	}
}

func TestPMSmoothedDirectionalGuidePreservesCrossingEdge(t *testing.T) {
	const w, h = 96, 72
	guide := image.NewNRGBA(image.Rect(0, 0, w, h))
	mask := image.NewAlpha(guide.Bounds())
	hole := image.Rect(26, 18, 70, 56)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			value := color.NRGBA{R: 42, G: 47, B: 53, A: 255}
			if y >= 36 {
				value = color.NRGBA{R: 205, G: 185, B: 116, A: 255}
			}
			// Model the weak row/column variation left by the directional
			// continuation. It should be smoothed without widening the edge.
			if image.Pt(x, y).In(hole) {
				variation := int(pmHash(uint32(x/3), uint32(y/3), 237)%13) - 6
				value.R = byte(clampInt(int(value.R)+variation, 0, 255))
				value.G = byte(clampInt(int(value.G)+variation, 0, 255))
				value.B = byte(clampInt(int(value.B)+variation, 0, 255))
				mask.Pix[y*mask.Stride+x] = 255
			}
			guide.SetNRGBA(x, y, value)
		}
	}

	smoothed := pmSmoothedDirectionalGuide(guide, mask)
	xRange := image.Rect(hole.Min.X+8, 0, hole.Max.X-8, 1)
	meanRow := func(y int) float64 {
		return patchMeanLuma(smoothed, image.Rect(xRange.Min.X, y, xRange.Max.X, y+1))
	}
	dark := meanRow(35)
	bright := meanRow(36)
	if dark > 70 || bright < 155 || bright-dark < 100 {
		t.Fatalf("edge-aware guide widened or erased crossing edge: dark=%.1f bright=%.1f contrast=%.1f", dark, bright, bright-dark)
	}
}

func pmTestAxisBandEnergy(src *image.NRGBA, bounds image.Rectangle) float64 {
	var energy float64
	var samples int
	for y := bounds.Min.Y + 1; y < bounds.Max.Y-1; y++ {
		for x := bounds.Min.X + 1; x < bounds.Max.X-1; x++ {
			center := y*src.Stride + x*4
			left := y*src.Stride + (x-1)*4
			right := y*src.Stride + (x+1)*4
			up := (y-1)*src.Stride + x*4
			down := (y+1)*src.Stride + x*4
			for channel := 0; channel < 3; channel++ {
				horizontal := int(src.Pix[left+channel]) - 2*int(src.Pix[center+channel]) + int(src.Pix[right+channel])
				vertical := int(src.Pix[up+channel]) - 2*int(src.Pix[center+channel]) + int(src.Pix[down+channel])
				energy += float64(absInt(horizontal) + absInt(vertical))
				samples += 2
			}
		}
	}
	return energy / float64(samples)
}

func TestPMNNFCoherenceWeightsSuppressIsolatedHypotheses(t *testing.T) {
	level := &pmLevel{w: 12, h: 8, patchSize: 5, active: image.Rect(0, 0, 12, 8)}
	nnf := make([]pmPoint, level.w*level.h)
	for y := 0; y < level.h; y++ {
		for x := 0; x < level.w; x++ {
			nnf[y*level.w+x] = pmPoint{x: int32(x + 20), y: int32(y + 3)}
		}
	}
	isolated := 4*level.w + 6
	nnf[isolated] = pmPoint{x: 70, y: 1}

	weights := pmNNFCoherenceWeights(level, nnf)
	coherent := weights[2*level.w+2]
	if coherent < 0.99 {
		t.Fatalf("large coherent displacement segment was downweighted: %.3f", coherent)
	}
	if weights[isolated] >= coherent*0.25 {
		t.Fatalf("isolated displacement retained too much vote weight: isolated=%.3f coherent=%.3f", weights[isolated], coherent)
	}
}

func TestPatchMatchFillContinuesStructuredEdge(t *testing.T) {
	const w, h = 128, 88
	original := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.Draw(original, original.Bounds(), &image.Uniform{C: color.NRGBA{R: 48, G: 52, B: 55, A: 255}}, image.Point{}, draw.Src)
	for y := 42; y <= 45; y++ {
		for x := 0; x < w; x++ {
			i := y*original.Stride + x*4
			original.Pix[i], original.Pix[i+1], original.Pix[i+2] = 210, 196, 162
		}
	}
	src := cloneNRGBA(original)
	mask := image.NewAlpha(src.Bounds())
	hole := image.Rect(57, 36, 71, 52)
	paintTestHole(src, mask, hole, color.NRGBA{R: 8, G: 8, B: 8, A: 255})

	out, err := PatchMatchFill(context.Background(), src, mask, 9, 5)
	if err != nil {
		t.Fatal(err)
	}
	stripe := patchMeanLuma(out, image.Rect(hole.Min.X+2, 42, hole.Max.X-2, 46))
	background := patchMeanLuma(out, image.Rect(hole.Min.X+2, 37, hole.Max.X-2, 40))
	if stripe < 160 || stripe-background < 75 {
		t.Fatalf("structured edge was not continued: stripe=%.1f background=%.1f", stripe, background)
	}
}

func TestPatchMatchFillPreservesBroadCrossingEdge(t *testing.T) {
	const w, h = 144, 96
	original := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			value := color.NRGBA{R: 43, G: 47, B: 53, A: 255}
			if y >= 70 {
				value = color.NRGBA{R: 202, G: 181, B: 111, A: 255}
			}
			grain := int(pmHash(uint32(x), uint32(y), 733)%9) - 4
			value.R = byte(clampInt(int(value.R)+grain, 0, 255))
			value.G = byte(clampInt(int(value.G)+grain, 0, 255))
			value.B = byte(clampInt(int(value.B)+grain, 0, 255))
			original.SetNRGBA(x, y, value)
		}
	}
	mask, err := buildStrokeMask(original.Bounds(), []TouchUpPoint{{X: 72, Y: 70}}, 40)
	if err != nil {
		t.Fatal(err)
	}
	out, err := PatchMatchFill(context.Background(), original, mask, 13, 5)
	if err != nil {
		t.Fatal(err)
	}
	dark := patchMeanLuma(out, image.Rect(58, 64, 86, 68))
	gold := patchMeanLuma(out, image.Rect(58, 74, 86, 82))
	if dark > 75 || gold < 145 || gold-dark < 90 {
		t.Fatalf("broad crossing edge was contaminated: dark=%.1f gold=%.1f contrast=%.1f", dark, gold, gold-dark)
	}
	for y := 74; y < 84; y++ {
		for x := 58; x < 86; x++ {
			if mask.AlphaAt(x, y).A > 250 && pixelLuma(out, x, y) < 115 {
				t.Fatalf("dark source edge intruded into continued gold band at (%d,%d): luma=%.1f", x, y, pixelLuma(out, x, y))
			}
		}
	}
}

func paintTestHole(src *image.NRGBA, mask *image.Alpha, bounds image.Rectangle, value color.NRGBA) {
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			mask.Pix[y*mask.Stride+x] = 255
			src.SetNRGBA(x, y, value)
		}
	}
}

func pixelLuma(src *image.NRGBA, x, y int) float64 {
	pixel := src.NRGBAAt(x, y)
	return 0.299*float64(pixel.R) + 0.587*float64(pixel.G) + 0.114*float64(pixel.B)
}

func patchMeanLuma(src *image.NRGBA, bounds image.Rectangle) float64 {
	var sum float64
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			sum += pixelLuma(src, x, y)
		}
	}
	return sum / float64(bounds.Dx()*bounds.Dy())
}

func patchLumaDeviation(src *image.NRGBA, bounds image.Rectangle) float64 {
	mean := patchMeanLuma(src, bounds)
	var sum float64
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			difference := pixelLuma(src, x, y) - mean
			sum += difference * difference
		}
	}
	return math.Sqrt(sum / float64(bounds.Dx()*bounds.Dy()))
}

func patchHighFrequencyEnergy(src *image.NRGBA, bounds image.Rectangle) float64 {
	var sum float64
	var count int
	for y := maxInt(1, bounds.Min.Y); y < minInt(src.Bounds().Dy()-1, bounds.Max.Y); y++ {
		for x := maxInt(1, bounds.Min.X); x < minInt(src.Bounds().Dx()-1, bounds.Max.X); x++ {
			center := pixelLuma(src, x, y)
			neighbours := (pixelLuma(src, x-1, y) + pixelLuma(src, x+1, y) +
				pixelLuma(src, x, y-1) + pixelLuma(src, x, y+1)) * 0.25
			sum += math.Abs(center - neighbours)
			count++
		}
	}
	return sum / float64(count)
}

func patchLumaMAE(actual, expected *image.NRGBA, bounds image.Rectangle) float64 {
	var sum float64
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			sum += math.Abs(pixelLuma(actual, x, y) - pixelLuma(expected, x, y))
		}
	}
	return sum / float64(bounds.Dx()*bounds.Dy())
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
