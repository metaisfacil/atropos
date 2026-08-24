//go:build amd64

package imageops

import (
	"image"
	"image/color"
	"testing"
)

func BenchmarkTransformsAMD64(b *testing.B) {
	src := image.NewNRGBA(image.Rect(0, 0, 1600, 1200))
	for i := range src.Pix {
		src.Pix[i] = uint8(i * 37)
	}
	points := [4]image.Point{{20, 10}, {1570, 30}, {35, 1160}, {1560, 1175}}
	b.Run("perspective", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			transformBenchmarkSink = PerspectiveTransform(src, points, [4]image.Point{{0, 0}, {1499, 0}, {0, 1099}, {1499, 1099}}, 1500, 1100)
		}
	})
	b.Run("rotate", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			transformBenchmarkSink = Rotate(src, 3.7, color.NRGBA{R: 20, G: 30, B: 40, A: 255})
		}
	})
	b.Run("disc-mask", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			transformBenchmarkSink = ApplyCircularMask(src, image.Pt(800, 600), 550, 25, 75, color.NRGBA{R: 20, G: 30, B: 40, A: 255})
		}
	})
}

var transformBenchmarkSink *image.NRGBA
