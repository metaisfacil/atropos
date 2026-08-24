package descreen

import (
	"math"
	"math/cmplx"
	"testing"
)

func TestNextSmoothFFT(t *testing.T) {
	for _, tc := range []struct{ in, want int }{
		{1, 1}, {2, 2}, {7, 8}, {5100, 5120}, {7020, 7200},
	} {
		if got := nextSmoothFFT(tc.in); got != tc.want {
			t.Errorf("nextSmoothFFT(%d)=%d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestFFTPlan32MatchesDFT(t *testing.T) {
	for _, n := range []int{2, 3, 4, 5, 6, 8, 9, 10, 12, 15, 16, 20, 25, 30, 45, 64, 75} {
		plan, err := newFFTPlan32(n)
		if err != nil {
			t.Fatalf("length %d: %v", n, err)
		}
		input := make([]complex64, n)
		for i := range input {
			input[i] = complex(float32((i*17+3)%23)-11, float32((i*7+5)%13)-6)
		}
		want := directDFT32(input, false)
		got := append([]complex64(nil), input...)
		plan.transform(got, make([]complex64, n), false)
		for i := range want {
			if delta := cmplx.Abs(complex128(got[i] - want[i])); delta > 2e-4*float64(n) {
				t.Fatalf("length %d bin %d: got %v want %v delta=%g", n, i, got[i], want[i], delta)
			}
		}

		plan.transform(got, make([]complex64, n), true)
		for i := range input {
			if delta := cmplx.Abs(complex128(got[i] - input[i])); delta > 2e-4 {
				t.Fatalf("length %d inverse sample %d: got %v want %v delta=%g", n, i, got[i], input[i], delta)
			}
		}
	}
}

func TestFFTPlan32RejectsUnsupportedLength(t *testing.T) {
	if _, err := newFFTPlan32(17); err == nil {
		t.Fatal("expected factor-17 length to be rejected")
	}
}

func TestRealFFTPlan32MatchesDFTAndRoundTrips(t *testing.T) {
	for _, n := range []int{4, 6, 8, 10, 12, 16, 20, 30, 50} {
		plan, err := newRealFFTPlan32(n)
		if err != nil {
			t.Fatalf("length %d: %v", n, err)
		}
		input := make([]float32, n)
		complexInput := make([]complex64, n)
		for i := range input {
			input[i] = float32((i*19+7)%31) - 15
			complexInput[i] = complex(input[i], 0)
		}
		want := directDFT32(complexInput, false)
		got := make([]complex64, n/2+1)
		packed := make([]complex64, n/2)
		scratch := make([]complex64, n/2)
		plan.forward(input, got, packed, scratch)
		for i := range got {
			if delta := cmplx.Abs(complex128(got[i] - want[i])); delta > 2e-4*float64(n) {
				t.Fatalf("length %d bin %d: got %v want %v delta=%g", n, i, got[i], want[i], delta)
			}
		}

		output := make([]float32, n)
		plan.inverse(got, output, packed, scratch)
		for i := range input {
			if delta := math.Abs(float64(output[i] - input[i])); delta > 3e-4 {
				t.Fatalf("length %d sample %d: got %g want %g delta=%g", n, i, output[i], input[i], delta)
			}
		}
	}
}

func directDFT32(input []complex64, inverse bool) []complex64 {
	n := len(input)
	out := make([]complex64, n)
	sign := -1.0
	if inverse {
		sign = 1
	}
	for k := 0; k < n; k++ {
		var sum complex128
		for j, value := range input {
			angle := sign * 2 * math.Pi * float64(j*k) / float64(n)
			sum += complex128(value) * cmplx.Rect(1, angle)
		}
		if inverse {
			sum /= complex(float64(n), 0)
		}
		out[k] = complex64(sum)
	}
	return out
}
