package main

import (
	"image"
	"image/color"
	"testing"
)

func TestDustLevelIndex(t *testing.T) {
	for level, want := range map[string]int{"low": 0, "medium": 1, "high": 2} {
		got, err := dustLevelIndex(level)
		if err != nil || got != want {
			t.Fatalf("dustLevelIndex(%q) = %d, %v; want %d, nil", level, got, err, want)
		}
	}
	if _, err := dustLevelIndex("maximum"); err == nil {
		t.Fatal("dustLevelIndex accepted an unknown level")
	}
}

func TestDustRemovalLeavesUniformImageUnchanged(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: 120, G: 120, B: 120, A: 255})
		}
	}
	out, repaired, usedDPI, err := applyDustRemoval(img, "medium", 0)
	if err != nil {
		t.Fatal(err)
	}
	if out != img || repaired != 0 {
		t.Fatalf("uniform image changed: outSame=%v repaired=%d", out == img, repaired)
	}
	if usedDPI != 300 {
		t.Fatalf("default DPI = %v; want 300", usedDPI)
	}
}

func TestDustRemovalRepairsIsolatedBrightDefect(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: 90, G: 105, B: 120, A: 255})
		}
	}
	img.SetNRGBA(32, 32, color.NRGBA{R: 255, G: 255, B: 255, A: 255})

	out, repaired, _, err := applyDustRemoval(img, "low", 400)
	if err != nil {
		t.Fatal(err)
	}
	if repaired == 0 {
		t.Fatal("isolated defect was not repaired")
	}
	if got, want := out.NRGBAAt(32, 32), (color.NRGBA{R: 90, G: 105, B: 120, A: 255}); got != want {
		t.Fatalf("repaired defect = %#v; want %#v", got, want)
	}
}

func TestFilterDustComponentsUsesLevelAreaBanks(t *testing.T) {
	const width, height = 40, 20
	makeComponent := func() []uint8 {
		mask := make([]uint8, width*height)
		// Dense 17x10 component: area 170. It exceeds Low's dense cap of
		// 160 but remains below Medium's cap of 240 at the 400-DPI baseline.
		for y := 3; y < 13; y++ {
			for x := 4; x < 21; x++ {
				mask[y*width+x] = 1
			}
		}
		return mask
	}

	lowMask := makeComponent()
	lowAccepted := make([]uint8, len(lowMask))
	filterDustComponents(lowMask, lowAccepted, width, height, reflectiveDustProfile, reflectiveDustProfile.limits[0])
	if got := countBinaryMask(lowAccepted); got != 0 {
		t.Fatalf("Low accepted %d pixels; want 0", got)
	}

	mediumMask := makeComponent()
	mediumAccepted := make([]uint8, len(mediumMask))
	filterDustComponents(mediumMask, mediumAccepted, width, height, reflectiveDustProfile, reflectiveDustProfile.limits[1])
	if got := countBinaryMask(mediumAccepted); got != 170 {
		t.Fatalf("Medium accepted %d pixels; want 170", got)
	}
}

func TestFillBinaryMaskHoles(t *testing.T) {
	const width, height = 7, 7
	mask := make([]uint8, width*height)
	for x := 2; x <= 4; x++ {
		mask[2*width+x] = 1
		mask[4*width+x] = 1
	}
	mask[3*width+2] = 1
	mask[3*width+4] = 1
	fillBinaryMaskHoles(mask, make([]uint8, len(mask)), width, height)
	if mask[3*width+3] != 1 {
		t.Fatal("enclosed mask hole was not filled")
	}
	if mask[0] != 0 {
		t.Fatal("exterior background was filled")
	}
}

func TestMedianRepairUsesUnmaskedLocalSamples(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 9, 9))
	for y := 0; y < 9; y++ {
		for x := 0; x < 9; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: 80, G: 100, B: 120, A: 255})
		}
	}
	img.SetNRGBA(4, 4, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
	mask := make([]uint8, 9*9)
	mask[4*9+4] = 1
	out, repaired := medianRepair(img, mask)
	if repaired == 0 {
		t.Fatal("median repair made no changes")
	}
	got := out.NRGBAAt(4, 4)
	want := (color.NRGBA{R: 80, G: 100, B: 120, A: 255})
	if got != want {
		t.Fatalf("repaired centre = %#v; want %#v", got, want)
	}
}
