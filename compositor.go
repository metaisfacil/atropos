package main

// compositor.go — standalone planar image stitching engine.
//
// Operates independently of the main app state: it accepts a slice of
// *image.NRGBA images and returns a single stitched *image.NRGBA.
// The caller (app_compositor.go) handles file I/O and Wails integration.
//
// Algorithm overview:
//  1. Downsample each image to a working resolution for feature detection.
//  2. Detect Shi-Tomasi corners (goodFeaturesToTrack from imgproc.go).
//  3. Extract a normalised 17×17 grayscale patch descriptor at each corner.
//  4. Match pairs of adjacent images with SSD + Lowe's ratio test.
//  5. Run RANSAC with DLT homography estimation on the matched points.
//  6. Compose pairwise homographies into a global reference frame (image 0).
//  7. Render all images onto an output canvas using bilinear interpolation
//     and distance-to-border weighted blending.

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"math"
	"math/rand"
	goruntime "runtime"
	"sync"
)

// ---- Constants ---------------------------------------------------------------

const (
	compositorWorkDim    = 1000  // longest side for feature detection downsample
	compositorPatchRad   = 8     // patch half-size → 17×17 = 289 pixels
	compositorMaxFeats   = 600   // max Shi-Tomasi corners per image
	compositorRansacIter = 2000  // RANSAC iterations per image pair
	compositorRansacThr  = 15.0  // inlier threshold in full-res pixels
	compositorRatioThr   = 0.75  // Lowe's ratio test threshold
	compositorMinMatches = 6     // minimum inliers required to accept a registration
	compositorMaxOutDim  = 20000 // safety cap on output canvas side length
)

// ---- Types -------------------------------------------------------------------

// stitchPt is a 2-D floating-point coordinate.
type stitchPt struct{ X, Y float64 }

// stitchFeature is a Shi-Tomasi corner with its normalised patch descriptor.
type stitchFeature struct {
	Pt   stitchPt
	Desc []float32 // mean-subtracted, unit-variance patch; nil = unusable
}

// stitchMatch is an index pair (A→B) returned by stitchMatchFeatures.
type stitchMatch struct{ A, B int }

// stitchH is a row-major 3×3 homography matrix (indices 0-8).
type stitchH [9]float64

// ---- Top-level entry point ---------------------------------------------------

// stitchImages assembles a sequence of images that share overlap between
// consecutive pairs.  Image 0 is the reference frame; each subsequent image is
// registered to the previous one and the transforms are composed.
func stitchImages(imgs []*image.NRGBA) (*image.NRGBA, error) {
	n := len(imgs)
	if n < 2 {
		return nil, errors.New("compositor: need at least 2 images")
	}

	// 1. Detect features in every image at working resolution.
	feats := make([][]stitchFeature, n)
	scales := make([]float64, n)
	for i, img := range imgs {
		feats[i], scales[i] = stitchDetectAndDescribe(img)
		if len(feats[i]) < compositorMinMatches {
			return nil, fmt.Errorf("compositor: too few features in image %d (%d found)", i, len(feats[i]))
		}
	}

	// 2. Register each image to the previous one; compose into global Hs.
	Hs := make([]stitchH, n)
	Hs[0] = stitchIdentity()
	for i := 1; i < n; i++ {
		// Match: src = image i, dst = image i-1.
		matches := stitchMatchFeatures(feats[i], feats[i-1])
		if len(matches) < compositorMinMatches {
			return nil, fmt.Errorf("compositor: insufficient feature matches between image %d and %d (%d found, need %d)", i-1, i, len(matches), compositorMinMatches)
		}

		// Convert working-resolution coords to full-resolution.
		srcPts := make([]stitchPt, len(matches))
		dstPts := make([]stitchPt, len(matches))
		si, di := scales[i], scales[i-1]
		for k, m := range matches {
			srcPts[k] = stitchPt{feats[i][m.A].Pt.X / si, feats[i][m.A].Pt.Y / si}
			dstPts[k] = stitchPt{feats[i-1][m.B].Pt.X / di, feats[i-1][m.B].Pt.Y / di}
		}

		// RANSAC: find H mapping from image i → image i-1.
		H, _, ok := stitchRANSAC(srcPts, dstPts)
		if !ok {
			return nil, fmt.Errorf("compositor: RANSAC failed for image pair (%d, %d) — overlap may be insufficient", i-1, i)
		}

		// Compose: H[i] maps from image-i coords → reference (image 0) coords.
		Hs[i] = stitchMul(Hs[i-1], H)
	}

	// 3. Compute output canvas bounds.
	minX, minY, maxX, maxY := stitchOutputBounds(imgs, Hs)
	outW := int(math.Ceil(maxX-minX)) + 1
	outH := int(math.Ceil(maxY-minY)) + 1
	if outW > compositorMaxOutDim || outH > compositorMaxOutDim {
		return nil, fmt.Errorf("compositor: output canvas too large (%dx%d); max is %d on each side", outW, outH, compositorMaxOutDim)
	}
	if outW <= 0 || outH <= 0 {
		return nil, errors.New("compositor: degenerate output bounds")
	}

	// 4. Render.
	return stitchRender(imgs, Hs, minX, minY, outW, outH), nil
}

// ---- Feature detection & description -----------------------------------------

// stitchDetectAndDescribe detects Shi-Tomasi corners and extracts normalised
// patch descriptors from a downsampled working copy of img.
// Returns the features and the scale factor (working / full).
func stitchDetectAndDescribe(img *image.NRGBA) ([]stitchFeature, float64) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()

	scale := 1.0
	workImg := img
	if mx := max(w, h); mx > compositorWorkDim {
		scale = float64(compositorWorkDim) / float64(mx)
		nw := int(float64(w) * scale)
		nh := int(float64(h) * scale)
		workImg = resizeNRGBA(img, nw, nh)
	}

	gray := toGrayscale(workImg)
	// Use a moderate quality level and minimum distance to get a good spread.
	pts, _ := goodFeaturesToTrack(context.Background(), gray, compositorMaxFeats, 0.01, 10, 7)

	feats := make([]stitchFeature, 0, len(pts))
	for _, p := range pts {
		desc := stitchPatchDesc(gray, p.X, p.Y)
		if desc == nil {
			continue
		}
		feats = append(feats, stitchFeature{
			Pt:   stitchPt{float64(p.X), float64(p.Y)},
			Desc: desc,
		})
	}
	return feats, scale
}

// stitchPatchDesc extracts a normalised (zero-mean, unit-variance) grayscale
// patch of size (2r+1)² centred at (cx, cy).  Returns nil for edge points or
// uniform patches (unusable as discriminative descriptors).
func stitchPatchDesc(gray *image.Gray, cx, cy int) []float32 {
	r := compositorPatchRad
	b := gray.Bounds()
	if cx-r < 0 || cy-r < 0 || cx+r >= b.Dx() || cy+r >= b.Dy() {
		return nil
	}
	side := 2*r + 1
	patch := make([]float32, side*side)
	var mean float32
	for dy := -r; dy <= r; dy++ {
		for dx := -r; dx <= r; dx++ {
			v := float32(gray.GrayAt(cx+dx, cy+dy).Y)
			patch[(dy+r)*side+(dx+r)] = v
			mean += v
		}
	}
	mean /= float32(side * side)

	var variance float32
	for _, v := range patch {
		d := v - mean
		variance += d * d
	}
	variance /= float32(side * side)
	if variance < 1.0 { // uniform patch — not useful
		return nil
	}
	invStd := float32(1.0 / math.Sqrt(float64(variance)))
	for i, v := range patch {
		patch[i] = (v - mean) * invStd
	}
	return patch
}

// ---- Feature matching --------------------------------------------------------

// stitchMatchFeatures matches every feature in setA against every feature in
// setB using SSD distance with Lowe's ratio test.
// A match (A:i, B:j) means setA[i] best corresponds to setB[j].
func stitchMatchFeatures(setA, setB []stitchFeature) []stitchMatch {
	var matches []stitchMatch
	for i, fa := range setA {
		if fa.Desc == nil {
			continue
		}
		best1, best2 := math.MaxFloat64, math.MaxFloat64
		bestJ := -1
		for j, fb := range setB {
			if fb.Desc == nil {
				continue
			}
			d := stitchSSD(fa.Desc, fb.Desc)
			if d < best1 {
				best2 = best1
				best1 = d
				bestJ = j
			} else if d < best2 {
				best2 = d
			}
		}
		if bestJ >= 0 && best2 > 1e-10 && best1/best2 < compositorRatioThr {
			matches = append(matches, stitchMatch{A: i, B: bestJ})
		}
	}
	return matches
}

// stitchSSD computes the sum of squared differences between two equal-length
// float32 slices.
func stitchSSD(a, b []float32) float64 {
	var s float64
	for i := range a {
		d := float64(a[i] - b[i])
		s += d * d
	}
	return s
}

// ---- RANSAC ------------------------------------------------------------------

// stitchRANSAC robustly estimates the homography mapping src → dst using
// RANSAC.  Returns the best homography, the inlier indices into src/dst, and
// whether estimation succeeded.
func stitchRANSAC(src, dst []stitchPt) (stitchH, []int, bool) {
	n := len(src)
	if n < 4 {
		return stitchIdentity(), nil, false
	}

	rng := rand.New(rand.NewSource(0xdeadbeef))
	thrSq := compositorRansacThr * compositorRansacThr

	var bestH stitchH
	var bestInliers []int

	sampSrc := make([]stitchPt, 4)
	sampDst := make([]stitchPt, 4)

	for iter := 0; iter < compositorRansacIter; iter++ {
		// Pick 4 distinct random correspondences.
		idx := stitchSample4(n, rng)
		for k := 0; k < 4; k++ {
			sampSrc[k] = src[idx[k]]
			sampDst[k] = dst[idx[k]]
		}
		H, ok := stitchDLT(sampSrc, sampDst)
		if !ok {
			continue
		}
		// Count inliers.
		var inliers []int
		for i := 0; i < n; i++ {
			p := stitchApply(H, src[i])
			dx, dy := p.X-dst[i].X, p.Y-dst[i].Y
			if dx*dx+dy*dy < thrSq {
				inliers = append(inliers, i)
			}
		}
		if len(inliers) > len(bestInliers) {
			bestInliers = inliers
			bestH = H
		}
	}

	if len(bestInliers) < compositorMinMatches {
		return stitchIdentity(), nil, false
	}

	// Refine using all inliers.
	iSrc := make([]stitchPt, len(bestInliers))
	iDst := make([]stitchPt, len(bestInliers))
	for k, i := range bestInliers {
		iSrc[k] = src[i]
		iDst[k] = dst[i]
	}
	if H, ok := stitchDLT(iSrc, iDst); ok {
		bestH = H
	}

	return bestH, bestInliers, true
}

// stitchSample4 returns 4 distinct random indices in [0, n).
func stitchSample4(n int, rng *rand.Rand) [4]int {
	perm := rng.Perm(n)
	return [4]int{perm[0], perm[1], perm[2], perm[3]}
}

// ---- DLT homography estimation -----------------------------------------------

// stitchDLT computes the homography H mapping src[i] → dst[i] for n ≥ 4
// correspondences using the Direct Linear Transform with Hartley normalisation.
func stitchDLT(src, dst []stitchPt) (stitchH, bool) {
	n := len(src)
	if n < 4 {
		return stitchIdentity(), false
	}

	// Hartley normalisation: translate centroid to origin, scale so average
	// distance from origin is √2.  Dramatically improves numerical stability.
	Tsrc, TsrcInv := stitchNorm(src)
	Tdst, TdstInv := stitchNorm(dst)
	_ = TsrcInv

	nSrc := make([]stitchPt, n)
	nDst := make([]stitchPt, n)
	for i := range src {
		nSrc[i] = stitchApply(Tsrc, src[i])
		nDst[i] = stitchApply(Tdst, dst[i])
	}

	// Build normal equations (AtA)h = (Atb) where h ∈ ℝ⁸ (h₂₂ = 1).
	// For correspondence (x,y)→(u,v):
	//   row 0: [x, y, 1, 0, 0, 0, -u·x, -u·y] · h = u
	//   row 1: [0, 0, 0, x, y, 1, -v·x, -v·y] · h = v
	var AtA [8][8]float64
	var Atb [8]float64
	for i := 0; i < n; i++ {
		x, y := nSrc[i].X, nSrc[i].Y
		u, v := nDst[i].X, nDst[i].Y
		a0 := [8]float64{x, y, 1, 0, 0, 0, -u * x, -u * y}
		a1 := [8]float64{0, 0, 0, x, y, 1, -v * x, -v * y}
		for j := 0; j < 8; j++ {
			Atb[j] += a0[j]*u + a1[j]*v
			for k := 0; k < 8; k++ {
				AtA[j][k] += a0[j]*a0[k] + a1[j]*a1[k]
			}
		}
	}

	h, ok := gaussSolve8(AtA, Atb)
	if !ok {
		return stitchIdentity(), false
	}

	Hn := stitchH{h[0], h[1], h[2], h[3], h[4], h[5], h[6], h[7], 1.0}
	// Denormalise: H = Tdst⁻¹ · Hn · Tsrc
	return stitchMul(stitchMul(TdstInv, Hn), Tsrc), true
}

// stitchNorm computes the Hartley normalisation transform for a point set.
// Returns T (the normalisation) and T⁻¹.
func stitchNorm(pts []stitchPt) (T, Tinv stitchH) {
	if len(pts) == 0 {
		return stitchIdentity(), stitchIdentity()
	}
	var cx, cy float64
	for _, p := range pts {
		cx += p.X
		cy += p.Y
	}
	cx /= float64(len(pts))
	cy /= float64(len(pts))

	var avgD float64
	for _, p := range pts {
		dx, dy := p.X-cx, p.Y-cy
		avgD += math.Sqrt(dx*dx + dy*dy)
	}
	avgD /= float64(len(pts))
	if avgD < 1e-10 {
		return stitchIdentity(), stitchIdentity()
	}
	s := math.Sqrt2 / avgD
	T = stitchH{s, 0, -s * cx, 0, s, -s * cy, 0, 0, 1}
	Tinv = stitchH{1 / s, 0, cx, 0, 1 / s, cy, 0, 0, 1}
	return
}

// gaussSolve8 solves the 8×8 linear system A·h = b using Gaussian elimination
// with partial pivoting.  Returns the solution and whether it succeeded.
func gaussSolve8(A [8][8]float64, b [8]float64) ([8]float64, bool) {
	// Augmented matrix [A|b].
	var aug [8][9]float64
	for i := 0; i < 8; i++ {
		for j := 0; j < 8; j++ {
			aug[i][j] = A[i][j]
		}
		aug[i][8] = b[i]
	}

	for col := 0; col < 8; col++ {
		// Partial pivot.
		pivot := col
		maxV := math.Abs(aug[col][col])
		for row := col + 1; row < 8; row++ {
			if v := math.Abs(aug[row][col]); v > maxV {
				maxV = v
				pivot = row
			}
		}
		if maxV < 1e-12 {
			return [8]float64{}, false
		}
		aug[col], aug[pivot] = aug[pivot], aug[col]

		pv := aug[col][col]
		for row := col + 1; row < 8; row++ {
			f := aug[row][col] / pv
			for k := col; k <= 8; k++ {
				aug[row][k] -= f * aug[col][k]
			}
		}
	}

	var h [8]float64
	for i := 7; i >= 0; i-- {
		h[i] = aug[i][8]
		for j := i + 1; j < 8; j++ {
			h[i] -= aug[i][j] * h[j]
		}
		h[i] /= aug[i][i]
	}
	return h, true
}

// ---- Homography arithmetic ---------------------------------------------------

func stitchIdentity() stitchH {
	return stitchH{1, 0, 0, 0, 1, 0, 0, 0, 1}
}

// stitchApply maps point p through homography H.
func stitchApply(H stitchH, p stitchPt) stitchPt {
	x, y := p.X, p.Y
	w := H[6]*x + H[7]*y + H[8]
	if math.Abs(w) < 1e-12 {
		return p
	}
	return stitchPt{
		X: (H[0]*x + H[1]*y + H[2]) / w,
		Y: (H[3]*x + H[4]*y + H[5]) / w,
	}
}

// stitchMul returns A·B.
func stitchMul(A, B stitchH) stitchH {
	var C stitchH
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			for k := 0; k < 3; k++ {
				C[i*3+j] += A[i*3+k] * B[k*3+j]
			}
		}
	}
	return C
}

// stitchInv computes the inverse of H using Cramer's rule.
func stitchInv(H stitchH) stitchH {
	a, b, c := H[0], H[1], H[2]
	d, e, f := H[3], H[4], H[5]
	g, h, k := H[6], H[7], H[8]
	det := a*(e*k-f*h) - b*(d*k-f*g) + c*(d*h-e*g)
	if math.Abs(det) < 1e-12 {
		return stitchIdentity()
	}
	inv := 1.0 / det
	return stitchH{
		(e*k - f*h) * inv, -(b*k - c*h) * inv, (b*f - c*e) * inv,
		-(d*k - f*g) * inv, (a*k - c*g) * inv, -(a*f - c*d) * inv,
		(d*h - e*g) * inv, -(a*h - b*g) * inv, (a*e - b*d) * inv,
	}
}

// ---- Output bounds -----------------------------------------------------------

// stitchOutputBounds computes the bounding box (in reference-frame coordinates)
// of all transformed image corners.
func stitchOutputBounds(imgs []*image.NRGBA, Hs []stitchH) (minX, minY, maxX, maxY float64) {
	minX = math.MaxFloat64
	minY = math.MaxFloat64
	maxX = -math.MaxFloat64
	maxY = -math.MaxFloat64
	for i, img := range imgs {
		b := img.Bounds()
		fw, fh := float64(b.Dx()), float64(b.Dy())
		for _, c := range [4]stitchPt{{0, 0}, {fw, 0}, {0, fh}, {fw, fh}} {
			r := stitchApply(Hs[i], c)
			if r.X < minX {
				minX = r.X
			}
			if r.Y < minY {
				minY = r.Y
			}
			if r.X > maxX {
				maxX = r.X
			}
			if r.Y > maxY {
				maxY = r.Y
			}
		}
	}
	return
}

// ---- Rendering ---------------------------------------------------------------

// stitchRender composites all images onto an output canvas.
// offsetX/offsetY are the reference-frame coordinates corresponding to pixel
// (0,0) in the output (i.e. minX/minY from stitchOutputBounds).
// Blending uses per-pixel distance-to-border weights.
func stitchRender(imgs []*image.NRGBA, Hs []stitchH, offsetX, offsetY float64, outW, outH int) *image.NRGBA {
	out := image.NewNRGBA(image.Rect(0, 0, outW, outH))
	n := len(imgs)

	// Pre-compute inverse homographies and image bounds (float).
	invHs := make([]stitchH, n)
	fW := make([]float64, n)
	fH := make([]float64, n)
	for i, img := range imgs {
		invHs[i] = stitchInv(Hs[i])
		b := img.Bounds()
		fW[i] = float64(b.Dx())
		fH[i] = float64(b.Dy())
	}

	numCPU := goruntime.NumCPU()
	rows := make(chan int, outH)
	for oy := 0; oy < outH; oy++ {
		rows <- oy
	}
	close(rows)

	var wg sync.WaitGroup
	for t := 0; t < numCPU; t++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for oy := range rows {
				refY := float64(oy) + offsetY
				for ox := 0; ox < outW; ox++ {
					refX := float64(ox) + offsetX
					var rSum, gSum, bSum, wSum float64
					for i, img := range imgs {
						// Back-project reference pixel to source image space.
						sp := stitchApply(invHs[i], stitchPt{refX, refY})
						sx, sy := sp.X, sp.Y
						if sx < 0 || sy < 0 || sx >= fW[i] || sy >= fH[i] {
							continue
						}
						// Distance-to-border weight (feathering).
						w := math.Min(sx, math.Min(fW[i]-1-sx, math.Min(sy, fH[i]-1-sy)))
						if w <= 0 {
							continue
						}
						r, g, bl := stitchBilinear(img, sx, sy)
						rSum += w * r
						gSum += w * g
						bSum += w * bl
						wSum += w
					}
					if wSum > 0 {
						out.SetNRGBA(ox, oy, color.NRGBA{
							R: clampByte(int(rSum / wSum + 0.5)),
							G: clampByte(int(gSum / wSum + 0.5)),
							B: clampByte(int(bSum / wSum + 0.5)),
							A: 255,
						})
					}
				}
			}
		}()
	}
	wg.Wait()
	return out
}

// stitchBilinear samples img at sub-pixel position (x, y) using bilinear
// interpolation.  Returns float64 channel values in [0, 255].
func stitchBilinear(img *image.NRGBA, x, y float64) (r, g, b float64) {
	b0 := img.Bounds()
	w, h := b0.Dx(), b0.Dy()
	ix, iy := int(x), int(y)
	fx, fy := x-float64(ix), y-float64(iy)
	ix1 := ix + 1
	if ix1 >= w {
		ix1 = w - 1
	}
	iy1 := iy + 1
	if iy1 >= h {
		iy1 = h - 1
	}
	c00 := img.NRGBAAt(b0.Min.X+ix, b0.Min.Y+iy)
	c10 := img.NRGBAAt(b0.Min.X+ix1, b0.Min.Y+iy)
	c01 := img.NRGBAAt(b0.Min.X+ix, b0.Min.Y+iy1)
	c11 := img.NRGBAAt(b0.Min.X+ix1, b0.Min.Y+iy1)
	w00 := (1 - fx) * (1 - fy)
	w10 := fx * (1 - fy)
	w01 := (1 - fx) * fy
	w11 := fx * fy
	r = w00*float64(c00.R) + w10*float64(c10.R) + w01*float64(c01.R) + w11*float64(c11.R)
	g = w00*float64(c00.G) + w10*float64(c10.G) + w01*float64(c01.G) + w11*float64(c11.G)
	b = w00*float64(c00.B) + w10*float64(c10.B) + w01*float64(c01.B) + w11*float64(c11.B)
	return
}

