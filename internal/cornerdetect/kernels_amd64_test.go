//go:build amd64

package cornerdetect

import (
	"context"
	"fmt"
	"image"
	"math"
	"reflect"
	"testing"
)

func TestCornerSobelAVX2MatchesScalar(t *testing.T) {
	if !cornerUseAVX2 {
		t.Skip("AVX2 is unavailable on this CPU")
	}
	const n = 67 // exercises four full vectors plus the scalar tail
	top := make([]uint8, n+2)
	middle := make([]uint8, n+2)
	bottom := make([]uint8, n+2)
	for i := range top {
		top[i] = uint8((i*37 + i*i*3) & 255)
		middle[i] = uint8((i*71 + 19) & 255)
		bottom[i] = uint8((i*11 + i*i*5 + 97) & 255)
	}
	wantX, wantY := make([]int16, n), make([]int16, n)
	gotX, gotY := make([]int16, n), make([]int16, n)
	cornerSobelScalar(top, middle, bottom, wantX, wantY, 0)
	cornerSobelRow(top, middle, bottom, gotX, gotY)
	if !reflect.DeepEqual(gotX, wantX) || !reflect.DeepEqual(gotY, wantY) {
		t.Fatal("AVX2 Sobel output differs from scalar output")
	}
}

func TestCornerBlurAVX2MatchesScalar(t *testing.T) {
	if !cornerUseAVX2 {
		t.Skip("AVX2 is unavailable on this CPU")
	}
	const n = 67
	a, middle, c := make([]uint8, n), make([]uint8, n), make([]uint8, n)
	for i := range a {
		a[i] = uint8(i*37 + i*i*3)
		middle[i] = uint8(i*71 + 19)
		c[i] = uint8(i*11 + i*i*5 + 97)
	}
	want, got := make([]uint8, n), make([]uint8, n)
	original := cornerUseAVX2
	cornerUseAVX2 = false
	cornerBlurRow(a, middle, c, want)
	cornerUseAVX2 = original
	cornerBlurRow(a, middle, c, got)
	if !reflect.DeepEqual(got, want) {
		t.Fatal("AVX2 Gaussian blur output differs from scalar output")
	}
}

func TestCornerResizeGrayAVX2MatchesScalar(t *testing.T) {
	if !cornerUseAVX2 {
		t.Skip("AVX2 is unavailable on this CPU")
	}
	for _, factor := range []int{2, 4} {
		const dstWidth = 67
		srcStride := dstWidth*factor + 11
		src := make([]uint8, srcStride*factor)
		for i := range src {
			src[i] = uint8(i*37 + i*i*3 + factor*19)
		}
		want, got := make([]uint8, dstWidth), make([]uint8, dstWidth)
		original := cornerUseAVX2
		cornerUseAVX2 = false
		cornerResizeGrayRow(src, srcStride, factor, want)
		cornerUseAVX2 = original
		cornerResizeGrayRow(src, srcStride, factor, got)
		if !reflect.DeepEqual(got, want) {
			t.Logf("factor %d got=%v want=%v", factor, got[:min(20, len(got))], want[:min(20, len(want))])
			for i := range got {
				if got[i] != want[i] {
					t.Fatalf("factor %d lane %d: AVX2=%d scalar=%d", factor, i, got[i], want[i])
				}
			}
		}
	}
}

func TestCornerEigenAVX2MatchesScalar(t *testing.T) {
	if !cornerUseAVX2 {
		t.Skip("AVX2 is unavailable on this CPU")
	}
	streams := makeCornerEigenTestStreams(67)
	want := make([]float64, 67)
	got := make([]float64, 67)

	original := cornerUseAVX2
	cornerUseAVX2 = false
	wantMax := runCornerEigenTestStreams(streams, want)
	cornerUseAVX2 = original
	gotMax := runCornerEigenTestStreams(streams, got)

	if math.Float64bits(gotMax) != math.Float64bits(wantMax) {
		t.Fatalf("AVX2 max=%g, scalar max=%g", gotMax, wantMax)
	}
	for i := range got {
		if math.Float64bits(got[i]) != math.Float64bits(want[i]) {
			t.Fatalf("pixel %d: AVX2=%g, scalar=%g", i, got[i], want[i])
		}
	}
}

func TestGoodFeaturesToTrackAVX2MatchesScalar(t *testing.T) {
	if !cornerUseAVX2 {
		t.Skip("AVX2 is unavailable on this CPU")
	}
	gray := image.NewGray(image.Rect(0, 0, 257, 193))
	for y := 0; y < 193; y++ {
		for x := 0; x < 257; x++ {
			gray.Pix[y*gray.Stride+x] = uint8((x*13 + y*29 + (x*y)%251) & 255)
		}
	}
	original := cornerUseAVX2
	cornerUseAVX2 = false
	want, wantErr := Detect(context.Background(), gray, Options{MaxCorners: 100, QualityLevel: 0.01, MinDistance: 8, BlockSize: 7})
	cornerUseAVX2 = original
	got, gotErr := Detect(context.Background(), gray, Options{MaxCorners: 100, QualityLevel: 0.01, MinDistance: 8, BlockSize: 7})
	if wantErr != nil || gotErr != nil {
		t.Fatalf("scalar error=%v, AVX2 error=%v", wantErr, gotErr)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("AVX2 corners differ from scalar corners: got %d, want %d", len(got), len(want))
	}
}

func BenchmarkCornerSobelAMD64(b *testing.B) {
	if !cornerUseAVX2 {
		b.Skip("AVX2 is unavailable on this CPU")
	}
	const n = 1498
	top := make([]uint8, n+2)
	middle := make([]uint8, n+2)
	bottom := make([]uint8, n+2)
	for i := range top {
		top[i], middle[i], bottom[i] = uint8(i*37), uint8(i*71+19), uint8(i*11+97)
	}
	ix, iy := make([]int16, n), make([]int16, n)
	benchmarkCornerModes(b, func() { cornerSobelRow(top, middle, bottom, ix, iy) })
}

func BenchmarkCornerBlurAMD64(b *testing.B) {
	if !cornerUseAVX2 {
		b.Skip("AVX2 is unavailable on this CPU")
	}
	const n = 1500
	a, middle, c := make([]uint8, n), make([]uint8, n), make([]uint8, n)
	dst := make([]uint8, n)
	for i := range a {
		a[i], middle[i], c[i] = uint8(i*37), uint8(i*71+19), uint8(i*11+97)
	}
	benchmarkCornerModes(b, func() { cornerBlurRow(a, middle, c, dst) })
}

func BenchmarkCornerEigenAMD64(b *testing.B) {
	if !cornerUseAVX2 {
		b.Skip("AVX2 is unavailable on this CPU")
	}
	streams := makeCornerEigenTestStreams(1494)
	dst := make([]float64, 1494)
	benchmarkCornerModes(b, func() { cornerEigenBenchmarkSink = runCornerEigenTestStreams(streams, dst) })
}

func BenchmarkResizeGrayFixedFactorsAMD64(b *testing.B) {
	if !cornerUseAVX2 {
		b.Skip("AVX2 is unavailable on this CPU")
	}
	for _, factor := range []int{2, 4} {
		src := image.NewGray(image.Rect(0, 0, 1504, 1000))
		for i := range src.Pix {
			src.Pix[i] = uint8(i*37 + i/17)
		}
		b.Run(fmt.Sprintf("%dx/scalar", factor), func(b *testing.B) {
			original := cornerUseAVX2
			defer func() { cornerUseAVX2 = original }()
			cornerUseAVX2 = false
			for i := 0; i < b.N; i++ {
				cornerResizeBenchmarkSink = ResizeGray(src, 1504/factor, 1000/factor)
			}
		})
		b.Run(fmt.Sprintf("%dx/avx2", factor), func(b *testing.B) {
			original := cornerUseAVX2
			defer func() { cornerUseAVX2 = original }()
			cornerUseAVX2 = true
			for i := 0; i < b.N; i++ {
				cornerResizeBenchmarkSink = ResizeGray(src, 1504/factor, 1000/factor)
			}
		})
	}
}

func BenchmarkGoodFeaturesToTrackAMD64(b *testing.B) {
	if !cornerUseAVX2 {
		b.Skip("AVX2 is unavailable on this CPU")
	}
	gray := image.NewGray(image.Rect(0, 0, 1000, 667))
	for y := 0; y < 667; y++ {
		for x := 0; x < 1000; x++ {
			value := uint8(35 + (x+y)%12)
			if x%125 > 18 && x%125 < 107 && y%95 > 14 && y%95 < 81 {
				value = uint8(185 + (x*3+y*5)%45)
			}
			gray.Pix[y*gray.Stride+x] = value
		}
	}

	original := cornerUseAVX2
	defer func() { cornerUseAVX2 = original }()
	for _, benchmark := range []struct {
		name string
		simd bool
	}{{"scalar", false}, {"avx2", true}} {
		b.Run(benchmark.name, func(b *testing.B) {
			cornerUseAVX2 = benchmark.simd
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				points, err := Detect(context.Background(), gray, Options{MaxCorners: 500, QualityLevel: 0.01, MinDistance: 15, BlockSize: 7})
				if err != nil {
					b.Fatal(err)
				}
				cornerPointBenchmarkSink = len(points)
			}
		})
	}
}

func benchmarkCornerModes(b *testing.B, fn func()) {
	original := cornerUseAVX2
	defer func() { cornerUseAVX2 = original }()
	for _, benchmark := range []struct {
		name string
		simd bool
	}{{"scalar", false}, {"avx2", true}} {
		b.Run(benchmark.name, func(b *testing.B) {
			cornerUseAVX2 = benchmark.simd
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				fn()
			}
		})
	}
}

type cornerEigenTestStreams [12][]float64

func makeCornerEigenTestStreams(n int) cornerEigenTestStreams {
	var streams cornerEigenTestStreams
	for i := range streams {
		streams[i] = make([]float64, n)
	}
	for i := 0; i < n; i++ {
		// Only the lower-right stream need be populated to produce the desired
		// positive-semidefinite tensor; the other SAT terms remain zero.
		streams[0][i] = float64(40000 + i*101)
		streams[4][i] = float64(25000 + i*73)
		streams[8][i] = float64((i%97 - 48) * 100)
	}
	return streams
}

func runCornerEigenTestStreams(s cornerEigenTestStreams, dst []float64) float64 {
	return cornerEigenRow(
		s[0], s[1], s[2], s[3],
		s[4], s[5], s[6], s[7],
		s[8], s[9], s[10], s[11],
		dst,
	)
}

var cornerEigenBenchmarkSink float64
var cornerPointBenchmarkSink int
var cornerResizeBenchmarkSink *image.Gray
