package main

import "math"

// cornerSobelArgs is shared with the architecture-specific SIMD kernels.
// n is the number of output pixels; callers provide source rows starting one
// pixel to the left of the first output and tightly packed output slices.
type cornerSobelArgs struct {
	top    *uint8
	middle *uint8
	bottom *uint8
	ix     *int16
	iy     *int16
	n      int
}

type cornerBlurArgs struct {
	a   *uint8
	b   *uint8
	c   *uint8
	dst *uint8
	n   int
}

// cornerEigenArgs is shared with the architecture-specific SIMD kernels. Each
// four-pointer group contains the lower-right, upper-right, lower-left and
// upper-left SAT streams used to recover one structure-tensor component.
type cornerEigenArgs struct {
	xx11 *float64
	xx01 *float64
	xx10 *float64
	xx00 *float64
	yy11 *float64
	yy01 *float64
	yy10 *float64
	yy00 *float64
	xy11 *float64
	xy01 *float64
	xy10 *float64
	xy00 *float64
	dst  *float64
	n    int
	max  float64
}

// cornerSobelRow computes the interior Sobel gradients for one row. Gradients
// are exact signed 16-bit integers (the Sobel range is only [-1020, 1020]),
// cutting their memory traffic to one quarter of the former float64 storage.
func cornerSobelRow(top, middle, bottom []uint8, ix, iy []int16) {
	n := len(ix)
	if n == 0 {
		return
	}
	vectorN := cornerSobelVectorCount(n)
	if vectorN > 0 {
		args := cornerSobelArgs{
			top:    &top[0],
			middle: &middle[0],
			bottom: &bottom[0],
			ix:     &ix[0],
			iy:     &iy[0],
			n:      vectorN,
		}
		cornerSobelSIMD(&args)
	}
	cornerSobelScalar(top, middle, bottom, ix, iy, vectorN)
}

func cornerSobelScalar(top, middle, bottom []uint8, ix, iy []int16, start int) {
	for x := start; x < len(ix); x++ {
		gx := -int(top[x]) - 2*int(middle[x]) - int(bottom[x]) +
			int(top[x+2]) + 2*int(middle[x+2]) + int(bottom[x+2])
		gy := -int(top[x]) - 2*int(top[x+1]) - int(top[x+2]) +
			int(bottom[x]) + 2*int(bottom[x+1]) + int(bottom[x+2])
		ix[x] = int16(gx)
		iy[x] = int16(gy)
	}
}

// cornerBlurRow applies [1 2 1]/4 to three equally sized byte streams. It is
// used for horizontal rows (three shifted views of one row) and vertical rows.
func cornerBlurRow(a, b, c, dst []uint8) {
	n := len(dst)
	if n == 0 {
		return
	}
	vectorN := cornerBlurVectorCount(n)
	if vectorN > 0 {
		args := cornerBlurArgs{a: &a[0], b: &b[0], c: &c[0], dst: &dst[0], n: vectorN}
		cornerBlurSIMD(&args)
	}
	for i := vectorN; i < n; i++ {
		dst[i] = uint8((int(a[i]) + 2*int(b[i]) + int(c[i])) / 4)
	}
}

// cornerEigenRow recovers the three tensor sums from their summed-area tables
// and evaluates the minimum eigenvalue for a contiguous output row.
func cornerEigenRow(
	xx11, xx01, xx10, xx00,
	yy11, yy01, yy10, yy00,
	xy11, xy01, xy10, xy00,
	dst []float64,
) float64 {
	n := len(dst)
	if n == 0 {
		return 0
	}
	vectorN := cornerEigenVectorCount(n)
	maxEig := 0.0
	if vectorN > 0 {
		args := cornerEigenArgs{
			xx11: &xx11[0], xx01: &xx01[0], xx10: &xx10[0], xx00: &xx00[0],
			yy11: &yy11[0], yy01: &yy01[0], yy10: &yy10[0], yy00: &yy00[0],
			xy11: &xy11[0], xy01: &xy01[0], xy10: &xy10[0], xy00: &xy00[0],
			dst: &dst[0], n: vectorN,
		}
		cornerEigenSIMD(&args)
		maxEig = args.max
	}
	for x := vectorN; x < n; x++ {
		sxx := xx11[x] - xx01[x] - xx10[x] + xx00[x]
		syy := yy11[x] - yy01[x] - yy10[x] + yy00[x]
		sxy := xy11[x] - xy01[x] - xy10[x] + xy00[x]

		trace := sxx + syy
		det := sxx*syy - sxy*sxy
		disc := trace*trace/4.0 - det
		if disc < 0 {
			disc = 0
		}
		minEig := trace/2.0 - math.Sqrt(disc)
		dst[x] = minEig
		if minEig > maxEig {
			maxEig = minEig
		}
	}
	return maxEig
}
