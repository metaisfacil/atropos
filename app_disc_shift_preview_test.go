package main

import (
	"image"
	"image/color"
	"math"
	"testing"

	"atropos/internal/imageops"
)

// composeDiscShiftPreview reproduces, at full resolution, what the UI draws
// during a Ctrl+drag: source content shifted by (dx,dy) and mapped through the
// preview geometry, beneath the stationary vignette.
func composeDiscShiftPreview(src *image.NRGBA, p *DiscShiftPreview, dx, dy int) *image.NRGBA {
	out := image.NewNRGBA(image.Rect(0, 0, p.Width, p.Height))
	mask := imageops.NewCircularMask(p.Radius, p.FeatherSize, p.CutoutRadius)
	rad := p.Rotation * math.Pi / 180
	cos, sin := math.Cos(rad), math.Sin(rad)
	levels := func(v float64) float64 {
		if p.LevelsBlack == 0 && p.LevelsWhite == 255 {
			return v
		}
		return (v - float64(p.LevelsBlack)) * 255 / float64(p.LevelsWhite-p.LevelsBlack)
	}
	b := src.Bounds()
	sample := func(sx, sy float64, ch int) float64 {
		// Bilinear on pixel centres, clamped to the source.
		fx, fy := sx-0.5, sy-0.5
		x0, y0 := int(math.Floor(fx)), int(math.Floor(fy))
		wx, wy := fx-float64(x0), fy-float64(y0)
		at := func(x, y int) float64 {
			x = clamp(x, b.Min.X, b.Max.X-1)
			y = clamp(y, b.Min.Y, b.Max.Y-1)
			return float64(src.Pix[src.PixOffset(x, y)+ch])
		}
		return at(x0, y0)*(1-wx)*(1-wy) + at(x0+1, y0)*wx*(1-wy) +
			at(x0, y0+1)*(1-wx)*wy + at(x0+1, y0+1)*wx*wy
	}
	bg := [3]float64{float64(p.BgR), float64(p.BgG), float64(p.BgB)}
	for y := 0; y < p.Height; y++ {
		for x := 0; x < p.Width; x++ {
			px, py := float64(x)+0.5, float64(y)+0.5
			rx, ry := px-p.PivotX, py-p.PivotY
			sx := cos*rx + sin*ry + p.SourcePivotX + float64(dx)
			sy := -sin*rx + cos*ry + p.SourcePivotY + float64(dy)
			mx, my := px-p.MaskCenterX, py-p.MaskCenterY
			a := mask.Alpha(mx*mx + my*my)
			off := out.PixOffset(x, y)
			for ch := 0; ch < 3; ch++ {
				v := a*levels(sample(sx, sy, ch)) + (1-a)*bg[ch]
				out.Pix[off+ch] = uint8(math.Max(0, math.Min(255, math.Round(v))))
			}
			out.Pix[off+3] = 255
		}
	}
	return out
}

func fillDiscShiftTestPattern(img *image.NRGBA, sharp bool) {
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			var r, g, bl float64
			if sharp {
				r = float64((x*7 + y*13) % 200)
				g = float64((x*3 + y*29) % 200)
				bl = float64((x*x + y) % 200)
			} else {
				r = 100 + 90*math.Sin(float64(x)/23)
				g = 100 + 90*math.Cos(float64(y)/31)
				bl = 100 + 90*math.Sin(float64(x+y)/41)
			}
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(20 + r), G: uint8(20 + g), B: uint8(20 + bl), A: 255})
		}
	}
}

func diffStats(t *testing.T, got, want *image.NRGBA) (mean float64, maxDiff int) {
	t.Helper()
	if got.Bounds().Size() != want.Bounds().Size() {
		t.Fatalf("size mismatch: got %v want %v", got.Bounds().Size(), want.Bounds().Size())
	}
	var sum, n float64
	for y := 0; y < want.Bounds().Dy(); y++ {
		for x := 0; x < want.Bounds().Dx(); x++ {
			go1 := got.PixOffset(got.Bounds().Min.X+x, got.Bounds().Min.Y+y)
			wo := want.PixOffset(x, y)
			for ch := 0; ch < 3; ch++ {
				d := int(got.Pix[go1+ch]) - int(want.Pix[wo+ch])
				if d < 0 {
					d = -d
				}
				sum += float64(d)
				n++
				maxDiff = max(maxDiff, d)
			}
		}
	}
	return sum / n, maxDiff
}

// The live Ctrl+drag preview must match what ShiftDisc commits, so releasing
// the mouse does not visibly change the viewport.
func TestDiscShiftPreviewMatchesCommittedShift(t *testing.T) {
	cases := []struct {
		name      string
		sharp     bool
		rotate    float64
		crops     []CropRequest
		levels    [2]int
		dx, dy    int
		maxMean   float64
		maxSingle int
	}{
		{name: "plain", sharp: true, dx: 9, dy: -6, maxMean: 0.5, maxSingle: 1},
		{name: "insets+levels", sharp: true, dx: -13, dy: 4,
			crops:  []CropRequest{{Direction: "top", Amount: 7}, {Direction: "left", Amount: 11}, {Direction: "right", Amount: 3}},
			levels: [2]int{10, 240}, maxMean: 0.6, maxSingle: 3},
		{name: "rotated+insets", rotate: 17, dx: 12, dy: 8,
			crops:   []CropRequest{{Direction: "bottom", Amount: 6}, {Direction: "left", Amount: 9}},
			maxMean: 0.6, maxSingle: 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := newTestApp(320, 300)
			fillDiscShiftTestPattern(a.currentImage, tc.sharp)
			a.bgColor = color.NRGBA{R: 200, G: 40, B: 90, A: 255}
			a.featherSize = 12
			a.discCenterCutout = true
			a.discCutoutPercent = 15
			if _, err := a.DrawDisc(DiscDrawRequest{CenterX: 160, CenterY: 150, Radius: 90}); err != nil {
				t.Fatal(err)
			}
			if tc.rotate != 0 {
				if _, err := a.RotateDisc(DiscRotateRequest{Angle: tc.rotate}); err != nil {
					t.Fatal(err)
				}
			}
			for _, c := range tc.crops {
				if _, err := a.Crop(c); err != nil {
					t.Fatal(err)
				}
			}
			if tc.levels != ([2]int{}) {
				a.postDiscBlack, a.postDiscWhite = tc.levels[0], tc.levels[1]
				if _, err := a.redrawDisc(); err != nil {
					t.Fatal(err)
				}
			}

			preview, err := a.DiscShiftPreview()
			if err != nil {
				t.Fatal(err)
			}
			src, ok := a.ResolvePreviewImage(preview.Source)
			if !ok || src != a.discBaseImage {
				t.Fatal("preview source does not resolve to the disc base image")
			}
			if b := a.warpedImage.Bounds(); preview.Width != b.Dx() || preview.Height != b.Dy() {
				t.Fatalf("preview dims %dx%d, warped %v", preview.Width, preview.Height, b)
			}

			// Before any shift the preview must already match the current render.
			mean, maxD := diffStats(t, a.warpedImage, composeDiscShiftPreview(src, preview, 0, 0))
			if mean > tc.maxMean || maxD > tc.maxSingle {
				t.Fatalf("unshifted preview differs from render: mean=%.3f max=%d", mean, maxD)
			}

			want := composeDiscShiftPreview(src, preview, tc.dx, tc.dy)
			if _, err := a.ShiftDisc(ShiftDiscRequest{DX: tc.dx, DY: tc.dy}); err != nil {
				t.Fatal(err)
			}
			mean, maxD = diffStats(t, a.warpedImage, want)
			t.Logf("committed vs preview: mean=%.3f max=%d", mean, maxD)
			if mean > tc.maxMean || maxD > tc.maxSingle {
				t.Fatalf("shift preview differs from committed shift: mean=%.3f max=%d", mean, maxD)
			}
		})
	}
}

func TestDiscShiftPreviewUnavailable(t *testing.T) {
	a := newTestApp(200, 200)
	if _, err := a.DiscShiftPreview(); err == nil {
		t.Fatal("expected error with no disc")
	}
	if _, err := a.DrawDisc(DiscDrawRequest{CenterX: 100, CenterY: 100, Radius: 60}); err != nil {
		t.Fatal(err)
	}
	a.bakeDiscWorkingImage(a.warpedImage)
	if _, err := a.DiscShiftPreview(); err == nil {
		t.Fatal("expected error for a baked disc, whose redraw ignores the centre")
	}
}
