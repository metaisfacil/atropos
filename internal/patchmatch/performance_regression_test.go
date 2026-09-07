package patchmatch

import (
	"context"
	"crypto/sha256"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"

	_ "golang.org/x/image/tiff"
)

// These fixtures use the 43px brush, 15px patch and five search passes that
// scanned-print retouching actually runs with. Benchmarks built around a 7px
// patch understate what a real stroke costs.
func pmBrushPerformanceFixture(long bool) (*image.NRGBA, *image.Alpha) {
	const w, h = 640, 640
	src := image.NewNRGBA(image.Rect(0, 0, w, h))
	mask := image.NewAlpha(src.Bounds())
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			grain := int(pmHash(uint32(x), uint32(y), 41)%29) - 14
			v := 90 + grain + int(25*math.Sin(float64(x+2*y)/80))
			if y < 250+x/5 {
				v += 70
			}
			src.SetNRGBA(x, y, color.NRGBA{R: byte(v + 10), G: byte(v), B: byte(v - 8), A: 255})
		}
	}
	steps := 1
	if long {
		steps = 65
	}
	for step := 0; step < steps; step++ {
		xc, yc := 320.0, 320.0
		if long {
			t := float64(step) / float64(steps-1)
			xc = 280 + 80*t
			yc = 255 + 134*t
		}
		for y := int(yc) - 23; y <= int(yc)+23; y++ {
			for x := int(xc) - 23; x <= int(xc)+23; x++ {
				dx, dy := float64(x)-xc, float64(y)-yc
				a := clampInt(int((22.5-math.Sqrt(dx*dx+dy*dy))*255), 0, 255)
				if a > int(mask.Pix[y*mask.Stride+x]) {
					mask.Pix[y*mask.Stride+x] = byte(a)
				}
			}
		}
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if mask.Pix[y*mask.Stride+x] != 0 {
				src.SetNRGBA(x, y, color.NRGBA{R: 235, G: 230, B: 220, A: 255})
			}
		}
	}
	return src, mask
}

// Set ATROPOS_PM_SCAN to replay representative masks on a local scan.
func pmLoggedScanFixtures(t testing.TB) map[string]struct {
	src  *image.NRGBA
	mask *image.Alpha
} {
	path := os.Getenv("ATROPOS_PM_SCAN")
	if path == "" {
		t.Skip("set ATROPOS_PM_SCAN for local scan replay")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	scan, _, err := image.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	out := make(map[string]struct {
		src  *image.NRGBA
		mask *image.Alpha
	})
	for _, name := range []string{"dab", "stroke"} {
		origin := image.Pt(1375, 2785)
		if name == "stroke" {
			origin = image.Pt(1163, 2764)
		}
		crop := image.Rectangle{Min: origin, Max: origin.Add(image.Pt(640, 640))}
		if !crop.In(scan.Bounds()) {
			t.Fatalf("scan %v does not contain logged region %v", scan.Bounds(), crop)
		}
		src := image.NewNRGBA(image.Rect(0, 0, 640, 640))
		draw.Draw(src, src.Bounds(), scan, origin, draw.Src)
		_, mask := pmBrushPerformanceFixture(name == "stroke")
		out[name] = struct {
			src  *image.NRGBA
			mask *image.Alpha
		}{src, mask}
	}
	return out
}

func BenchmarkPatchMatchLoggedScan(b *testing.B) {
	fixtures := pmLoggedScanFixtures(b)
	for _, name := range []string{"dab", "stroke"} {
		fixture := fixtures[name]
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, _, err := FillROI(context.Background(), fixture.src, fixture.mask, image.Rectangle{}, 15, 5); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestPatchMatchLoggedScanReplay(t *testing.T) {
	for name, fixture := range pmLoggedScanFixtures(t) {
		out, err := Fill(context.Background(), fixture.src, fixture.mask, 15, 5)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("%s output SHA256 %x", name, sha256.Sum256(out.Pix))
		if directory := os.Getenv("ATROPOS_PM_OUTPUT"); directory != "" {
			if err := os.MkdirAll(directory, 0755); err != nil {
				t.Fatal(err)
			}
			f, err := os.Create(filepath.Join(directory, name+".png"))
			if err != nil {
				t.Fatal(err)
			}
			err = png.Encode(f, out)
			closeErr := f.Close()
			if err != nil {
				t.Fatal(err)
			}
			if closeErr != nil {
				t.Fatal(closeErr)
			}
		}
	}
}

func BenchmarkPatchMatchBrush43(b *testing.B) {
	for _, name := range []string{"dab", "stroke"} {
		b.Run(name, func(b *testing.B) {
			src, mask := pmBrushPerformanceFixture(name == "stroke")
			bounds := maskBounds(mask)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, _, err := FillROI(context.Background(), src, mask, bounds, 15, 5); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
