package main

import (
	"context"
	"image"
	"os"
	"testing"

	"atropos/internal/raster"
)

// Opt-in replay through the actual brush rasterizer and full-document fill
// wrapper. ATROPOS_PM_SCAN can point at a lossless PNG conversion when the Go
// TIFF decoder cannot read the original scan. File decoding is outside timing.
func BenchmarkTouchupLoggedScan(b *testing.B) {
	path := os.Getenv("ATROPOS_PM_SCAN")
	if path == "" {
		b.Skip("set ATROPOS_PM_SCAN for full-document replay")
	}
	f, err := os.Open(path)
	if err != nil {
		b.Fatal(err)
	}
	decoded, _, err := image.Decode(f)
	f.Close()
	if err != nil {
		b.Fatal(err)
	}
	src := raster.ToNRGBA(decoded)
	for _, name := range []string{"dab", "stroke"} {
		points := []TouchUpPoint{{X: 1695, Y: 3105}}
		if name == "stroke" {
			points = make([]TouchUpPoint, 65)
			for i := range points {
				t := float64(i) / 64
				points[i] = TouchUpPoint{X: 1443 + 80*t, Y: 3016 + 135*t}
			}
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				mask, err := buildStrokeMask(src.Bounds(), points, 43)
				if err != nil {
					b.Fatal(err)
				}
				if _, err := patchMatchChunkedFill(context.Background(), src, mask, 15, 5); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
