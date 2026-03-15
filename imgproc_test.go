package main

import (
	"image"
	"image/color"
	"math"
	"strings"
	"testing"
)

// ---- clamp / clampByte ----

func TestClamp_InRange(t *testing.T) {
	if v := clamp(5, 0, 10); v != 5 {
		t.Fatalf("expected 5, got %d", v)
	}
}

func TestClamp_BelowMin(t *testing.T) {
	if v := clamp(-3, 0, 10); v != 0 {
		t.Fatalf("expected 0, got %d", v)
	}
}

func TestClamp_AboveMax(t *testing.T) {
	if v := clamp(15, 0, 10); v != 10 {
		t.Fatalf("expected 10, got %d", v)
	}
}

func TestClamp_AtBoundaries(t *testing.T) {
	if v := clamp(0, 0, 10); v != 0 {
		t.Fatalf("expected 0, got %d", v)
	}
	if v := clamp(10, 0, 10); v != 10 {
		t.Fatalf("expected 10, got %d", v)
	}
}

func TestClampByte_Normal(t *testing.T) {
	if v := clampByte(128); v != 128 {
		t.Fatalf("expected 128, got %d", v)
	}
}

func TestClampByte_Negative(t *testing.T) {
	if v := clampByte(-50); v != 0 {
		t.Fatalf("expected 0, got %d", v)
	}
}

func TestClampByte_Over255(t *testing.T) {
	if v := clampByte(300); v != 255 {
		t.Fatalf("expected 255, got %d", v)
	}
}

func TestClampByte_Boundaries(t *testing.T) {
	if v := clampByte(0); v != 0 {
		t.Fatalf("expected 0, got %d", v)
	}
	if v := clampByte(255); v != 255 {
		t.Fatalf("expected 255, got %d", v)
	}
}

// ---- toNRGBA ----

func TestToNRGBA_Passthrough(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	src.SetNRGBA(1, 1, color.NRGBA{R: 200, G: 100, B: 50, A: 255})

	dst := toNRGBA(src)
	c := dst.NRGBAAt(1, 1)
	if c.R != 200 || c.G != 100 || c.B != 50 || c.A != 255 {
		t.Fatalf("expected (200,100,50,255), got %v", c)
	}
}

func TestToNRGBA_FromRGBA(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 2, 2))
	src.SetRGBA(0, 0, color.RGBA{R: 128, G: 64, B: 32, A: 255})

	dst := toNRGBA(src)
	if dst.Bounds().Dx() != 2 || dst.Bounds().Dy() != 2 {
		t.Fatal("bounds mismatch")
	}
	c := dst.NRGBAAt(0, 0)
	if c.R != 128 || c.G != 64 || c.B != 32 {
		t.Fatalf("pixel mismatch: %v", c)
	}
}

// ---- cloneImage ----

func TestCloneImage_DeepCopy(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 3, 3))
	src.SetNRGBA(1, 1, color.NRGBA{R: 255, G: 0, B: 0, A: 255})

	dst := cloneImage(src)

	// Modify original
	src.SetNRGBA(1, 1, color.NRGBA{R: 0, G: 255, B: 0, A: 255})

	// Clone should be unaffected
	c := dst.NRGBAAt(1, 1)
	if c.R != 255 || c.G != 0 {
		t.Fatal("cloneImage did not make a deep copy")
	}
}

func TestCloneImage_SameBounds(t *testing.T) {
	src := image.NewNRGBA(image.Rect(5, 10, 15, 20))
	dst := cloneImage(src)
	if dst.Bounds() != src.Bounds() {
		t.Fatalf("bounds differ: %v vs %v", src.Bounds(), dst.Bounds())
	}
}

// ---- subImage ----

func TestSubImage_CorrectExtract(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 10, 10))
	// Fill pixel (5,5) with a known colour
	src.SetNRGBA(5, 5, color.NRGBA{R: 42, G: 84, B: 126, A: 255})

	sub := subImage(src, image.Rect(3, 3, 8, 8))
	if sub.Bounds().Dx() != 5 || sub.Bounds().Dy() != 5 {
		t.Fatalf("expected 5x5, got %dx%d", sub.Bounds().Dx(), sub.Bounds().Dy())
	}

	// Pixel (5,5) in src → (2,2) in sub
	c := sub.NRGBAAt(2, 2)
	if c.R != 42 || c.G != 84 || c.B != 126 {
		t.Fatalf("expected (42,84,126), got %v", c)
	}
}

func TestSubImage_ClampsToBounds(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 5, 5))
	sub := subImage(src, image.Rect(-10, -10, 100, 100))
	// Should clamp to source bounds
	if sub.Bounds().Dx() != 5 || sub.Bounds().Dy() != 5 {
		t.Fatalf("expected 5x5, got %dx%d", sub.Bounds().Dx(), sub.Bounds().Dy())
	}
}

// ---- imageToBase64 ----

func TestImageToBase64_ProducesDataURI(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	s, err := imageToBase64(img)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(s, "data:image/jpeg;base64,") {
		t.Fatal("missing data URI prefix")
	}
	// Base64 payload should be non-empty
	payload := strings.TrimPrefix(s, "data:image/jpeg;base64,")
	if len(payload) == 0 {
		t.Fatal("empty base64 payload")
	}
}

// ---- toGrayscale ----

func TestToGrayscale_White(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			src.SetNRGBA(x, y, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
		}
	}
	gray := toGrayscale(src)
	if gray.GrayAt(0, 0).Y != 255 {
		t.Fatalf("white should become gray 255, got %d", gray.GrayAt(0, 0).Y)
	}
}

func TestToGrayscale_Black(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	// Default is all zeros (black, transparent) — set alpha
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			src.SetNRGBA(x, y, color.NRGBA{R: 0, G: 0, B: 0, A: 255})
		}
	}
	gray := toGrayscale(src)
	if gray.GrayAt(0, 0).Y != 0 {
		t.Fatalf("black should become gray 0, got %d", gray.GrayAt(0, 0).Y)
	}
}

func TestToGrayscale_Dimensions(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 7, 13))
	gray := toGrayscale(src)
	if gray.Bounds().Dx() != 7 || gray.Bounds().Dy() != 13 {
		t.Fatalf("expected 7x13, got %dx%d", gray.Bounds().Dx(), gray.Bounds().Dy())
	}
}

// ---- applyAccentAdjustment ----

func TestApplyAccentAdjustment_Zero(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	src.SetNRGBA(0, 0, color.NRGBA{R: 100, G: 100, B: 100, A: 255})

	dst := applyAccentAdjustment(src, 0)
	c := dst.NRGBAAt(0, 0)
	if c.R != 100 || c.G != 100 || c.B != 100 {
		t.Fatalf("zero accent should be identity, got %v", c)
	}
}

func TestApplyAccentAdjustment_Positive(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	src.SetNRGBA(0, 0, color.NRGBA{R: 100, G: 100, B: 100, A: 255})

	dst := applyAccentAdjustment(src, 50)
	c := dst.NRGBAAt(0, 0)
	if c.R != 150 || c.G != 150 || c.B != 150 {
		t.Fatalf("expected (150,150,150), got %v", c)
	}
}

func TestApplyAccentAdjustment_ClampHigh(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	src.SetNRGBA(0, 0, color.NRGBA{R: 200, G: 200, B: 200, A: 255})

	dst := applyAccentAdjustment(src, 100)
	c := dst.NRGBAAt(0, 0)
	if c.R != 255 || c.G != 255 || c.B != 255 {
		t.Fatalf("should clamp to 255, got %v", c)
	}
}

func TestApplyAccentAdjustment_Negative(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	src.SetNRGBA(0, 0, color.NRGBA{R: 30, G: 30, B: 30, A: 255})

	dst := applyAccentAdjustment(src, -50)
	c := dst.NRGBAAt(0, 0)
	if c.R != 0 || c.G != 0 || c.B != 0 {
		t.Fatalf("should clamp to 0, got %v", c)
	}
}

func TestApplyAccentAdjustment_PreservesAlpha(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	src.SetNRGBA(0, 0, color.NRGBA{R: 100, G: 100, B: 100, A: 128})

	dst := applyAccentAdjustment(src, 20)
	if dst.NRGBAAt(0, 0).A != 128 {
		t.Fatal("accent adjustment should not change alpha")
	}
}

// ---- applyCLAHE ----

func TestApplyCLAHE_OutputRange(t *testing.T) {
	// Create a grayscale image with known values
	src := image.NewGray(image.Rect(0, 0, 32, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			src.SetGray(x, y, color.Gray{Y: uint8((x + y) * 4 % 256)})
		}
	}

	dst := applyCLAHE(src, 2.0, 8)

	if dst.Bounds().Dx() != 32 || dst.Bounds().Dy() != 32 {
		t.Fatal("CLAHE should preserve dimensions")
	}

	// All output values should be valid [0, 255]
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			v := dst.GrayAt(x, y).Y
			if v > 255 {
				t.Fatalf("out of range at (%d,%d): %d", x, y, v)
			}
		}
	}
}

func TestApplyCLAHE_UniformImage(t *testing.T) {
	// Uniform image: CLAHE should return all same value (or close to it)
	src := image.NewGray(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			src.SetGray(x, y, color.Gray{Y: 128})
		}
	}

	dst := applyCLAHE(src, 2.0, 8)

	// All pixels should still be close to the same value
	first := dst.GrayAt(0, 0).Y
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			diff := int(dst.GrayAt(x, y).Y) - int(first)
			if diff < -5 || diff > 5 {
				t.Fatalf("uniform input should produce near-uniform output; pixel (%d,%d) differs by %d", x, y, diff)
			}
		}
	}
}

// ---- goodFeaturesToTrack ----

func TestGoodFeaturesToTrack_BlackImage(t *testing.T) {
	// All black → no gradients → no corners
	gray := image.NewGray(image.Rect(0, 0, 64, 64))
	pts := goodFeaturesToTrack(gray, 10, 0.01, 10, 3)
	if len(pts) != 0 {
		t.Fatalf("expected 0 corners on blank image, got %d", len(pts))
	}
}

func TestGoodFeaturesToTrack_SingleCorner(t *testing.T) {
	// Draw a white square on black background — should detect corners
	gray := image.NewGray(image.Rect(0, 0, 64, 64))
	for y := 20; y < 44; y++ {
		for x := 20; x < 44; x++ {
			gray.SetGray(x, y, color.Gray{Y: 255})
		}
	}

	pts := goodFeaturesToTrack(gray, 20, 0.01, 5, 3)
	if len(pts) == 0 {
		t.Fatal("expected at least one corner on a white square")
	}
}

func TestGoodFeaturesToTrack_RespectsMaxCorners(t *testing.T) {
	gray := image.NewGray(image.Rect(0, 0, 64, 64))
	for y := 20; y < 44; y++ {
		for x := 20; x < 44; x++ {
			gray.SetGray(x, y, color.Gray{Y: 255})
		}
	}

	pts := goodFeaturesToTrack(gray, 2, 0.01, 5, 3)
	if len(pts) > 2 {
		t.Fatalf("expected at most 2 corners, got %d", len(pts))
	}
}

// ---- perspectiveTransform ----

func TestPerspectiveTransform_IdentityMapping(t *testing.T) {
	// 10×10 image with a known pixel
	src := image.NewNRGBA(image.Rect(0, 0, 10, 10))
	for y := 0; y < 10; y++ {
		for x := 0; x < 10; x++ {
			src.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 25), G: uint8(y * 25), B: 128, A: 255})
		}
	}

	corners := [4]image.Point{{0, 0}, {9, 0}, {9, 9}, {0, 9}}
	dst := perspectiveTransform(src, corners, corners, 10, 10)

	if dst.Bounds().Dx() != 10 || dst.Bounds().Dy() != 10 {
		t.Fatal("output size mismatch")
	}

	// Interior pixels should be approximately preserved (bilinear may shift by 1)
	c := dst.NRGBAAt(5, 5)
	// Expected: R≈125, G≈125, B=128
	if math.Abs(float64(c.R)-125) > 30 || math.Abs(float64(c.G)-125) > 30 {
		t.Fatalf("interior pixel (5,5) too far off: %v", c)
	}
}

func TestPerspectiveTransform_OutputDimensions(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 100, 100))
	srcPts := [4]image.Point{{10, 10}, {90, 10}, {90, 90}, {10, 90}}
	dstPts := [4]image.Point{{0, 0}, {50, 0}, {50, 80}, {0, 80}}

	out := perspectiveTransform(src, srcPts, dstPts, 50, 80)
	if out.Bounds().Dx() != 50 || out.Bounds().Dy() != 80 {
		t.Fatalf("expected 50×80, got %d×%d", out.Bounds().Dx(), out.Bounds().Dy())
	}
}

// ---- rotate90 ----

func TestRotate90_CW_Dimensions(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 4, 2))
	dst := rotate90(src, 1)
	// 4×2 rotated CW → 2×4
	if dst.Bounds().Dx() != 2 || dst.Bounds().Dy() != 4 {
		t.Fatalf("expected 2×4, got %d×%d", dst.Bounds().Dx(), dst.Bounds().Dy())
	}
}

func TestRotate90_CCW_Dimensions(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 4, 2))
	dst := rotate90(src, 0)
	if dst.Bounds().Dx() != 2 || dst.Bounds().Dy() != 4 {
		t.Fatalf("expected 2×4, got %d×%d", dst.Bounds().Dx(), dst.Bounds().Dy())
	}
}

func TestRotate90_CW_PixelPlacement(t *testing.T) {
	// 3×2 image: mark top-left red
	src := image.NewNRGBA(image.Rect(0, 0, 3, 2))
	src.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})

	dst := rotate90(src, 1) // CW: (0,0) → (h-1-0, 0) = (1, 0) in 2×3 output
	c := dst.NRGBAAt(1, 0)
	if c.R != 255 {
		t.Fatalf("CW rotation: expected red at (1,0), got %v", c)
	}
}

func TestRotate90_FourRotationsIdentity(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	src.SetNRGBA(1, 0, color.NRGBA{R: 42, G: 84, B: 126, A: 255})

	r := src
	for i := 0; i < 4; i++ {
		r = rotate90(r, 1)
	}

	// After 4 CW rotations, should be back to original
	c := r.NRGBAAt(1, 0)
	if c.R != 42 || c.G != 84 || c.B != 126 {
		t.Fatalf("4 CW rotations should be identity; pixel (1,0): %v", c)
	}
}

// ---- rotateArbitrary ----

func TestRotateArbitrary_ZeroDegrees(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 10, 10))
	src.SetNRGBA(5, 5, color.NRGBA{R: 200, G: 100, B: 50, A: 255})
	bg := color.NRGBA{R: 0, G: 0, B: 0, A: 255}

	dst := rotateArbitrary(src, 0, bg)
	c := dst.NRGBAAt(5, 5)
	if c.R != 200 || c.G != 100 || c.B != 50 {
		t.Fatalf("0° rotation should preserve pixels, got %v", c)
	}
}

func TestRotateArbitrary_PreservesDimensions(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 20, 30))
	bg := color.NRGBA{A: 255}
	dst := rotateArbitrary(src, 45, bg)
	if dst.Bounds().Dx() != 20 || dst.Bounds().Dy() != 30 {
		t.Fatalf("expected 20×30, got %d×%d", dst.Bounds().Dx(), dst.Bounds().Dy())
	}
}

func TestRotateArbitrary_360Degrees(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 10, 10))
	src.SetNRGBA(5, 5, color.NRGBA{R: 200, G: 100, B: 50, A: 255})
	bg := color.NRGBA{R: 0, G: 0, B: 0, A: 255}

	dst := rotateArbitrary(src, 360, bg)
	c := dst.NRGBAAt(5, 5)
	// Should be approximately the same (tiny floating point differences possible)
	if math.Abs(float64(c.R)-200) > 2 || math.Abs(float64(c.G)-100) > 2 {
		t.Fatalf("360° rotation should ≈ identity, got %v", c)
	}
}

// ---- applyCircularMaskWithFeather ----

func TestApplyCircularMask_CenterPixel(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 50, 50))
	for y := 0; y < 50; y++ {
		for x := 0; x < 50; x++ {
			src.SetNRGBA(x, y, color.NRGBA{R: 200, G: 100, B: 50, A: 255})
		}
	}
	bg := color.NRGBA{R: 0, G: 0, B: 0, A: 255}
	center := image.Pt(25, 25)

	dst := applyCircularMaskWithFeather(src, center, 20, 5, bg)

	// Center should be unchanged (inside inner radius)
	c := dst.NRGBAAt(25, 25)
	if c.R != 200 || c.G != 100 || c.B != 50 {
		t.Fatalf("center pixel should be source colour, got %v", c)
	}
}

func TestApplyCircularMask_FarPixel(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 50, 50))
	for y := 0; y < 50; y++ {
		for x := 0; x < 50; x++ {
			src.SetNRGBA(x, y, color.NRGBA{R: 200, G: 100, B: 50, A: 255})
		}
	}
	bg := color.NRGBA{R: 0, G: 0, B: 0, A: 255}
	center := image.Pt(25, 25)

	dst := applyCircularMaskWithFeather(src, center, 10, 3, bg)

	// Corner pixel (0,0) is far outside radius+feather → should be bg
	c := dst.NRGBAAt(0, 0)
	if c.R != 0 || c.G != 0 || c.B != 0 {
		t.Fatalf("far pixel should be background, got %v", c)
	}
}

func TestApplyCircularMask_Dimensions(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 30, 40))
	bg := color.NRGBA{A: 255}
	dst := applyCircularMaskWithFeather(src, image.Pt(15, 20), 10, 5, bg)
	if dst.Bounds().Dx() != 30 || dst.Bounds().Dy() != 40 {
		t.Fatal("dimensions should match source")
	}
}

// ---- drawFilledCircle ----

func TestDrawFilledCircle_CenterColoured(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 20, 20))
	c := color.NRGBA{R: 255, G: 0, B: 0, A: 255}
	drawFilledCircle(img, image.Pt(10, 10), 5, c)

	// Centre should be red
	got := img.NRGBAAt(10, 10)
	if got.R != 255 {
		t.Fatalf("center should be red, got %v", got)
	}
}

func TestDrawFilledCircle_OutsideUnchanged(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 20, 20))
	c := color.NRGBA{R: 255, G: 0, B: 0, A: 255}
	drawFilledCircle(img, image.Pt(10, 10), 3, c)

	// Far corner should be untouched (transparent black)
	got := img.NRGBAAt(0, 0)
	if got.R != 0 || got.G != 0 || got.B != 0 {
		t.Fatalf("outside pixel should be unchanged, got %v", got)
	}
}

func TestDrawFilledCircle_EdgePixels(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 20, 20))
	c := color.NRGBA{R: 255, G: 0, B: 0, A: 255}
	drawFilledCircle(img, image.Pt(10, 10), 5, c)

	// Pixel exactly at radius (5,0 offset) should be included (5²+0²=25 ≤ 25)
	got := img.NRGBAAt(15, 10)
	if got.R != 255 {
		t.Fatalf("pixel at exact radius should be filled, got %v", got)
	}
}

// ---- resizeGray ----

func TestResizeGray_Dimensions(t *testing.T) {
	src := image.NewGray(image.Rect(0, 0, 100, 80))
	dst := resizeGray(src, 50, 40)
	if dst.Bounds().Dx() != 50 || dst.Bounds().Dy() != 40 {
		t.Fatalf("expected 50×40, got %d×%d", dst.Bounds().Dx(), dst.Bounds().Dy())
	}
}

func TestResizeGray_Upscale(t *testing.T) {
	src := image.NewGray(image.Rect(0, 0, 2, 2))
	src.SetGray(0, 0, color.Gray{Y: 100})
	src.SetGray(1, 0, color.Gray{Y: 200})
	src.SetGray(0, 1, color.Gray{Y: 50})
	src.SetGray(1, 1, color.Gray{Y: 150})

	dst := resizeGray(src, 4, 4)
	if dst.Bounds().Dx() != 4 || dst.Bounds().Dy() != 4 {
		t.Fatal("upscale dimensions wrong")
	}

	// Top-left quadrant should map to (0,0) = 100
	if dst.GrayAt(0, 0).Y != 100 {
		t.Fatalf("expected 100, got %d", dst.GrayAt(0, 0).Y)
	}
}

func TestResizeGray_ZeroSize(t *testing.T) {
	src := image.NewGray(image.Rect(0, 0, 10, 10))
	dst := resizeGray(src, 0, 0)
	// Should return src unchanged
	if dst.Bounds().Dx() != 10 {
		t.Fatal("zero-size resize should return original")
	}
}

func TestResizeGray_PreservesContent(t *testing.T) {
	// 4×4 all white → resize to 2×2 → should still be white
	src := image.NewGray(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			src.SetGray(x, y, color.Gray{Y: 255})
		}
	}
	dst := resizeGray(src, 2, 2)
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			if dst.GrayAt(x, y).Y != 255 {
				t.Fatalf("pixel (%d,%d) should be 255", x, y)
			}
		}
	}
}

// ---- resizeNRGBA ----

func TestResizeNRGBA_Dimensions(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 100, 200))
	dst := resizeNRGBA(src, 50, 100)
	if dst.Bounds().Dx() != 50 || dst.Bounds().Dy() != 100 {
		t.Fatalf("expected 50x100, got %dx%d", dst.Bounds().Dx(), dst.Bounds().Dy())
	}
}

func TestResizeNRGBA_ZeroSize(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 10, 10))
	dst := resizeNRGBA(src, 0, 0)
	if dst.Bounds().Dx() != 10 {
		t.Fatal("zero-size resize should return original")
	}
}

func TestResizeNRGBA_PreservesColor(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	red := color.NRGBA{R: 255, G: 0, B: 0, A: 255}
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			src.SetNRGBA(x, y, red)
		}
	}
	dst := resizeNRGBA(src, 2, 2)
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			c := dst.NRGBAAt(x, y)
			if c.R != 255 || c.G != 0 || c.B != 0 || c.A != 255 {
				t.Fatalf("pixel (%d,%d) = %v, want red", x, y, c)
			}
		}
	}
}

func TestImageToBase64_DownscalesLargeImages(t *testing.T) {
	// Create a large image that triggers downscaling
	img := image.NewNRGBA(image.Rect(0, 0, 3200, 2400))
	s, err := imageToBase64(img)
	if err != nil {
		t.Fatal(err)
	}
	// Should still produce a valid data URI
	if !strings.HasPrefix(s, "data:image/jpeg;base64,") {
		t.Fatal("expected JPEG data URI for large image")
	}
	// The base64 payload should be far smaller than full-res PNG would be
	if len(s) > 500000 {
		t.Fatalf("preview too large: %d bytes, expected under 500KB", len(s))
	}
}
