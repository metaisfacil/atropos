package patchmatch

import (
	"bytes"
	"context"
	"image"
	"math"
	"testing"
)

func photoTestLevel() *pmLevel {
	img := image.NewNRGBA(image.Rect(0, 0, 48, 24))
	for y := 0; y < 24; y++ {
		for x := 0; x < 48; x++ {
			i := y*img.Stride + x*4
			v := byte(100)
			if x >= 24 {
				v = 70
			}
			img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = v, v, v, 255
		}
	}
	mask := image.NewAlpha(img.Bounds())
	level := preparePMLevel(img, mask, binarySourceMask(mask), 7)
	level.photoEnabled = true
	pmPreparePhotoSourceStats(level)
	level.targetPlanes = packPMPixels(img)
	pmPreparePhotoTargetStats(level, &level.targetPlanes)
	return level
}

func TestPhotoCostDoesNotCreditIdentityTransform(t *testing.T) {
	level := photoTestLevel()
	explained, regularizer := pmPhotoCostAdjustment(level, 10, 12, pmPoint{x: 34, y: 12}, pmIdentityPhotoTransform())
	if explained != 0 || regularizer != 0 {
		t.Fatalf("identity transform cannot explain a color mismatch: explained=%g regularizer=%g", explained, regularizer)
	}
}

func TestPhotoFloatIdentityPreservesFractionalSamples(t *testing.T) {
	for _, value := range []float32{0, 70, 127.25, 254.75, 255} {
		got := pmApplyPhotoRGBFloat(value, 0, pmIdentityPhotoTransform())
		if got != value {
			t.Fatalf("identity changed vote from %g to %g", value, got)
		}
	}
}

func TestPhotoCostCreditsOnlyBoundedCorrection(t *testing.T) {
	level := photoTestLevel()
	q := pmPoint{x: 34, y: 12}
	tr := pmEstimatePhotoTransform(level, &level.targetPlanes, 10, 12, q)
	explained, _ := pmPhotoCostAdjustment(level, 10, 12, q, tr)
	// Constant patches provide an exact oracle: the rendered residual after the
	// bounded bias must remain in the appearance cost.
	raw := float32(30 * 30)
	residual := 100 - pmApplyPhotoRGBFloat(70, 0, tr)
	maxExplained := raw - residual*residual
	if explained <= 0 || explained > maxExplained+0.01 {
		t.Fatalf("credited %g, but bounded transform explains only %g", explained, maxExplained)
	}
}

func TestPhotoWinningCostIndependentOfThreshold(t *testing.T) {
	level := photoTestLevel()
	q := pmPoint{x: 34, y: 12}
	full := pmPatchCost(level, &level.targetPlanes, 10, 12, q, float32(math.Inf(1)))
	for _, limit := range []float32{full + 1, 1000, 1e20} {
		got := pmPatchCost(level, &level.targetPlanes, 10, 12, q, limit)
		if absFloat32(got-full) > 0.001 {
			t.Fatalf("winning candidate changed cost with threshold %g: got %g want %g", limit, got, full)
		}
	}
}

func TestPhotoEarlyExitPreservesCandidateDecisions(t *testing.T) {
	level := photoTestLevel()
	for y := 0; y < level.h; y++ {
		for x := 0; x < level.w; x++ {
			i := y*level.src.Stride + x*4
			for c := 0; c < 3; c++ {
				level.src.Pix[i+c] = byte(pmHash(uint32(x), uint32(y), uint32(c+1)) % 256)
			}
		}
	}
	level.srcPlanes = packPMPixels(level.src)
	level.targetPlanes = packPMPixels(level.src)
	pmPreparePhotoSourceStats(level)
	pmPreparePhotoTargetStats(level, &level.targetPlanes)
	for _, enabled := range []bool{false, true} {
		level.photoEnabled = enabled
		for _, q := range level.sources {
			full := pmPatchCost(level, &level.targetPlanes, 10, 12, q, float32(math.Inf(1)))
			for _, limit := range []float32{full * 0.25, full * 0.9, full + 1, full*2 + 1} {
				got := pmPatchCost(level, &level.targetPlanes, 10, 12, q, limit)
				if (got < limit) != (full < limit) || (got < limit && absFloat32(got-full) > 0.01) {
					t.Fatalf("photo=%v source=%v limit=%g: pruned=%g full=%g", enabled, q, limit, got, full)
				}
			}
		}
	}
}

func TestPhotoFillPreservesFlatColorAndKnownPixels(t *testing.T) {
	src, mask := makeFilledSrc(96, 72)
	for y := 0; y < 72; y++ {
		for x := 0; x < 96; x++ {
			if mask.Pix[y*mask.Stride+x] != 0 {
				i := y*src.Stride + x*4
				src.Pix[i], src.Pix[i+1], src.Pix[i+2] = 5, 20, 240
			}
		}
	}
	before := append([]byte(nil), src.Pix...)
	out, err := Fill(context.Background(), src, mask, 7, 4)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, src.Pix) {
		t.Fatal("fill modified source")
	}
	for y := 0; y < 72; y++ {
		for x := 0; x < 96; x++ {
			i := y*out.Stride + x*4
			if mask.Pix[y*mask.Stride+x] == 0 {
				if !bytes.Equal(out.Pix[i:i+4], before[i:i+4]) {
					t.Fatalf("known pixel changed at (%d,%d)", x, y)
				}
				continue
			}
			for c, want := range []byte{180, 176, 168, 255} {
				// Pyramid/vote quantization may differ by one code value.
				if absInt(int(out.Pix[i+c])-int(want)) > 1 {
					t.Fatalf("flat repair drifted at (%d,%d) channel %d: got %d want %d +/-1", x, y, c, out.Pix[i+c], want)
				}
			}
		}
	}
}

func TestPhotoMomentsRetainLowContrastFarFromOrigin(t *testing.T) {
	const size = 512
	field := pmBuildPhotoIntegral(size, size, func(x, y int) (float32, float32, float32, float32) {
		v := float32(200 + (x+y)%2)
		return 1, v, v, v
	})
	stats := pmPhotoPatchStats(&field, 500, 500, 3)
	// A 7x7 checkerboard contains 24 high and 25 low samples.
	mean := float32(200 + 24.0/49)
	std := float32(math.Sqrt(24.0 * 25 / (49 * 49)))
	if absFloat32(stats.mean[0]-mean) > 0.001 || absFloat32(stats.stdL-std) > 0.001 {
		t.Fatalf("low-contrast moments lost to cancellation: mean=%g std=%g; want %g %g", stats.mean[0], stats.stdL, mean, std)
	}
}

func BenchmarkPatchMatchPhotoFill(b *testing.B) {
	src, mask := makeFilledSrc(176, 132)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := Fill(context.Background(), src, mask, 7, 4); err != nil {
			b.Fatal(err)
		}
	}
}
