package main

import (
	"image"
	"math"
)

// ============================================================
// GEOMETRY — pure math helpers for point ordering, line
// intersection, homography computation, and matrix inversion.
// ============================================================

// dist returns the Euclidean distance between two points.
func dist(a, b image.Point) float64 {
	dx := float64(a.X - b.X)
	dy := float64(a.Y - b.Y)
	return math.Sqrt(dx*dx + dy*dy)
}

// bilinear performs bilinear interpolation of four corner values.
func bilinear(c00, c10, c01, c11, fx, fy float64) float64 {
	return c00*(1-fx)*(1-fy) + c10*fx*(1-fy) + c01*(1-fx)*fy + c11*fx*fy
}

// lineIntersection computes the intersection of two lines defined by two points each.
// Returns nil when the lines are parallel.
func lineIntersection(line1, line2 []image.Point) *image.Point {
	x1, y1 := float64(line1[0].X), float64(line1[0].Y)
	x2, y2 := float64(line1[1].X), float64(line1[1].Y)
	x3, y3 := float64(line2[0].X), float64(line2[0].Y)
	x4, y4 := float64(line2[1].X), float64(line2[1].Y)

	denom := (x1-x2)*(y3-y4) - (y1-y2)*(x3-x4)
	if math.Abs(denom) < 1e-10 {
		return nil
	}

	px := ((x1*y2-y1*x2)*(x3-x4) - (x1-x2)*(x3*y4-y3*x4)) / denom
	py := ((x1*y2-y1*x2)*(y3-y4) - (y1-y2)*(x3*y4-y3*x4)) / denom

	return &image.Point{X: int(px), Y: int(py)}
}

// sortVertices sorts 4 points into UL, UR, BL, BR order
// (top two sorted by X, bottom two sorted by X).
func sortVertices(vertices []image.Point) []image.Point {
	pts := make([]image.Point, len(vertices))
	copy(pts, vertices)

	// Sort by Y (bubble)
	for i := 0; i < 3; i++ {
		for j := i + 1; j < 4; j++ {
			if pts[j].Y < pts[i].Y {
				pts[i], pts[j] = pts[j], pts[i]
			}
		}
	}
	// Top two by X
	if pts[0].X > pts[1].X {
		pts[0], pts[1] = pts[1], pts[0]
	}
	// Bottom two by X
	if pts[2].X > pts[3].X {
		pts[2], pts[3] = pts[3], pts[2]
	}
	return pts
}

// orderPoints orders 4 points as TL, TR, BR, BL using the sum/diff heuristic.
func orderPoints(pts []image.Point) []image.Point {
	result := make([]image.Point, 4)
	minSum, maxSum := math.MaxFloat64, -math.MaxFloat64
	minDiff, maxDiff := math.MaxFloat64, -math.MaxFloat64
	mi, mxi, mdi, mxdi := 0, 0, 0, 0

	for i, p := range pts {
		s := float64(p.X + p.Y)
		d := float64(p.X - p.Y)
		if s < minSum {
			minSum = s
			mi = i
		}
		if s > maxSum {
			maxSum = s
			mxi = i
		}
		if d < minDiff {
			minDiff = d
			mdi = i
		}
		if d > maxDiff {
			maxDiff = d
			mxdi = i
		}
	}
	result[0] = pts[mi]   // TL
	result[1] = pts[mxdi] // TR
	result[2] = pts[mxi]  // BR
	result[3] = pts[mdi]  // BL
	return result
}

// computeHomography computes a 3×3 homography mapping src→dst using DLT
// (Direct Linear Transform) with Gaussian elimination.
func computeHomography(src, dst [4][2]float64) [9]float64 {
	var A [8][9]float64
	for i := 0; i < 4; i++ {
		x, y := src[i][0], src[i][1]
		u, v := dst[i][0], dst[i][1]
		A[2*i] = [9]float64{-x, -y, -1, 0, 0, 0, u * x, u * y, u}
		A[2*i+1] = [9]float64{0, 0, 0, -x, -y, -1, v * x, v * y, v}
	}

	var m [8][9]float64
	copy(m[:], A[:])

	// Forward elimination with partial pivoting
	for col := 0; col < 8; col++ {
		maxRow := col
		maxVal := math.Abs(m[col][col])
		for row := col + 1; row < 8; row++ {
			if math.Abs(m[row][col]) > maxVal {
				maxVal = math.Abs(m[row][col])
				maxRow = row
			}
		}
		m[col], m[maxRow] = m[maxRow], m[col]

		if math.Abs(m[col][col]) < 1e-12 {
			continue
		}

		for row := col + 1; row < 8; row++ {
			factor := m[row][col] / m[col][col]
			for k := col; k < 9; k++ {
				m[row][k] -= factor * m[col][k]
			}
		}
	}

	// Back substitution with h[8] = 1
	var h [9]float64
	h[8] = 1.0
	for i := 7; i >= 0; i-- {
		sum := m[i][8] * h[8]
		for j := i + 1; j < 8; j++ {
			sum += m[i][j] * h[j]
		}
		if math.Abs(m[i][i]) < 1e-12 {
			h[i] = 0
		} else {
			h[i] = -sum / m[i][i]
		}
	}

	return h
}

// invert3x3 inverts a 3×3 matrix stored as [9]float64 in row-major order.
// Returns the input unchanged when the matrix is singular.
func invert3x3(m [9]float64) [9]float64 {
	det := m[0]*(m[4]*m[8]-m[5]*m[7]) - m[1]*(m[3]*m[8]-m[5]*m[6]) + m[2]*(m[3]*m[7]-m[4]*m[6])
	if math.Abs(det) < 1e-12 {
		return m
	}
	invDet := 1.0 / det

	return [9]float64{
		(m[4]*m[8] - m[5]*m[7]) * invDet,
		(m[2]*m[7] - m[1]*m[8]) * invDet,
		(m[1]*m[5] - m[2]*m[4]) * invDet,
		(m[5]*m[6] - m[3]*m[8]) * invDet,
		(m[0]*m[8] - m[2]*m[6]) * invDet,
		(m[2]*m[3] - m[0]*m[5]) * invDet,
		(m[3]*m[7] - m[4]*m[6]) * invDet,
		(m[1]*m[6] - m[0]*m[7]) * invDet,
		(m[0]*m[4] - m[1]*m[3]) * invDet,
	}
}
