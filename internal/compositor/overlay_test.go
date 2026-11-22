package compositor

import (
	"image"
	"image/color"
	"math/rand"
	"testing"

	"atropos/internal/imageops"
	"atropos/internal/raster"
)

func TestOverlayQuarterTurnAndMask(t *testing.T) {
	base := image.NewNRGBA(image.Rect(0, 0, 320, 240))
	rng := rand.New(rand.NewSource(17))
	for y := 0; y < 240; y++ {
		for x := 0; x < 320; x++ {
			v := uint8(rng.Intn(256))
			base.SetNRGBA(x, y, color.NRGBA{R: v, G: v, B: v, A: 255})
		}
	}
	covered := raster.CloneNRGBA(base)
	for y := 75; y < 135; y++ {
		for x := 115; x < 180; x++ {
			covered.SetNRGBA(x, y, color.NRGBA{R: 20, G: 20, B: 255, A: 255})
		}
	}
	covered.SetNRGBA(20, 20, color.NRGBA{R: 255, G: 0, B: 0, A: 255})
	rotated := imageops.Rotate90(covered, 1)
	session, err := AlignOverlay([]*image.NRGBA{base, rotated})
	if err != nil {
		t.Fatal(err)
	}
	w, h := session.Size()
	if w != 320 || h != 240 {
		t.Fatalf("unexpected output size: %dx%d", w, h)
	}
	before := session.Render(0)
	if before.NRGBAAt(145, 105) != base.NRGBAAt(145, 105) {
		t.Fatal("unpainted area should come from reference scan")
	}
	if err := session.AddStroke(MaskStroke{Layer: 1, Radius: .17, Points: []MaskPoint{{X: 145.0 / 320, Y: 105.0 / 240}}}); err != nil {
		t.Fatal(err)
	}
	after := session.Render(0)
	if after.NRGBAAt(145, 105) != covered.NRGBAAt(145, 105) {
		t.Fatal("painted area did not reveal selected layer")
	}
	if after.NRGBAAt(20, 20) != base.NRGBAAt(20, 20) {
		t.Fatal("painting changed an untouched area")
	}
	if !session.UndoStroke() {
		t.Fatal("mask undo failed")
	}
	if session.Render(0).NRGBAAt(145, 105) != base.NRGBAAt(145, 105) {
		t.Fatal("undo did not restore reference scan")
	}
}

func TestOverlayMaskFeather(t *testing.T) {
	s := &OverlaySession{
		images: []*image.NRGBA{
			image.NewNRGBA(image.Rect(0, 0, 100, 100)),
			image.NewNRGBA(image.Rect(0, 0, 100, 100)),
		},
		width: 100, height: 100,
	}
	stroke := MaskStroke{Layer: 1, Radius: .2, Points: []MaskPoint{{X: .5, Y: .5}}}
	probe := 50*100 + 65 // 15.5 px from the brush centre, within its 20 px radius
	if err := s.AddStroke(stroke); err != nil {
		t.Fatal(err)
	}
	if got := s.rasterMasks(100, 100)[1][probe]; got != 255 {
		t.Fatalf("hard brush has opacity %d, want 255", got)
	}
	s.ClearStrokes()
	stroke.Feather = .5
	if err := s.AddStroke(stroke); err != nil {
		t.Fatal(err)
	}
	medium := s.rasterMasks(100, 100)[1][probe]
	if medium == 0 || medium == 255 {
		t.Fatalf("feathered brush has opacity %d, want a blend", medium)
	}
	s.ClearStrokes()
	stroke.Feather = 1
	if err := s.AddStroke(stroke); err != nil {
		t.Fatal(err)
	}
	wide := s.rasterMasks(100, 100)[1][probe]
	if wide >= medium {
		t.Fatalf("wider feather has opacity %d, want less than %d", wide, medium)
	}
	s.ClearStrokes()
	stroke.Feather = 1.01
	if err := s.AddStroke(stroke); err == nil {
		t.Fatal("accepted feather above 100%")
	}
}
