package main

import (
	"image"
	"image/color"
	"image/png"
	"math/rand"
	"os"
	"path/filepath"
	"testing"

	"atropos/internal/imageops"
)

func makeRichFeatureImage(w, h int, seed int64) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	rng := rand.New(rand.NewSource(seed))
	// Multi-scale texture ensures strong Shi-Tomasi corners with unambiguous descriptors.
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			base := 80 + (x*90)/w + (y*60)/h + rng.Intn(45) - 22
			if base < 0 {
				base = 0
			} else if base > 255 {
				base = 255
			}
			v := uint8(base)
			img.SetNRGBA(x, y, color.NRGBA{R: v, G: v, B: v, A: 255})
		}
	}
	for i := 0; i < 160; i++ {
		bx := rng.Intn(w - 14)
		by := rng.Intn(h - 14)
		bw := 5 + rng.Intn(10)
		bh := 5 + rng.Intn(10)
		col := color.NRGBA{
			R: uint8(rng.Intn(256)),
			G: uint8(rng.Intn(256)),
			B: uint8(rng.Intn(256)),
			A: 255,
		}
		for py := by; py < by+bh && py < h; py++ {
			for px := bx; px < bx+bw && px < w; px++ {
				img.SetNRGBA(px, py, col)
			}
		}
	}
	return img
}

func cloneSubRectNRGBA(src *image.NRGBA, rect image.Rectangle) *image.NRGBA {
	dst := image.NewNRGBA(image.Rect(0, 0, rect.Dx(), rect.Dy()))
	for y := 0; y < rect.Dy(); y++ {
		for x := 0; x < rect.Dx(); x++ {
			dst.SetNRGBA(x, y, src.NRGBAAt(rect.Min.X+x, rect.Min.Y+y))
		}
	}
	return dst
}

func writeCompositorTestPNG(t *testing.T, dir, name string, img *image.NRGBA) string {
	t.Helper()
	p := filepath.Join(dir, name)
	f, err := os.Create(p)
	if err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatalf("encode %s: %v", name, err)
	}
	return p
}

func TestCompositorStitchWithPerImageRotation(t *testing.T) {
	t.Setenv("AppData", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	a := NewApp()
	dir := t.TempDir()

	full := makeRichFeatureImage(420, 260, 99)
	left := cloneSubRectNRGBA(full, image.Rect(0, 0, 260, 260))
	right := cloneSubRectNRGBA(full, image.Rect(160, 0, 420, 260))

	rightRot180 := imageops.Rotate90(imageops.Rotate90(right, 1), 1)
	rightRot90CCW := imageops.Rotate90(right, -1)
	rightRot90CW := imageops.Rotate90(right, 1)

	leftPath := writeCompositorTestPNG(t, dir, "left.png", left)
	rot180Path := writeCompositorTestPNG(t, dir, "right_180.png", rightRot180)
	rotCCWPath := writeCompositorTestPNG(t, dir, "right_ccw.png", rightRot90CCW)
	rotCWPath := writeCompositorTestPNG(t, dir, "right_cw.png", rightRot90CW)

	cases := []struct {
		name string
		req  CompositorStitchRequest
	}{
		{
			name: "ltr_180_opposite_second",
			req: CompositorStitchRequest{
				ImagePaths:    []string{leftPath, rot180Path},
				Orientation:   "ltr",
				RotationSteps: []int{0, 2},
			},
		},
		{
			name: "rtl_reverses_paths_and_rotations_together",
			req: CompositorStitchRequest{
				ImagePaths:    []string{rot180Path, leftPath},
				Orientation:   "rtl",
				RotationSteps: []int{2, 0},
			},
		},
		{
			name: "ltr_90cw_correction",
			req: CompositorStitchRequest{
				ImagePaths:    []string{leftPath, rotCCWPath},
				Orientation:   "ltr",
				RotationSteps: []int{0, 1},
			},
		},
		{
			name: "ltr_270cw_correction",
			req: CompositorStitchRequest{
				ImagePaths:    []string{leftPath, rotCWPath},
				Orientation:   "ltr",
				RotationSteps: []int{0, 3},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := a.CompositorStitch(tc.req)
			if err != nil {
				t.Fatalf("CompositorStitch failed: %v", err)
			}
			if res.Width <= 360 || res.Height < 220 {
				t.Fatalf("unexpected stitched dimensions %dx%d (want Width > 360, Height >= 220)", res.Width, res.Height)
			}
		})
	}
}

func TestCompositorAlignOverlayWithReferenceRotation(t *testing.T) {
	t.Setenv("AppData", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	a := NewApp()
	dir := t.TempDir()

	scan := makeRichFeatureImage(260, 180, 1234)
	scan1Path := writeCompositorTestPNG(t, dir, "scan1.png", scan)
	scan2Path := writeCompositorTestPNG(t, dir, "scan2.png", scan)

	// Rotate reference scan by 90 deg CW (260x180 -> 180x260) before overlay alignment.
	res, err := a.CompositorAlignOverlay(CompositorStitchRequest{
		ImagePaths:    []string{scan1Path, scan2Path},
		Orientation:   "overlay",
		RotationSteps: []int{1, 0},
	})
	if err != nil {
		t.Fatalf("CompositorAlignOverlay failed: %v", err)
	}
	if res.Width != 180 || res.Height != 260 {
		t.Fatalf("overlay dims = %dx%d, want 180x260 (90 deg rotated reference)", res.Width, res.Height)
	}
}
