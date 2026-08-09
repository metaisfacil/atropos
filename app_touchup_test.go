package main

import (
	"image"
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
