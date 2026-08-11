package main

import (
	"context"
	"image"
	"image/color"
	"math"
	"testing"
)

func TestPatchMatchFillPreservesSharpColourBoundary(t *testing.T) {
	const w, h = 200, 140
	edgeAt := func(x int) int { return 42 + x/4 }
	original := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if y < edgeAt(x) {
				fine := int(pmHash(uint32(x), uint32(y), 0x510e527f)%17) - 8
				original.SetNRGBA(x, y, color.NRGBA{R: byte(clampInt(48+fine, 0, 255)), G: byte(clampInt(40+fine, 0, 255)), B: byte(clampInt(45+fine/2, 0, 255)), A: 255})
			} else {
				fine := int(pmHash(uint32(x), uint32(y), 0x9b05688c)%11) - 5
				original.SetNRGBA(x, y, color.NRGBA{R: byte(clampInt(190+fine, 0, 255)), G: byte(clampInt(168+fine, 0, 255)), B: byte(clampInt(91+fine, 0, 255)), A: 255})
			}
		}
	}
	src := cloneNRGBA(original)
	mask := image.NewAlpha(src.Bounds())
	centers := []int{74, 98, 122}
	radius := 8
	for _, cx := range centers {
		cy := edgeAt(cx)
		for y := cy - radius; y <= cy+radius; y++ {
			for x := cx - radius; x <= cx+radius; x++ {
				dx, dy := x-cx, y-cy
				if dx*dx+dy*dy <= radius*radius {
					mask.Pix[y*mask.Stride+x] = 255
					src.SetNRGBA(x, y, color.NRGBA{R: 225, G: 220, B: 218, A: 255})
				}
			}
		}
	}
	out, err := PatchMatchFill(context.Background(), src, mask, 7, 4)
	if err != nil {
		t.Fatal(err)
	}
	var widths, errors []float64
	for _, cx := range centers {
		for x := cx - 4; x <= cx+4; x++ {
			ey := edgeAt(x)
			lowY, highY, midY := -1, -1, -1
			for y := ey - 8; y <= ey+8; y++ {
				v := rawTestLuma(out, x, y)
				if lowY < 0 && v >= 75 {
					lowY = y
				}
				if midY < 0 && v >= 112 {
					midY = y
				}
				if highY < 0 && v >= 150 {
					highY = y
				}
			}
			if lowY >= 0 && highY >= 0 {
				widths = append(widths, float64(highY-lowY))
			}
			if midY >= 0 {
				errors = append(errors, math.Abs(float64(midY-ey)))
			}
		}
	}
	mean := func(v []float64) float64 {
		s := 0.0
		for _, x := range v {
			s += x
		}
		return s / float64(len(v))
	}
	width := mean(widths)
	posErr := mean(errors)
	t.Logf("edge width %.2f position error %.2f", width, posErr)
	if width > 0.35 {
		t.Fatalf("repaired colour edge blurred: transition width %.2f px", width)
	}
	if posErr > 1.25 {
		t.Fatalf("repaired edge moved: mean position error %.2f px", posErr)
	}
}
func rawTestLuma(src *image.NRGBA, x, y int) float64 {
	p := src.NRGBAAt(x, y)
	return 0.299*float64(p.R) + 0.587*float64(p.G) + 0.114*float64(p.B)
}
