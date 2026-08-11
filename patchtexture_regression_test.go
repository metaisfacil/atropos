package main

import (
	"context"
	"image"
	"image/color"
	"math"
	"testing"
)

func TestPatchMatchFillPreservesFineStochasticTexture(t *testing.T) {
	const w, h = 176, 132
	original := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			fine := int(pmHash(uint32(x), uint32(y), 0x6a09e667)%51) - 25
			coarse := int(pmHash(uint32(x/2), uint32(y/2), 0xbb67ae85)%19) - 9
			base := 38 + coarse + fine
			original.SetNRGBA(x, y, color.NRGBA{
				R: byte(clampInt(base+fine/8, 0, 255)),
				G: byte(clampInt(base, 0, 255)),
				B: byte(clampInt(base-fine/10, 0, 255)),
				A: 255,
			})
		}
	}

	src := cloneNRGBA(original)
	mask := image.NewAlpha(src.Bounds())
	cx, cy, radius := w/2, h/2, 20
	for y := cy - radius; y <= cy+radius; y++ {
		for x := cx - radius; x <= cx+radius; x++ {
			dx, dy := x-cx, y-cy
			if dx*dx+dy*dy > radius*radius {
				continue
			}
			mask.Pix[y*mask.Stride+x] = 255
			src.SetNRGBA(x, y, color.NRGBA{R: 210, G: 196, B: 164, A: 255})
		}
	}

	out, err := PatchMatchFill(context.Background(), src, mask, 13, 5)
	if err != nil {
		t.Fatal(err)
	}
	inner := image.Rect(cx-radius+4, cy-radius+4, cx+radius-3, cy+radius-3)
	wantEnergy := pmTestHighFrequencyEnergy(original, inner)
	gotEnergy := pmTestHighFrequencyEnergy(out, inner)
	wantDeviation := pmTestLumaDeviation(original, inner)
	gotDeviation := pmTestLumaDeviation(out, inner)
	if gotEnergy < wantEnergy*0.80 {
		t.Fatalf("fine texture energy collapsed: got %.3f, want at least 80%% of %.3f", gotEnergy, wantEnergy)
	}
	if gotDeviation < wantDeviation*0.78 {
		t.Fatalf("fine texture variance collapsed: got %.3f, want at least 78%% of %.3f", gotDeviation, wantDeviation)
	}
}

func TestPatchMatchFillPreservesTextureBesideCrossingEdge(t *testing.T) {
	const w, h = 160, 112
	original := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			value := color.NRGBA{R: 205, G: 184, B: 111, A: 255}
			if y < 58 {
				fine := int(pmHash(uint32(x), uint32(y), 0x3c6ef372)%47) - 23
				coarse := int(pmHash(uint32(x/2), uint32(y/2), 0xa54ff53a)%15) - 7
				base := 39 + fine + coarse
				value = color.NRGBA{
					R: byte(clampInt(base+fine/9, 0, 255)),
					G: byte(clampInt(base, 0, 255)),
					B: byte(clampInt(base-fine/10, 0, 255)),
					A: 255,
				}
			}
			original.SetNRGBA(x, y, value)
		}
	}

	src := cloneNRGBA(original)
	mask := image.NewAlpha(src.Bounds())
	cx, cy, radius := 80, 54, 16
	for y := cy - radius; y <= cy+radius; y++ {
		for x := cx - radius; x <= cx+radius; x++ {
			dx, dy := x-cx, y-cy
			if dx*dx+dy*dy > radius*radius {
				continue
			}
			mask.Pix[y*mask.Stride+x] = 255
			src.SetNRGBA(x, y, color.NRGBA{R: 238, G: 238, B: 238, A: 255})
		}
	}

	out, err := PatchMatchFill(context.Background(), src, mask, 13, 5)
	if err != nil {
		t.Fatal(err)
	}
	darkRegion := image.Rect(70, 44, 90, 52)
	wantEnergy := pmTestHighFrequencyEnergy(original, darkRegion)
	gotEnergy := pmTestHighFrequencyEnergy(out, darkRegion)
	if gotEnergy < wantEnergy*0.78 {
		t.Fatalf("texture beside edge collapsed: got %.3f, want at least 78%% of %.3f", gotEnergy, wantEnergy)
	}
	dark := pmTestMeanLuma(out, image.Rect(70, 51, 90, 55))
	gold := pmTestMeanLuma(out, image.Rect(70, 61, 90, 65))
	if dark > 75 || gold < 145 || gold-dark < 85 {
		t.Fatalf("crossing edge damaged: dark=%.1f gold=%.1f contrast=%.1f", dark, gold, gold-dark)
	}
}

func pmTestPixelLuma(src *image.NRGBA, x, y int) float64 {
	p := src.NRGBAAt(x, y)
	return 0.299*float64(p.R) + 0.587*float64(p.G) + 0.114*float64(p.B)
}

func pmTestMeanLuma(src *image.NRGBA, bounds image.Rectangle) float64 {
	var sum float64
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			sum += pmTestPixelLuma(src, x, y)
		}
	}
	return sum / float64(bounds.Dx()*bounds.Dy())
}

func pmTestLumaDeviation(src *image.NRGBA, bounds image.Rectangle) float64 {
	mean := pmTestMeanLuma(src, bounds)
	var sum float64
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			d := pmTestPixelLuma(src, x, y) - mean
			sum += d * d
		}
	}
	return math.Sqrt(sum / float64(bounds.Dx()*bounds.Dy()))
}

func pmTestHighFrequencyEnergy(src *image.NRGBA, bounds image.Rectangle) float64 {
	var sum float64
	var count int
	for y := maxInt(1, bounds.Min.Y); y < minInt(src.Bounds().Dy()-1, bounds.Max.Y); y++ {
		for x := maxInt(1, bounds.Min.X); x < minInt(src.Bounds().Dx()-1, bounds.Max.X); x++ {
			center := pmTestPixelLuma(src, x, y)
			neighbors := (pmTestPixelLuma(src, x-1, y) + pmTestPixelLuma(src, x+1, y) +
				pmTestPixelLuma(src, x, y-1) + pmTestPixelLuma(src, x, y+1)) * 0.25
			sum += math.Abs(center - neighbors)
			count++
		}
	}
	return sum / float64(count)
}
