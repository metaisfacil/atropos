package cornerdetect

import (
	"math"
	"testing"
)

func TestRollingCornerTensorMatchesNaiveWindow(t *testing.T) {
	const w, h, blockSize = 37, 29, 7
	half := blockSize / 2
	ix := make([]int16, w*h)
	iy := make([]int16, w*h)
	for y := 1; y < h-1; y++ {
		for x := 1; x < w-1; x++ {
			i := y*w + x
			ix[i] = int16((x*31+y*17)%2041 - 1020)
			iy[i] = int16((x*13-y*29)%2041 - 1020)
		}
	}

	hxx, hyy, hxy := make([]int32, w*h), make([]int32, w*h), make([]int32, w*h)
	for y := 0; y < h; y++ {
		base := y * w
		cornerHorizontalTensor(ix[base:base+w], iy[base:base+w], blockSize,
			hxx[base:base+w], hyy[base:base+w], hxy[base:base+w])
	}

	n := w - 2*half
	for y := half; y < h-half; y++ {
		xx, yy, xy := make([]int32, n), make([]int32, n), make([]int32, n)
		for sourceY := y - half; sourceY <= y+half; sourceY++ {
			base := sourceY*w + half
			for x := range n {
				xx[x] += hxx[base+x]
				yy[x] += hyy[base+x]
				xy[x] += hxy[base+x]
			}
		}
		for x := half; x < w-half; x++ {
			var wantXX, wantYY, wantXY int32
			for wy := y - half; wy <= y+half; wy++ {
				for wx := x - half; wx <= x+half; wx++ {
					gx, gy := int32(ix[wy*w+wx]), int32(iy[wy*w+wx])
					wantXX += gx * gx
					wantYY += gy * gy
					wantXY += gx * gy
				}
			}
			got := x - half
			if xx[got] != wantXX || yy[got] != wantYY || xy[got] != wantXY {
				t.Fatalf("(%d,%d): got (%d,%d,%d), want (%d,%d,%d)",
					x, y, xx[got], yy[got], xy[got], wantXX, wantYY, wantXY)
			}
		}
	}
}

func TestCornerTensorEigenMatchesFloatReference(t *testing.T) {
	const n = 67
	xx, yy, xy := make([]int32, n), make([]int32, n), make([]int32, n)
	got := make([]float64, n)
	for i := range n {
		xx[i] = int32(3000000 + i*17003)
		yy[i] = int32(2200000 + i*13001)
		xy[i] = int32((i%17 - 8) * 17000)
	}
	gotMax := cornerTensorEigenRow(xx, yy, xy, got)
	wantMax := 0.0
	for i := range n {
		sxx, syy, sxy := float64(xx[i]), float64(yy[i]), float64(xy[i])
		trace := sxx + syy
		det := sxx*syy - sxy*sxy
		disc := trace*trace/4 - det
		if disc < 0 {
			disc = 0
		}
		want := trace/2 - math.Sqrt(disc)
		if math.Float64bits(got[i]) != math.Float64bits(want) {
			t.Fatalf("lane %d: got %g, want %g", i, got[i], want)
		}
		if want > wantMax {
			wantMax = want
		}
	}
	if math.Float64bits(gotMax) != math.Float64bits(wantMax) {
		t.Fatalf("max: got %g, want %g", gotMax, wantMax)
	}
}
