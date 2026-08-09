package main

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"strings"
	"testing"
)

func TestBuildStrokeMaskUsesBoundedGlobalCoordinates(t *testing.T) {
	mask, err := buildStrokeMask(
		image.Rect(0, 0, 4000, 3000),
		[]TouchUpPoint{{X: 100, Y: 200}, {X: 120, Y: 210}},
		40,
	)
	if err != nil {
		t.Fatalf("buildStrokeMask: %v", err)
	}

	if got, want := mask.Bounds(), image.Rect(79, 179, 141, 231); got != want {
		t.Fatalf("bounds = %v, want %v", got, want)
	}
	if len(mask.Pix) >= 4000*3000 {
		t.Fatalf("mask allocated %d bytes; expected bounded allocation", len(mask.Pix))
	}
	if mask.AlphaAt(110, 205).A != 255 {
		t.Fatal("stroke center was not fully covered")
	}
	if mask.AlphaAt(0, 0).A != 0 {
		t.Fatal("pixel outside bounded mask was covered")
	}
}

func TestBuildStrokeMaskSinglePointAndClipping(t *testing.T) {
	mask, err := buildStrokeMask(
		image.Rect(0, 0, 100, 80),
		[]TouchUpPoint{{X: 1, Y: 1}},
		20,
	)
	if err != nil {
		t.Fatalf("buildStrokeMask: %v", err)
	}

	if mask.Bounds().Min != (image.Point{}) {
		t.Fatalf("mask was not clipped to source bounds: %v", mask.Bounds())
	}
	if mask.AlphaAt(1, 1).A != 255 {
		t.Fatal("single click did not produce a round brush mark")
	}
}

func TestBuildStrokeMaskRejectsInvalidInput(t *testing.T) {
	if _, err := buildStrokeMask(image.Rect(0, 0, 10, 10), nil, 10); err == nil {
		t.Fatal("empty stroke was accepted")
	}
	if _, err := buildStrokeMask(image.Rect(0, 0, 10, 10), []TouchUpPoint{{X: 5, Y: 5}}, 0); err == nil {
		t.Fatal("zero brush size was accepted")
	}
	if _, err := buildStrokeMask(image.Rect(0, 0, 10, 10), []TouchUpPoint{{X: 50, Y: 50}}, 5); err == nil {
		t.Fatal("off-image stroke was accepted")
	}
}

func TestEncodeTouchUpPreviewPatchUsesTightTransparentBounds(t *testing.T) {
	out := image.NewNRGBA(image.Rect(0, 0, 40, 30))
	mask := image.NewAlpha(image.Rect(10, 8, 20, 18))
	mask.SetAlpha(12, 11, color.Alpha{A: 255})
	mask.SetAlpha(15, 14, color.Alpha{A: 128})
	out.SetNRGBA(12, 11, color.NRGBA{R: 10, G: 20, B: 30, A: 255})
	out.SetNRGBA(15, 14, color.NRGBA{R: 80, G: 90, B: 100, A: 128})

	patch, err := encodeTouchUpPreviewPatch(out, mask)
	if err != nil {
		t.Fatal(err)
	}
	if patch.X != 12 || patch.Y != 11 || patch.Width != 4 || patch.Height != 4 {
		t.Fatalf("patch geometry = (%d,%d %dx%d), want (12,11 4x4)", patch.X, patch.Y, patch.Width, patch.Height)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(patch.Source, "data:image/png;base64,"))
	if err != nil {
		t.Fatal(err)
	}
	decoded, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if got := color.NRGBAModel.Convert(decoded.At(0, 0)).(color.NRGBA); got != (color.NRGBA{R: 10, G: 20, B: 30, A: 255}) {
		t.Fatalf("first replacement pixel = %v", got)
	}
	if got := color.NRGBAModel.Convert(decoded.At(3, 3)).(color.NRGBA); got != (color.NRGBA{R: 80, G: 90, B: 100, A: 255}) {
		t.Fatalf("soft-mask replacement was blended twice: %v", got)
	}
	if got := color.NRGBAModel.Convert(decoded.At(1, 1)).(color.NRGBA); got.A != 0 {
		t.Fatalf("unchanged patch pixel is not transparent: %v", got)
	}
}

func BenchmarkBuildStrokeMask(b *testing.B) {
	points := make([]TouchUpPoint, 0, 168)
	for x := 500.0; x <= 1000; x += 3 {
		points = append(points, TouchUpPoint{X: x, Y: 750 + float64(int(x)%31)})
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := buildStrokeMask(image.Rect(0, 0, 6000, 4000), points, 40); err != nil {
			b.Fatal(err)
		}
	}
}
