package main

import (
	"context"
	"image"
	"image/color"
	"testing"
)

func TestLineDerivedCornerProposalsFindsRectangle(t *testing.T) {
	gray := image.NewGray(image.Rect(0, 0, 320, 240))
	for y := 0; y < 240; y++ {
		for x := 0; x < 320; x++ {
			gray.SetGray(x, y, color.Gray{Y: 245})
		}
	}
	for y := 45; y <= 195; y++ {
		for x := 55; x <= 270; x++ {
			gray.SetGray(x, y, color.Gray{Y: 80})
		}
	}
	got, err := lineDerivedCornerProposals(context.Background(), []*image.Gray{gray}, 500, 10)
	if err != nil {
		t.Fatal(err)
	}
	want := []image.Point{{55, 45}, {270, 45}, {55, 195}, {270, 195}}
	for _, corner := range want {
		found := false
		for _, candidate := range got {
			if dist(corner, candidate) <= 6 {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("no proposal near %v; got %v", corner, got)
		}
	}
}

func TestBackgroundDistanceSilhouetteSeparatesColourFromDarkTexture(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 160, 100))
	for y := 0; y < 100; y++ {
		for x := 0; x < 160; x++ {
			variation := uint8((x*7 + y*11) % 9)
			img.SetNRGBA(x, y, color.NRGBA{R: 48 + variation, G: 62 + variation, B: 65 + variation, A: 255})
		}
	}
	for y := 20; y < 80; y++ {
		for x := 30; x < 130; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: 220, G: 95, B: 35, A: 255})
		}
	}
	silhouette, background := backgroundDistanceSilhouette(img)
	if !background.dark {
		t.Fatal("dark perimeter was not classified as dark")
	}
	if silhouette.GrayAt(5, 5).Y > 10 {
		t.Fatalf("background texture was not suppressed: %d", silhouette.GrayAt(5, 5).Y)
	}
	if silhouette.GrayAt(80, 50).Y < 240 {
		t.Fatalf("contrasting insert was not isolated: %d", silhouette.GrayAt(80, 50).Y)
	}
}

func TestLineDerivedCornerProposalsIgnoresSingleLine(t *testing.T) {
	gray := image.NewGray(image.Rect(0, 0, 320, 240))
	for x := 20; x < 300; x++ {
		gray.SetGray(x, 100, color.Gray{Y: 255})
	}
	got, err := lineDerivedCornerProposals(context.Background(), []*image.Gray{gray}, 500, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("single line produced corner proposals: %v", got)
	}
}

func TestValidateFloatQuadRejectsConcaveShape(t *testing.T) {
	quad := [4]floatPoint{{10, 10}, {100, 10}, {40, 40}, {10, 100}}
	_, convex, _ := validateFloatQuad(quad)
	if convex {
		t.Fatal("concave quadrilateral was accepted")
	}
}
