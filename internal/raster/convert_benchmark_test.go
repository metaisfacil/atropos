package raster

import (
	"image"
	"testing"
)

func BenchmarkToNRGBALarge(b *testing.B) {
	src := image.NewNRGBA(image.Rect(0, 0, 10190, 6455))
	for i := range src.Pix {
		src.Pix[i] = byte(i)
	}
	b.SetBytes(int64(len(src.Pix)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result := ToNRGBA(src)
		if len(result.Pix) != len(src.Pix) {
			b.Fatal("clone has the wrong size")
		}
	}
}
