package main

import (
	"image"
	"image/color"
	"math"
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
	dst := applyDescreen(src, 92, 6, 4)
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
	dst := applyDescreen(src, 92, 6, 4)
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
	dst := applyDescreen(src, 92, 6, 4)
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
