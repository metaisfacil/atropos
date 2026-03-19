package main

import (
	"context"
	"image"
	"image/color"
	"image/draw"
	"testing"
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
