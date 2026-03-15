package main

import (
	"image"
	"math"
	"testing"
)

// ---- dist ----

func TestDist_345Triangle(t *testing.T) {
	d := dist(image.Pt(0, 0), image.Pt(3, 4))
	if math.Abs(d-5.0) > 1e-9 {
		t.Fatalf("expected 5, got %f", d)
	}
}

func TestDist_SamePoint(t *testing.T) {
	d := dist(image.Pt(7, 13), image.Pt(7, 13))
	if d != 0 {
		t.Fatalf("expected 0, got %f", d)
	}
}

func TestDist_Horizontal(t *testing.T) {
	d := dist(image.Pt(0, 0), image.Pt(10, 0))
	if math.Abs(d-10.0) > 1e-9 {
		t.Fatalf("expected 10, got %f", d)
	}
}

func TestDist_Vertical(t *testing.T) {
	d := dist(image.Pt(0, 0), image.Pt(0, 7))
	if math.Abs(d-7.0) > 1e-9 {
		t.Fatalf("expected 7, got %f", d)
	}
}

func TestDist_Symmetry(t *testing.T) {
	a := image.Pt(3, 7)
	b := image.Pt(11, 2)
	if math.Abs(dist(a, b)-dist(b, a)) > 1e-12 {
		t.Fatal("dist must be symmetric")
	}
}

// ---- bilinear ----

func TestBilinear_Corners(t *testing.T) {
	// At (0,0) should return c00
	if v := bilinear(10, 20, 30, 40, 0, 0); math.Abs(v-10) > 1e-9 {
		t.Fatalf("corner (0,0): expected 10, got %f", v)
	}
	// At (1,0) should return c10
	if v := bilinear(10, 20, 30, 40, 1, 0); math.Abs(v-20) > 1e-9 {
		t.Fatalf("corner (1,0): expected 20, got %f", v)
	}
	// At (0,1) should return c01
	if v := bilinear(10, 20, 30, 40, 0, 1); math.Abs(v-30) > 1e-9 {
		t.Fatalf("corner (0,1): expected 30, got %f", v)
	}
	// At (1,1) should return c11
	if v := bilinear(10, 20, 30, 40, 1, 1); math.Abs(v-40) > 1e-9 {
		t.Fatalf("corner (1,1): expected 40, got %f", v)
	}
}

func TestBilinear_Center(t *testing.T) {
	// All same value → centre equals that value
	v := bilinear(100, 100, 100, 100, 0.5, 0.5)
	if math.Abs(v-100) > 1e-9 {
		t.Fatalf("expected 100, got %f", v)
	}
}

func TestBilinear_MidX(t *testing.T) {
	// Linear along x at y=0: (0+100)/2 = 50
	v := bilinear(0, 100, 0, 100, 0.5, 0)
	if math.Abs(v-50) > 1e-9 {
		t.Fatalf("expected 50, got %f", v)
	}
}

func TestBilinear_Average(t *testing.T) {
	// Centre of 0,100,100,200 → (0+100+100+200)/4 = 125 using bilinear formula
	// bilinear(0,100,100,200,0.5,0.5) = 0*0.25+100*0.25+100*0.25+200*0.25 = 100
	v := bilinear(0, 100, 100, 200, 0.5, 0.5)
	if math.Abs(v-100) > 1e-9 {
		t.Fatalf("expected 100, got %f", v)
	}
}

// ---- lineIntersection ----

func TestLineIntersection_Perpendicular(t *testing.T) {
	// Horizontal line y=50 from x=0..100
	l1 := []image.Point{{0, 50}, {100, 50}}
	// Vertical line x=50 from y=0..100
	l2 := []image.Point{{50, 0}, {50, 100}}

	pt := lineIntersection(l1, l2)
	if pt == nil {
		t.Fatal("expected intersection, got nil")
	}
	if pt.X != 50 || pt.Y != 50 {
		t.Fatalf("expected (50,50), got (%d,%d)", pt.X, pt.Y)
	}
}

func TestLineIntersection_Parallel(t *testing.T) {
	l1 := []image.Point{{0, 0}, {100, 0}}
	l2 := []image.Point{{0, 10}, {100, 10}}

	pt := lineIntersection(l1, l2)
	if pt != nil {
		t.Fatalf("expected nil for parallel lines, got (%d,%d)", pt.X, pt.Y)
	}
}

func TestLineIntersection_Diagonal(t *testing.T) {
	// y=x and y=-x+100 should intersect at (50,50)
	l1 := []image.Point{{0, 0}, {100, 100}}
	l2 := []image.Point{{0, 100}, {100, 0}}

	pt := lineIntersection(l1, l2)
	if pt == nil {
		t.Fatal("expected intersection")
	}
	if pt.X != 50 || pt.Y != 50 {
		t.Fatalf("expected (50,50), got (%d,%d)", pt.X, pt.Y)
	}
}

func TestLineIntersection_Origin(t *testing.T) {
	l1 := []image.Point{{-10, 0}, {10, 0}}
	l2 := []image.Point{{0, -10}, {0, 10}}

	pt := lineIntersection(l1, l2)
	if pt == nil {
		t.Fatal("expected intersection at origin")
	}
	if pt.X != 0 || pt.Y != 0 {
		t.Fatalf("expected (0,0), got (%d,%d)", pt.X, pt.Y)
	}
}

// ---- sortVertices ----

func TestSortVertices_AlreadySorted(t *testing.T) {
	// UL(0,0) UR(100,0) BL(0,100) BR(100,100) — already in order
	pts := []image.Point{{0, 0}, {100, 0}, {0, 100}, {100, 100}}
	sorted := sortVertices(pts)

	expect := []image.Point{{0, 0}, {100, 0}, {0, 100}, {100, 100}}
	for i, p := range sorted {
		if p != expect[i] {
			t.Fatalf("index %d: expected %v, got %v", i, expect[i], p)
		}
	}
}

func TestSortVertices_Scrambled(t *testing.T) {
	// BR, UL, BL, UR → should sort to UL, UR, BL, BR
	pts := []image.Point{{100, 100}, {0, 0}, {0, 100}, {100, 0}}
	sorted := sortVertices(pts)

	// Top two (sorted by X): UL, UR
	if sorted[0].X > sorted[1].X {
		t.Fatal("top-left should have smaller X than top-right")
	}
	// Bottom two (sorted by X): BL, BR
	if sorted[2].X > sorted[3].X {
		t.Fatal("bottom-left should have smaller X than bottom-right")
	}
	// Top row should have smaller Y than bottom row
	if sorted[0].Y > sorted[2].Y {
		t.Fatal("top row should be above bottom row")
	}
}

func TestSortVertices_DoesNotMutateInput(t *testing.T) {
	pts := []image.Point{{100, 100}, {0, 0}, {0, 100}, {100, 0}}
	orig := make([]image.Point, 4)
	copy(orig, pts)
	sortVertices(pts)
	for i := range pts {
		if pts[i] != orig[i] {
			t.Fatal("sortVertices must not mutate the input slice")
		}
	}
}

// ---- orderPoints ----

func TestOrderPoints_Square(t *testing.T) {
	// TL(10,10) TR(90,10) BR(90,90) BL(10,90)
	pts := []image.Point{{90, 90}, {10, 10}, {90, 10}, {10, 90}}
	ordered := orderPoints(pts)

	// TL = min sum (10+10=20)
	if ordered[0] != (image.Point{10, 10}) {
		t.Fatalf("TL: expected (10,10), got %v", ordered[0])
	}
	// TR = max diff (90-10=80)
	if ordered[1] != (image.Point{90, 10}) {
		t.Fatalf("TR: expected (90,10), got %v", ordered[1])
	}
	// BR = max sum (90+90=180)
	if ordered[2] != (image.Point{90, 90}) {
		t.Fatalf("BR: expected (90,90), got %v", ordered[2])
	}
	// BL = min diff (10-90=-80)
	if ordered[3] != (image.Point{10, 90}) {
		t.Fatalf("BL: expected (10,90), got %v", ordered[3])
	}
}

func TestOrderPoints_Rectangle(t *testing.T) {
	pts := []image.Point{{200, 50}, {0, 0}, {0, 50}, {200, 0}}
	ordered := orderPoints(pts)

	if ordered[0] != (image.Point{0, 0}) {
		t.Fatalf("TL: expected (0,0), got %v", ordered[0])
	}
	if ordered[1] != (image.Point{200, 0}) {
		t.Fatalf("TR: expected (200,0), got %v", ordered[1])
	}
	if ordered[2] != (image.Point{200, 50}) {
		t.Fatalf("BR: expected (200,50), got %v", ordered[2])
	}
	if ordered[3] != (image.Point{0, 50}) {
		t.Fatalf("BL: expected (0,50), got %v", ordered[3])
	}
}

// ---- computeHomography ----

func TestComputeHomography_Identity(t *testing.T) {
	// Mapping that should produce an identity (or scalar multiple of it)
	pts := [4][2]float64{{0, 0}, {100, 0}, {100, 100}, {0, 100}}
	H := computeHomography(pts, pts)

	// H should be proportional to identity: diag non-zero, off-diag ≈ 0
	// Normalise so H[8]=1 (guaranteed by our solver)
	if math.Abs(H[8]) < 1e-12 {
		t.Fatal("H[8] should be non-zero")
	}
	scale := H[8]
	for i := 0; i < 9; i++ {
		H[i] /= scale
	}

	// Check identity-like: H[0]≈1, H[4]≈1, H[8]=1, rest ≈ 0
	tol := 1e-6
	if math.Abs(H[0]-1) > tol || math.Abs(H[4]-1) > tol || math.Abs(H[8]-1) > tol {
		t.Fatalf("diagonal should be ~1, got [%f, %f, %f]", H[0], H[4], H[8])
	}
	offDiag := []int{1, 2, 3, 5, 6, 7}
	for _, idx := range offDiag {
		if math.Abs(H[idx]) > tol {
			t.Fatalf("H[%d] should be ~0, got %f", idx, H[idx])
		}
	}
}

func TestComputeHomography_Translation(t *testing.T) {
	src := [4][2]float64{{0, 0}, {100, 0}, {100, 100}, {0, 100}}
	// Shift everything by (50, 30)
	dst := [4][2]float64{{50, 30}, {150, 30}, {150, 130}, {50, 130}}
	H := computeHomography(src, dst)

	// Apply H to (0,0): should give (50,30)
	w := H[6]*0 + H[7]*0 + H[8]
	x := (H[0]*0 + H[1]*0 + H[2]) / w
	y := (H[3]*0 + H[4]*0 + H[5]) / w

	if math.Abs(x-50) > 0.5 || math.Abs(y-30) > 0.5 {
		t.Fatalf("expected (50,30), got (%.2f,%.2f)", x, y)
	}
}

func TestComputeHomography_Scale(t *testing.T) {
	src := [4][2]float64{{0, 0}, {100, 0}, {100, 100}, {0, 100}}
	// Scale by 2x
	dst := [4][2]float64{{0, 0}, {200, 0}, {200, 200}, {0, 200}}
	H := computeHomography(src, dst)

	// (50,50) → (100,100)
	w := H[6]*50 + H[7]*50 + H[8]
	x := (H[0]*50 + H[1]*50 + H[2]) / w
	y := (H[3]*50 + H[4]*50 + H[5]) / w

	if math.Abs(x-100) > 0.5 || math.Abs(y-100) > 0.5 {
		t.Fatalf("expected (100,100), got (%.2f,%.2f)", x, y)
	}
}

// ---- invert3x3 ----

func TestInvert3x3_Identity(t *testing.T) {
	I := [9]float64{1, 0, 0, 0, 1, 0, 0, 0, 1}
	inv := invert3x3(I)
	for i := 0; i < 9; i++ {
		if math.Abs(inv[i]-I[i]) > 1e-12 {
			t.Fatalf("identity inverse failed at [%d]: got %f", i, inv[i])
		}
	}
}

func TestInvert3x3_KnownMatrix(t *testing.T) {
	// [2 1 0 ; 0 1 0 ; 0 0 1] → inv = [0.5 -0.5 0 ; 0 1 0 ; 0 0 1]
	m := [9]float64{2, 1, 0, 0, 1, 0, 0, 0, 1}
	inv := invert3x3(m)

	expected := [9]float64{0.5, -0.5, 0, 0, 1, 0, 0, 0, 1}
	for i := 0; i < 9; i++ {
		if math.Abs(inv[i]-expected[i]) > 1e-9 {
			t.Fatalf("[%d]: expected %f, got %f", i, expected[i], inv[i])
		}
	}
}

func TestInvert3x3_ProductIsIdentity(t *testing.T) {
	m := [9]float64{3, 0, 2, 2, 0, -2, 0, 1, 1}
	inv := invert3x3(m)

	// Compute m * inv and check it equals identity
	var product [9]float64
	for r := 0; r < 3; r++ {
		for c := 0; c < 3; c++ {
			for k := 0; k < 3; k++ {
				product[r*3+c] += m[r*3+k] * inv[k*3+c]
			}
		}
	}

	for r := 0; r < 3; r++ {
		for c := 0; c < 3; c++ {
			expected := 0.0
			if r == c {
				expected = 1.0
			}
			if math.Abs(product[r*3+c]-expected) > 1e-9 {
				t.Fatalf("M*M^-1 [%d,%d]: expected %f, got %f", r, c, expected, product[r*3+c])
			}
		}
	}
}

func TestInvert3x3_Singular(t *testing.T) {
	// Singular matrix (row 2 = 2*row 1)
	m := [9]float64{1, 2, 3, 2, 4, 6, 0, 0, 1}
	inv := invert3x3(m)
	// When det≈0, function returns original matrix
	for i := 0; i < 9; i++ {
		if inv[i] != m[i] {
			t.Fatalf("singular case: expected original matrix back, index %d differs", i)
		}
	}
}
