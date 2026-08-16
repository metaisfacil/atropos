package main

import (
	"image"
	"image/color"
	"math"
	"os"
	"sync"
	"testing"
)

// ---- nextPow2FFT ----

func TestNextPow2FFT_ExactPowers(t *testing.T) {
	for _, v := range []int{1, 2, 4, 8, 16, 64, 128, 1024} {
		if got := nextPow2FFT(v); got != v {
			t.Fatalf("nextPow2FFT(%d) = %d, want %d", v, got, v)
		}
	}
}

func TestNextPow2FFT_RoundsUp(t *testing.T) {
	cases := [][2]int{{3, 4}, {5, 8}, {6, 8}, {7, 8}, {9, 16}, {100, 128}, {300, 512}}
	for _, c := range cases {
		if got := nextPow2FFT(c[0]); got != c[1] {
			t.Fatalf("nextPow2FFT(%d) = %d, want %d", c[0], got, c[1])
		}
	}
}

// ---- pFor ----

func TestPFor_CoversWholeRange(t *testing.T) {
	const total = 100
	covered := make([]int, total)
	var mu sync.Mutex
	pFor(total, 8, func(s, e int) {
		mu.Lock()
		for i := s; i < e; i++ {
			covered[i]++
		}
		mu.Unlock()
	})
	for i, v := range covered {
		if v != 1 {
			t.Fatalf("index %d covered %d times (want 1)", i, v)
		}
	}
}

func TestPFor_SingleWorker(t *testing.T) {
	sum := 0
	pFor(10, 1, func(s, e int) { sum += e - s })
	if sum != 10 {
		t.Fatalf("expected sum=10, got %d", sum)
	}
}

func TestPFor_MoreWorkersThanTotal(t *testing.T) {
	covered := make([]int, 3)
	var mu sync.Mutex
	pFor(3, 100, func(s, e int) {
		mu.Lock()
		for i := s; i < e; i++ {
			covered[i]++
		}
		mu.Unlock()
	})
	for i, v := range covered {
		if v != 1 {
			t.Fatalf("index %d covered %d times (want 1)", i, v)
		}
	}
}

// ---- fft1d ----

func TestFFT1d_RoundTrip(t *testing.T) {
	// FFT followed by IFFT should recover the original values within tolerance.
	n := 8
	orig := []complex128{1, 2, 3, 4, 5, 6, 7, 8}
	x := make([]complex128, n)
	copy(x, orig)

	fft1d(x, false)
	fft1d(x, true)

	for i, v := range orig {
		diff := math.Abs(real(x[i])-real(v)) + math.Abs(imag(x[i])-imag(v))
		if diff > 1e-9 {
			t.Fatalf("index %d: got %v, want %v (diff %.2e)", i, x[i], v, diff)
		}
	}
}

func TestFFT1d_DCSpike(t *testing.T) {
	// Forward FFT of [N, 0, 0, ...] should give all bins equal magnitude N.
	n := 8
	x := make([]complex128, n)
	x[0] = complex(float64(n), 0) // DC spike
	fft1d(x, false)
	for i, v := range x {
		mag := math.Sqrt(real(v)*real(v) + imag(v)*imag(v))
		if math.Abs(mag-float64(n)) > 1e-9 {
			t.Fatalf("bin %d: magnitude %.6f, want %d", i, mag, n)
		}
	}
}

func TestFFT1d_Length1IsNoop(t *testing.T) {
	x := []complex128{complex(42, 7)}
	fft1d(x, false)
	if real(x[0]) != 42 || imag(x[0]) != 7 {
		t.Fatalf("length-1 FFT should be identity, got %v", x[0])
	}
}

// ---- fftShift2d ----

func TestFFTShift2d_SelfInverse(t *testing.T) {
	// Two shifts on an even-sized array should return original.
	rows, cols := 4, 4
	data := make([]complex128, rows*cols)
	for i := range data {
		data[i] = complex(float64(i), 0)
	}
	orig := make([]complex128, len(data))
	copy(orig, data)

	fftShift2d(data, rows, cols)
	fftShift2d(data, rows, cols)

	for i, v := range orig {
		if data[i] != v {
			t.Fatalf("index %d: got %v, want %v", i, data[i], v)
		}
	}
}

func TestFFTShift2d_DCMovesToCenter(t *testing.T) {
	// A spike at (0,0) should move to (rows/2, cols/2) after shift.
	rows, cols := 4, 4
	data := make([]complex128, rows*cols)
	data[0] = complex(1, 0) // top-left corner (DC)

	fftShift2d(data, rows, cols)

	center := (rows/2)*cols + cols/2
	if real(data[center]) != 1 {
		t.Fatalf("DC should move to center index %d, got %v", center, data[center])
	}
	// Original position should now be 0.
	if real(data[0]) != 0 {
		t.Fatalf("original corner should be 0 after shift, got %v", data[0])
	}
}

// ---- applyDescreen ----

func TestApplyDescreen_PreservesDimensions(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 64, 48))
	for y := 0; y < 48; y++ {
		for x := 0; x < 64; x++ {
			src.SetNRGBA(x, y, color.NRGBA{R: 128, G: 100, B: 80, A: 255})
		}
	}
	dst := applyDescreen(src, 92, 6, 4, 100, nil)
	if dst.Bounds().Dx() != 64 || dst.Bounds().Dy() != 48 {
		t.Fatalf("expected 64×48, got %d×%d", dst.Bounds().Dx(), dst.Bounds().Dy())
	}
}

func TestApplyDescreen_PreservesAlpha(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			src.SetNRGBA(x, y, color.NRGBA{R: 200, G: 150, B: 100, A: 200})
		}
	}
	dst := applyDescreen(src, 92, 6, 4, 100, nil)
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			if a := dst.NRGBAAt(x, y).A; a != 200 {
				t.Fatalf("pixel (%d,%d): alpha %d, want 200", x, y, a)
			}
		}
	}
}

func TestApplyDescreen_UniformImagePreserved(t *testing.T) {
	// A uniform image has no frequency peaks to suppress; output should be ≈ input.
	src := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			src.SetNRGBA(x, y, color.NRGBA{R: 180, G: 120, B: 60, A: 255})
		}
	}
	dst := applyDescreen(src, 92, 6, 4, 100, nil)
	// Allow ±2 rounding tolerance from FFT round-trip.
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			c := dst.NRGBAAt(x, y)
			if math.Abs(float64(c.R)-180) > 2 ||
				math.Abs(float64(c.G)-120) > 2 ||
				math.Abs(float64(c.B)-60) > 2 {
				t.Fatalf("pixel (%d,%d) = %v, expected ≈(180,120,60)", x, y, c)
			}
		}
	}
}

func TestApplyDescreenLuminance_UniformImagePreservesColorAndAlpha(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			src.SetNRGBA(x, y, color.NRGBA{R: 180, G: 120, B: 60, A: 173})
		}
	}

	dst := applyDescreenLuminance(src, 92, 6, 4, 100, nil)
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			got := dst.NRGBAAt(x, y)
			if math.Abs(float64(got.R)-180) > 2 ||
				math.Abs(float64(got.G)-120) > 2 ||
				math.Abs(float64(got.B)-60) > 2 {
				t.Fatalf("pixel (%d,%d) = %v, expected approximately (180,120,60)", x, y, got)
			}
			if got.A != 173 {
				t.Fatalf("pixel (%d,%d) alpha = %d, want 173", x, y, got.A)
			}
		}
	}
}

func TestApplyDescreenLuminance_PreservesChromaDifferences(t *testing.T) {
	const size = 32
	src := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			dot := 40
			if (x/2+y/2)%2 == 0 {
				dot = -40
			}
			src.SetNRGBA(x, y, color.NRGBA{
				R: uint8(130 + dot),
				G: uint8(110 + dot),
				B: uint8(90 + dot),
				A: 211,
			})
		}
	}

	plan, err := newDescreenFFTPlan32(size, size)
	if err != nil {
		t.Fatal(err)
	}
	spectrum := plan.forwardLuminance(src, 4)
	if _, peaks := plan.buildDescreenThresholdMask(spectrum, 92, 4, 4); peaks == 0 {
		t.Fatal("test pattern produced no luminance peaks")
	}

	dst := applyDescreenLuminance(src, 92, 2, 4, 0, nil)
	changed := false
	for i := 0; i < len(src.Pix); i += 4 {
		if dst.Pix[i] != src.Pix[i] {
			changed = true
		}
		if int(dst.Pix[i])-int(dst.Pix[i+1]) != 20 ||
			int(dst.Pix[i+1])-int(dst.Pix[i+2]) != 20 {
			t.Fatalf("pixel byte %d changed chroma differences: got RGB (%d,%d,%d)",
				i, dst.Pix[i], dst.Pix[i+1], dst.Pix[i+2])
		}
		if dst.Pix[i+3] != 211 {
			t.Fatalf("pixel byte %d alpha = %d, want 211", i, dst.Pix[i+3])
		}
	}
	if !changed {
		t.Fatal("luminance descreen did not alter the screen pattern")
	}
}

func TestDescreenFFT32TwoDimensionalRoundTrip(t *testing.T) {
	const width, height = 30, 18
	src := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			src.SetNRGBA(x, y, color.NRGBA{
				R: uint8((x*17 + y*11 + 31) & 255),
				G: uint8((x*7 + y*23 + 19) & 255),
				B: uint8((x*29 + y*3 + 5) & 255),
				A: 255,
			})
		}
	}
	plan, err := newDescreenFFTPlan32(nextSmoothFFT(height), nextSmoothEvenFFT(width))
	if err != nil {
		t.Fatal(err)
	}
	dst := image.NewNRGBA(src.Bounds())
	for channel := 0; channel < 3; channel++ {
		spectrum := plan.forwardChannel(src, channel, 4)
		plan.inverseChannel(spectrum, dst, channel, 4)
	}
	for i := 0; i < len(src.Pix); i += 4 {
		for channel := 0; channel < 3; channel++ {
			delta := int(dst.Pix[i+channel]) - int(src.Pix[i+channel])
			if delta < -1 || delta > 1 {
				t.Fatalf("pixel byte %d channel %d: got %d want %d", i, channel, dst.Pix[i+channel], src.Pix[i+channel])
			}
		}
	}
}

func TestApplyDescreen32CloseToLegacyAtSamePadding(t *testing.T) {
	const size = 32
	src := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			dot := uint8(35)
			if (x/2+y/2)%2 == 0 {
				dot = 220
			}
			src.SetNRGBA(x, y, color.NRGBA{R: dot, G: uint8((int(dot) + x*3) & 255), B: uint8((int(dot) + y*5) & 255), A: 255})
		}
	}
	plan, err := newDescreenFFTPlan32(size, size)
	if err != nil {
		t.Fatal(err)
	}
	spectrum := plan.forwardChannel(src, 0, 4)
	_, peaks := plan.buildDescreenThresholdMask(spectrum, 92, 4, 4)
	if peaks == 0 {
		t.Fatal("test pattern produced no peaks, so it does not exercise frequency-mask mapping")
	}
	got := applyDescreen(src, 92, 2, 4, 68, nil)
	want := applyDescreenLegacy(src, 92, 2, 4, 68, nil)
	maxDelta := 0
	for i := 0; i < len(got.Pix); i += 4 {
		for channel := 0; channel < 3; channel++ {
			delta := int(got.Pix[i+channel]) - int(want.Pix[i+channel])
			if delta < 0 {
				delta = -delta
			}
			if delta > maxDelta {
				maxDelta = delta
			}
		}
	}
	if maxDelta > 2 {
		t.Fatalf("pure-Go real FFT differs from legacy output by up to %d levels", maxDelta)
	}
}

func TestDilate2dFFTSlidingWindowMatchesNaive(t *testing.T) {
	const rows, cols = 17, 23
	src := make([]float32, rows*cols)
	for i := range src {
		src[i] = float32((i*37 + i/7) % 256)
	}
	for _, radius := range []int{0, 1, 2, 6, 30} {
		got := dilate2dFFT(src, rows, cols, radius, radius, 4)
		want := naiveDilate2dFFT(src, rows, cols, radius, radius)
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("radius %d index %d: got %g want %g", radius, i, got[i], want[i])
			}
		}
	}
}

func TestDilateBinaryMaskFFTMatchesNaive(t *testing.T) {
	const rows, cols = 17, 23
	src := make([]float32, rows*cols)
	for i := range src {
		if (i*37+i/7)%19 == 0 {
			src[i] = 255
		}
	}
	for _, radius := range []int{0, 1, 2, 6, 30} {
		got := append([]float32(nil), src...)
		scratch := make([]float32, len(got))
		dilateBinaryMaskFFTInPlace(got, scratch, rows, cols, radius, radius, 4)
		want := naiveDilate2dFFT(src, rows, cols, radius, radius)
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("radius %d index %d: got %g want %g", radius, i, got[i], want[i])
			}
		}
	}
}

func naiveDilate2dFFT(src []float32, rows, cols, kw, kh int) []float32 {
	out := make([]float32, rows*cols)
	for y := 0; y < rows; y++ {
		for x := 0; x < cols; x++ {
			var maximum float32
			for dy := -kh; dy <= kh; dy++ {
				ny := y + dy
				if ny < 0 {
					ny = 0
				} else if ny >= rows {
					ny = rows - 1
				}
				for dx := -kw; dx <= kw; dx++ {
					nx := x + dx
					if nx < 0 {
						nx = 0
					} else if nx >= cols {
						nx = cols - 1
					}
					if value := src[ny*cols+nx]; value > maximum {
						maximum = value
					}
				}
			}
			out[y*cols+x] = maximum
		}
	}
	return out
}

func BenchmarkApplyDescreen5100x7020(b *testing.B) {
	if os.Getenv("ATROPOS_BENCH_FULL") == "" {
		b.Skip("set ATROPOS_BENCH_FULL=1 to run the full-scan benchmark")
	}
	const width, height = 5100, 7020
	src := image.NewNRGBA(image.Rect(0, 0, width, height))
	for i := 0; i < len(src.Pix); i += 4 {
		pixel := i / 4
		src.Pix[i] = uint8((pixel*17 + pixel/width*13) & 255)
		src.Pix[i+1] = uint8((pixel*7 + pixel/width*29) & 255)
		src.Pix[i+2] = uint8((pixel*23 + pixel/width*5) & 255)
		src.Pix[i+3] = 255
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = applyDescreen(src, 92, 6, 4, 68, b.Logf)
	}
}
