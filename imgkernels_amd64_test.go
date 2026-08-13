//go:build amd64

package main

import (
	"image"
	"image/color"
	"math/rand"
	"reflect"
	"testing"
)

func TestApplyLevelsAVX2MatchesScalar(t *testing.T) {
	if !imgUseAVX2 {
		t.Skip("AVX2 is unavailable on this CPU")
	}
	rng := rand.New(rand.NewSource(42))
	for _, points := range [][2]int{{0, 255}, {17, 231}, {100, 101}, {127, 255}, {0, 128}} {
		pix := make([]uint8, 4*103)
		if _, err := rng.Read(pix); err != nil {
			t.Fatal(err)
		}
		want := append([]uint8(nil), pix...)
		got := append([]uint8(nil), pix...)
		scale := 255.0 / float64(points[1]-points[0])
		original := imgUseAVX2
		imgUseAVX2 = false
		applyLevelsPixels(want, points[0], scale)
		imgUseAVX2 = original
		applyLevelsPixels(got, points[0], scale)
		if !reflect.DeepEqual(got, want) {
			for i := range got {
				if got[i] != want[i] {
					t.Fatalf("points %v byte %d: AVX2=%d scalar=%d", points, i, got[i], want[i])
				}
			}
		}
	}
}

func TestGrayscaleAccentAVX2MatchesScalar(t *testing.T) {
	if !imgUseAVX2 {
		t.Skip("AVX2 is unavailable on this CPU")
	}
	rng := rand.New(rand.NewSource(73))
	for _, accent := range []int{-255, -73, -1, 0, 1, 91, 255} {
		src := make([]uint8, 4*103)
		if _, err := rng.Read(src); err != nil {
			t.Fatal(err)
		}
		want, got := make([]uint8, 103), make([]uint8, 103)
		original := imgUseAVX2
		imgUseAVX2 = false
		grayscaleAccentRow(src, want, accent)
		imgUseAVX2 = original
		grayscaleAccentRow(src, got, accent)
		if !reflect.DeepEqual(got, want) {
			for i := range got {
				if got[i] != want[i] {
					t.Fatalf("accent %d pixel %d: AVX2=%d scalar=%d", accent, i, got[i], want[i])
				}
			}
		}
	}
}

func TestMaskBlendAVX2MatchesScalar(t *testing.T) {
	if !imgUseAVX2 {
		t.Skip("AVX2 is unavailable on this CPU")
	}
	rng := rand.New(rand.NewSource(101))
	src := make([]uint8, 4*103)
	if _, err := rng.Read(src); err != nil {
		t.Fatal(err)
	}
	alpha := make([]float64, 103)
	for i := range alpha {
		switch i % 5 {
		case 0:
			alpha[i] = 0
		case 1:
			alpha[i] = 1
		default:
			alpha[i] = float64((i*37)%1000) / 1000
		}
	}
	want, got := make([]uint8, len(src)), make([]uint8, len(src))
	original := imgUseAVX2
	imgUseAVX2 = false
	maskBlendRow(src, want, alpha, 17, 83, 201)
	imgUseAVX2 = original
	maskBlendRow(src, got, alpha, 17, 83, 201)
	if !reflect.DeepEqual(got, want) {
		t.Logf("got=%v want=%v", got[:16], want[:16])
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("byte %d: AVX2=%d scalar=%d alpha=%g", i, got[i], want[i], alpha[i/4])
			}
		}
	}
}

func BenchmarkApplyLevelsAMD64(b *testing.B) {
	pix := make([]uint8, 6000*4000*4)
	for i := range pix {
		pix[i] = uint8(i * 37)
	}
	original := imgUseAVX2
	defer func() { imgUseAVX2 = original }()
	for _, benchmark := range []struct {
		name string
		simd bool
	}{{"scalar", false}, {"avx2", true}} {
		b.Run(benchmark.name, func(b *testing.B) {
			imgUseAVX2 = benchmark.simd
			for i := 0; i < b.N; i++ {
				applyLevelsPixels(pix, 17, 255.0/214.0)
			}
		})
	}
}

func BenchmarkGrayscaleAccentAMD64(b *testing.B) {
	src := make([]uint8, 3000*2000*4)
	dst := make([]uint8, 3000*2000)
	for i := range src {
		src[i] = uint8(i * 37)
	}
	original := imgUseAVX2
	defer func() { imgUseAVX2 = original }()
	for _, benchmark := range []struct {
		name string
		simd bool
	}{{"scalar", false}, {"avx2", true}} {
		b.Run(benchmark.name, func(b *testing.B) {
			imgUseAVX2 = benchmark.simd
			for i := 0; i < b.N; i++ {
				grayscaleAccentRow(src, dst, 23)
			}
		})
	}
}

func BenchmarkMaskBlendAMD64(b *testing.B) {
	const pixels = 1500 * 1500
	src, dst := make([]uint8, pixels*4), make([]uint8, pixels*4)
	alpha := make([]float64, pixels)
	for i := range src {
		src[i] = uint8(i * 37)
	}
	for i := range alpha {
		alpha[i] = float64(i%1000) / 1000
	}
	original := imgUseAVX2
	defer func() { imgUseAVX2 = original }()
	for _, benchmark := range []struct {
		name string
		simd bool
	}{{"scalar", false}, {"avx2", true}} {
		b.Run(benchmark.name, func(b *testing.B) {
			imgUseAVX2 = benchmark.simd
			for i := 0; i < b.N; i++ {
				maskBlendRow(src, dst, alpha, 17, 83, 201)
			}
		})
	}
}

func BenchmarkOptimizedGeometryAMD64(b *testing.B) {
	src := image.NewNRGBA(image.Rect(0, 0, 1600, 1200))
	for i := range src.Pix {
		src.Pix[i] = uint8(i * 37)
	}
	points := [4]image.Point{{20, 10}, {1570, 30}, {35, 1160}, {1560, 1175}}
	b.Run("perspective", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			geometryBenchmarkSink = perspectiveTransform(src, points, [4]image.Point{{0, 0}, {1499, 0}, {0, 1099}, {1499, 1099}}, 1500, 1100)
		}
	})
	b.Run("rotate", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			geometryBenchmarkSink = rotateArbitrary(src, 3.7, color.NRGBA{R: 20, G: 30, B: 40, A: 255})
		}
	})
	b.Run("disc-mask", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			geometryBenchmarkSink = applyCircularMaskWithFeather(src, image.Pt(800, 600), 550, 25, 75, color.NRGBA{R: 20, G: 30, B: 40, A: 255})
		}
	})
}

var geometryBenchmarkSink *image.NRGBA
