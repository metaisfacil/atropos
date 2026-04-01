package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"math"
	goruntime "runtime"
	"sort"
	"sync"
)

// ============================================================
// IMAGE PROCESSING — pure Go pixel-level operations: colour
// conversion, histogram equalisation, corner detection,
// perspective warp, rotation, masking, resize, and drawing.
// ============================================================

// ---- Numeric helpers ----

// clamp constrains val to [lo, hi].
func clamp(val, lo, hi int) int {
	if val < lo {
		return lo
	}
	if val > hi {
		return hi
	}
	return val
}

// clampByte constrains an int to [0, 255] and returns uint8.
func clampByte(v int) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}

// ---- Image conversion helpers ----

// toNRGBA converts any image.Image to *image.NRGBA.
// Fast paths avoid draw.Draw overhead for common types.
func toNRGBA(src image.Image) *image.NRGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))

	switch s := src.(type) {
	case *image.NRGBA:
		// Already NRGBA — straight copy if stride matches
		if s.Stride == w*4 {
			copy(dst.Pix, s.Pix[:w*h*4])
		} else {
			for y := 0; y < h; y++ {
				srcOff := (b.Min.Y+y-s.Rect.Min.Y)*s.Stride + (b.Min.X-s.Rect.Min.X)*4
				dstOff := y * dst.Stride
				copy(dst.Pix[dstOff:dstOff+w*4], s.Pix[srcOff:srcOff+w*4])
			}
		}
		return dst

	case *image.RGBA:
		// RGBA → NRGBA: un-premultiply alpha, parallelised
		nCPU := goruntime.NumCPU()
		var wg sync.WaitGroup
		rowsPer := (h + nCPU - 1) / nCPU
		for i := 0; i < nCPU; i++ {
			y0, y1 := i*rowsPer, (i+1)*rowsPer
			if y1 > h {
				y1 = h
			}
			if y0 >= y1 {
				break
			}
			wg.Add(1)
			go func(y0, y1 int) {
				defer wg.Done()
				for y := y0; y < y1; y++ {
					srcOff := (b.Min.Y+y-s.Rect.Min.Y)*s.Stride + (b.Min.X-s.Rect.Min.X)*4
					dstOff := y * dst.Stride
					for x := 0; x < w; x++ {
						si := srcOff + x*4
						di := dstOff + x*4
						a := uint32(s.Pix[si+3])
						switch a {
						case 0:
							// leave dst as zero
						case 255:
							dst.Pix[di] = s.Pix[si]
							dst.Pix[di+1] = s.Pix[si+1]
							dst.Pix[di+2] = s.Pix[si+2]
							dst.Pix[di+3] = 255
						default:
							dst.Pix[di] = uint8(uint32(s.Pix[si]) * 255 / a)
							dst.Pix[di+1] = uint8(uint32(s.Pix[si+1]) * 255 / a)
							dst.Pix[di+2] = uint8(uint32(s.Pix[si+2]) * 255 / a)
							dst.Pix[di+3] = uint8(a)
						}
					}
				}
			}(y0, y1)
		}
		wg.Wait()
		return dst

	default:
		// Generic path — parallelised
		nCPU := goruntime.NumCPU()
		var wg sync.WaitGroup
		rowsPer := (h + nCPU - 1) / nCPU
		for i := 0; i < nCPU; i++ {
			y0, y1 := i*rowsPer, (i+1)*rowsPer
			if y1 > h {
				y1 = h
			}
			if y0 >= y1 {
				break
			}
			wg.Add(1)
			go func(y0, y1 int) {
				defer wg.Done()
				for y := y0; y < y1; y++ {
					dstOff := y * dst.Stride
					for x := 0; x < w; x++ {
						r, g, bl, a := src.At(x+b.Min.X, y+b.Min.Y).RGBA()
						di := dstOff + x*4
						switch a {
						case 0:
							// leave as zero
						case 0xffff:
							dst.Pix[di] = uint8(r >> 8)
							dst.Pix[di+1] = uint8(g >> 8)
							dst.Pix[di+2] = uint8(bl >> 8)
							dst.Pix[di+3] = 0xff
						default:
							dst.Pix[di] = uint8(((r * 0xffff) / a) >> 8)
							dst.Pix[di+1] = uint8(((g * 0xffff) / a) >> 8)
							dst.Pix[di+2] = uint8(((bl * 0xffff) / a) >> 8)
							dst.Pix[di+3] = uint8(a >> 8)
						}
					}
				}
			}(y0, y1)
		}
		wg.Wait()
		return dst
	}
}

// cloneImage returns a deep copy of an NRGBA image.
func cloneImage(src *image.NRGBA) *image.NRGBA {
	b := src.Bounds()
	dst := image.NewNRGBA(b)
	copy(dst.Pix, src.Pix)
	return dst
}

// subImage extracts a sub-rectangle as a new independent image.
func subImage(src *image.NRGBA, r image.Rectangle) *image.NRGBA {
	r = r.Intersect(src.Bounds())
	dst := image.NewNRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	draw.Draw(dst, dst.Bounds(), src, r.Min, draw.Src)
	return dst
}

// imageToBase64 encodes an image as a base64 data URI for the frontend.
// Large images are downscaled to fit within maxPreviewDim and encoded as
// JPEG for speed; smaller images use PNG for quality.
func imageToBase64(img *image.NRGBA) (string, error) {
	const maxPreviewDim = 1600
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()

	var encImg image.Image = img
	if w > maxPreviewDim || h > maxPreviewDim {
		scale := float64(maxPreviewDim) / float64(w)
		if float64(maxPreviewDim)/float64(h) < scale {
			scale = float64(maxPreviewDim) / float64(h)
		}
		nw := int(float64(w) * scale)
		nh := int(float64(h) * scale)
		encImg = resizeNRGBA(img, nw, nh)
	}

	var buf bytes.Buffer
	// Use JPEG for speed on any non-trivial image
	if err := jpeg.Encode(&buf, encImg, &jpeg.Options{Quality: 85}); err != nil {
		return "", err
	}
	return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

// ---- Grayscale conversion ----

// toGrayscale converts an NRGBA image to grayscale using luminance weights.
func toGrayscale(src *image.NRGBA) *image.Gray {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dst := image.NewGray(image.Rect(0, 0, w, h))
	srcStride := src.Stride
	nCPU := goruntime.NumCPU()
	pFor(h, nCPU, func(start, end int) {
		for rowIdx := start; rowIdx < end; rowIdx++ {
			srcBase := rowIdx * srcStride
			dstBase := rowIdx * w
			for colIdx := 0; colIdx < w; colIdx++ {
				off := srcBase + colIdx*4
				r := uint32(src.Pix[off])
				g := uint32(src.Pix[off+1])
				bl := uint32(src.Pix[off+2])
				dst.Pix[dstBase+colIdx] = uint8((19595*r + 38470*g + 7471*bl + 32768) >> 16)
			}
		}
	})
	return dst
}

// stretchGrayPercentiles remaps the grayscale values so that the lowPct
// percentile maps to 0 and the highPct percentile maps to 255. Useful as
// a pre-processing step to boost contrast on images with non-white
// backgrounds or clipped histograms.
func stretchGrayPercentiles(src *image.Gray, lowPct, highPct float64) *image.Gray {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w == 0 || h == 0 {
		return src
	}

	var hist [256]int
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			hist[src.GrayAt(x, y).Y]++
		}
	}

	total := w * h
	if total == 0 {
		return src
	}

	// Clamp percentiles
	if lowPct < 0 {
		lowPct = 0
	}
	if highPct > 1 {
		highPct = 1
	}
	if lowPct >= highPct {
		return src
	}

	lowCount := int(float64(total) * lowPct)
	highCount := int(float64(total) * highPct)

	cum := 0
	vlow := 0
	for i := 0; i < 256; i++ {
		cum += hist[i]
		if cum >= lowCount {
			vlow = i
			break
		}
	}
	cum = 0
	vhigh := 255
	for i := 0; i < 256; i++ {
		cum += hist[i]
		if cum >= highCount {
			vhigh = i
			break
		}
	}

	if vlow >= vhigh {
		// Both percentiles collapsed to the same bin — this happens when the
		// object of interest occupies < 1% of the image (e.g. a small card on a
		// large dark background). Fall back to the actual data range so the
		// stretch is not silently skipped.
		actualMin, actualMax := 255, 0
		for i, c := range hist {
			if c > 0 {
				if i < actualMin {
					actualMin = i
				}
				if i > actualMax {
					actualMax = i
				}
			}
		}
		if actualMin >= actualMax {
			return src
		}
		vlow, vhigh = actualMin, actualMax
	}

	dst := image.NewGray(image.Rect(0, 0, w, h))
	scale := 255.0 / float64(vhigh-vlow)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := int(src.GrayAt(x, y).Y)
			mapped := int(float64(v-vlow) * scale)
			dst.SetGray(x, y, color.Gray{Y: clampByte(mapped)})
		}
	}
	return dst
}

// ---- Accent adjustment (brightness shift) ----

// applyAccentAdjustment shifts all pixel values by accentValue, clamping to [0,255].
func applyAccentAdjustment(src *image.NRGBA, accentValue int) *image.NRGBA {
	if accentValue == 0 {
		return cloneImage(src)
	}
	dst := cloneImage(src)
	for i := 0; i < len(dst.Pix); i += 4 {
		dst.Pix[i+0] = clampByte(int(dst.Pix[i+0]) + accentValue)
		dst.Pix[i+1] = clampByte(int(dst.Pix[i+1]) + accentValue)
		dst.Pix[i+2] = clampByte(int(dst.Pix[i+2]) + accentValue)
	}
	return dst
}

// ---- Levels adjustment (Auto Contrast) ----

// computeAutoContrastPoints scans all pixels for the minimum and maximum
// luminance and returns them as black/white points, matching the behaviour
// of Photoshop's Image > Auto Contrast. Fully-transparent pixels are
// skipped. Falls back to (0, 255) for flat/empty images to avoid a
// divide-by-zero in applyLevels.
func computeAutoContrastPoints(src *image.NRGBA) (blackPt, whitePt int) {
	minL, maxL := 255, 0
	b := src.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := src.NRGBAAt(x, y)
			if c.A == 0 {
				continue
			}
			r, g, bl := int(c.R), int(c.G), int(c.B)
			// ITU-R BT.601 luma (integer approximation)
			lum := (19595*r + 38470*g + 7471*bl) >> 16
			if lum < minL {
				minL = lum
			}
			if lum > maxL {
				maxL = lum
			}
		}
	}
	if minL >= maxL {
		return 0, 255 // flat or empty image â€” no-op stretch
	}
	return minL, maxL
}

// applyLevels stretches each colour channel linearly so that blackPt maps
// to 0 and whitePt maps to 255, clamping out-of-range values. Alpha is
// preserved unchanged. Equivalent to Photoshop's Levels black/white points.
func applyLevels(src *image.NRGBA, blackPt, whitePt int) *image.NRGBA {
	if blackPt >= whitePt {
		return cloneImage(src)
	}
	dst := cloneImage(src)
	scale := 255.0 / float64(whitePt-blackPt)
	for i := 0; i < len(dst.Pix); i += 4 {
		dst.Pix[i+0] = clampByte(int(float64(int(dst.Pix[i+0])-blackPt) * scale))
		dst.Pix[i+1] = clampByte(int(float64(int(dst.Pix[i+1])-blackPt) * scale))
		dst.Pix[i+2] = clampByte(int(float64(int(dst.Pix[i+2])-blackPt) * scale))
		// dst.Pix[i+3] â€” alpha untouched
	}
	return dst
}

// ---- CLAHE (Contrast Limited Adaptive Histogram Equalization) ----

// applyCLAHE applies CLAHE to a grayscale image.
func applyCLAHE(src *image.Gray, clipLimit float64, tileSize int) *image.Gray {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dst := image.NewGray(image.Rect(0, 0, w, h))

	tw := (w + tileSize - 1) / tileSize
	th := (h + tileSize - 1) / tileSize

	type tileCDF struct {
		cdf [256]float64
	}
	cdfs := make([]tileCDF, tw*th)

	srcStride := src.Stride
	dstStride := dst.Stride
	for ty := 0; ty < th; ty++ {
		for tx := 0; tx < tw; tx++ {
			x0 := tx * w / tw
			y0 := ty * h / th
			x1 := (tx + 1) * w / tw
			y1 := (ty + 1) * h / th

			var hist [256]int
			n := 0
			for yy := y0; yy < y1; yy++ {
				srcRow := yy * srcStride
				for xx := x0; xx < x1; xx++ {
					hist[src.Pix[srcRow+xx]]++
					n++
				}
			}

			limit := int(clipLimit * float64(n) / 256.0)
			if limit < 1 {
				limit = 1
			}
			excess := 0
			for i := range hist {
				if hist[i] > limit {
					excess += hist[i] - limit
					hist[i] = limit
				}
			}
			add := excess / 256
			for i := range hist {
				hist[i] += add
			}

			var cdf [256]float64
			cum := 0
			total := 0
			for i := range hist {
				total += hist[i]
			}
			if total == 0 {
				total = 1
			}
			for i := range hist {
				cum += hist[i]
				cdf[i] = float64(cum) / float64(total)
			}
			cdfs[ty*tw+tx] = tileCDF{cdf: cdf}
		}
	}

	nCPU := goruntime.NumCPU()
	pFor(h, nCPU, func(start, end int) {
		for y := start; y < end; y++ {
			fy := (float64(y)/float64(h))*float64(th) - 0.5
			ty0 := int(math.Floor(fy))
			ty1 := ty0 + 1
			wy := fy - float64(ty0)
			if ty0 < 0 {
				ty0 = 0
				wy = 0
			}
			if ty1 >= th {
				ty1 = th - 1
				wy = 0
			}
			srcRow := y * srcStride
			dstRow := y * dstStride
			for x := 0; x < w; x++ {
				fx := (float64(x)/float64(w))*float64(tw) - 0.5
				tx0 := int(math.Floor(fx))
				tx1 := tx0 + 1
				wx := fx - float64(tx0)
				if tx0 < 0 {
					tx0 = 0
					wx = 0
				}
				if tx1 >= tw {
					tx1 = tw - 1
					wx = 0
				}

				v := src.Pix[srcRow+x]
				c00 := cdfs[ty0*tw+tx0].cdf[v]
				c10 := cdfs[ty0*tw+tx1].cdf[v]
				c01 := cdfs[ty1*tw+tx0].cdf[v]
				c11 := cdfs[ty1*tw+tx1].cdf[v]

				top := c00*(1-wx) + c10*wx
				bot := c01*(1-wx) + c11*wx
				val := top*(1-wy) + bot*wy

				dst.Pix[dstRow+x] = uint8(clampByte(int(val * 255)))
			}
		}
	})

	return dst
}

// ---- Shi-Tomasi corner detection (goodFeaturesToTrack) ----

// gaussianBlurGray applies a separable 3-tap Gaussian blur [1 2 1]/4 to a
// grayscale image with border replication. Pre-smoothing before gradient
// computation suppresses noise-driven false corner responses.
func gaussianBlurGray(src *image.Gray) *image.Gray {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w < 3 || h < 3 {
		return src
	}

	tmp := image.NewGray(image.Rect(0, 0, w, h))
	// Horizontal pass: [1 2 1] / 4
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			x0 := clamp(x-1, 0, w-1)
			x1 := clamp(x+1, 0, w-1)
			v := int(src.GrayAt(x0, y).Y) + 2*int(src.GrayAt(x, y).Y) + int(src.GrayAt(x1, y).Y)
			tmp.SetGray(x, y, color.Gray{Y: uint8(v / 4)})
		}
	}
	dst := image.NewGray(image.Rect(0, 0, w, h))
	// Vertical pass: [1 2 1] / 4
	for y := 0; y < h; y++ {
		y0 := clamp(y-1, 0, h-1)
		y1 := clamp(y+1, 0, h-1)
		for x := 0; x < w; x++ {
			v := int(tmp.GrayAt(x, y0).Y) + 2*int(tmp.GrayAt(x, y).Y) + int(tmp.GrayAt(x, y1).Y)
			dst.SetGray(x, y, color.Gray{Y: uint8(v / 4)})
		}
	}
	return dst
}

// goodFeaturesToTrack implements the Shi-Tomasi corner detector in pure Go.
func goodFeaturesToTrack(ctx context.Context, gray *image.Gray, maxCorners int, qualityLevel float64, minDistance int, blockSize int) ([]image.Point, error) {
	b := gray.Bounds()
	w, h := b.Dx(), b.Dy()

	// Pre-smooth to suppress noise-driven gradient responses before computing
	// the structure tensor. This mirrors the standard OpenCV implementation.
	gray = gaussianBlurGray(gray)

	nCPU := goruntime.NumCPU()
	pix := gray.Pix
	stride := gray.Stride

	// ---- Sobel gradients (parallel) --------------------------------
	// Read directly from gray.Pix to avoid per-call bounds checks.
	ix := make([]float64, w*h)
	iy := make([]float64, w*h)

	pFor(h-2, nCPU, func(start, end int) {
		for row := start + 1; row <= end; row++ {
			for col := 1; col < w-1; col++ {
				p00 := float64(pix[(row-1)*stride+(col-1)])
				p01 := float64(pix[(row-1)*stride+col])
				p02 := float64(pix[(row-1)*stride+(col+1)])
				p10 := float64(pix[row*stride+(col-1)])
				p12 := float64(pix[row*stride+(col+1)])
				p20 := float64(pix[(row+1)*stride+(col-1)])
				p21 := float64(pix[(row+1)*stride+col])
				p22 := float64(pix[(row+1)*stride+(col+1)])
				ix[row*w+col] = -p00 - 2*p10 - p20 + p02 + 2*p12 + p22
				iy[row*w+col] = -p00 - 2*p01 - p02 + p20 + 2*p21 + p22
			}
		}
	})

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// ---- Summed-area tables for ix², iy², ix·iy -------------------
	// SAT is (h+1)×(w+1), 1-indexed: sat[(y+1)*(w+1)+(x+1)] = Σf[0..y][0..x].
	// Building it is O(w*h) sequential and very cache-friendly.
	sw1 := w + 1
	satN := (h + 1) * sw1
	satXX := make([]float64, satN)
	satYY := make([]float64, satN)
	satXY := make([]float64, satN)

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			gx := ix[y*w+x]
			gy := iy[y*w+x]
			i := (y+1)*sw1 + (x + 1)
			satXX[i] = gx*gx + satXX[i-1] + satXX[i-sw1] - satXX[i-sw1-1]
			satYY[i] = gy*gy + satYY[i-1] + satYY[i-sw1] - satYY[i-sw1-1]
			satXY[i] = gx*gy + satXY[i-1] + satXY[i-sw1] - satXY[i-sw1-1]
		}
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// ---- Structure tensor + min eigenvalue (parallel) --------------
	// Each pixel's window sum is now 4 SAT lookups — O(1) per pixel
	// regardless of blockSize, vs the previous O(blockSize²) inner loop.
	half := blockSize / 2
	cornerMap := make([]float64, w*h)

	nWorkers := nCPU
	validRows := h - 2*half
	if validRows < 1 {
		validRows = 1
	}
	if nWorkers > validRows {
		nWorkers = validRows
	}

	localMax := make([]float64, nWorkers)
	workerErrs := make([]error, nWorkers)
	chunk := (validRows + nWorkers - 1) / nWorkers

	var wg sync.WaitGroup
	for wid := 0; wid < nWorkers; wid++ {
		wid := wid
		rowStart := half + wid*chunk
		rowEnd := rowStart + chunk
		if rowEnd > h-half {
			rowEnd = h - half
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := ctx.Err(); err != nil {
				workerErrs[wid] = err
				return
			}
			lmax := 0.0
			for y := rowStart; y < rowEnd; y++ {
				r1 := y - half     // SAT top boundary (0-indexed)
				r2p1 := y + half + 1 // SAT bottom boundary + 1
				for x := half; x < w-half; x++ {
					c1 := x - half
					c2p1 := x + half + 1
					i11 := r2p1*sw1 + c2p1
					i01 := r1*sw1 + c2p1
					i10 := r2p1*sw1 + c1
					i00 := r1*sw1 + c1
					sxx := satXX[i11] - satXX[i01] - satXX[i10] + satXX[i00]
					syy := satYY[i11] - satYY[i01] - satYY[i10] + satYY[i00]
					sxy := satXY[i11] - satXY[i01] - satXY[i10] + satXY[i00]

					trace := sxx + syy
					det := sxx*syy - sxy*sxy
					disc := trace*trace/4.0 - det
					if disc < 0 {
						disc = 0
					}
					minEig := trace/2.0 - math.Sqrt(disc)
					cornerMap[y*w+x] = minEig
					if minEig > lmax {
						lmax = minEig
					}
				}
			}
			localMax[wid] = lmax
		}()
	}
	wg.Wait()

	for _, err := range workerErrs {
		if err != nil {
			return nil, err
		}
	}

	maxEig := 0.0
	for _, v := range localMax {
		if v > maxEig {
			maxEig = v
		}
	}

	threshold := maxEig * qualityLevel

	type candidate struct {
		pt  image.Point
		val float64
	}
	var candidates []candidate
	for y := half; y < h-half; y++ {
		for x := half; x < w-half; x++ {
			v := cornerMap[y*w+x]
			if v > threshold {
				candidates = append(candidates, candidate{image.Pt(x, y), v})
			}
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].val > candidates[j].val
	})

	minDistSq := float64(minDistance * minDistance)
	var result []image.Point
	for _, c := range candidates {
		if len(result) >= maxCorners {
			break
		}
		tooClose := false
		for _, r := range result {
			dx := float64(c.pt.X - r.X)
			dy := float64(c.pt.Y - r.Y)
			if dx*dx+dy*dy < minDistSq {
				tooClose = true
				break
			}
		}
		if !tooClose {
			result = append(result, c.pt)
		}
	}

	return result, nil
}

// ---- Perspective transform ----

// perspectiveTransform applies a 4-point perspective warp in pure Go.
func perspectiveTransform(src *image.NRGBA, srcPts, dstPts [4]image.Point, outW, outH int) *image.NRGBA {
	H := computeHomography(
		[4][2]float64{
			{float64(srcPts[0].X), float64(srcPts[0].Y)},
			{float64(srcPts[1].X), float64(srcPts[1].Y)},
			{float64(srcPts[2].X), float64(srcPts[2].Y)},
			{float64(srcPts[3].X), float64(srcPts[3].Y)},
		},
		[4][2]float64{
			{float64(dstPts[0].X), float64(dstPts[0].Y)},
			{float64(dstPts[1].X), float64(dstPts[1].Y)},
			{float64(dstPts[2].X), float64(dstPts[2].Y)},
			{float64(dstPts[3].X), float64(dstPts[3].Y)},
		},
	)

	Hinv := invert3x3(H)

	dst := image.NewNRGBA(image.Rect(0, 0, outW, outH))
	sb := src.Bounds()

	for y := 0; y < outH; y++ {
		for x := 0; x < outW; x++ {
			dx, dy := float64(x)+0.5, float64(y)+0.5
			w := Hinv[6]*dx + Hinv[7]*dy + Hinv[8]
			if math.Abs(w) < 1e-12 {
				continue
			}
			sx := (Hinv[0]*dx + Hinv[1]*dy + Hinv[2]) / w
			sy := (Hinv[3]*dx + Hinv[4]*dy + Hinv[5]) / w

			ix0 := int(math.Floor(sx))
			iy0 := int(math.Floor(sy))
			ffx := sx - float64(ix0)
			ffy := sy - float64(iy0)

			// Clamp coordinates to valid range to avoid transparent pixels at edges
			ix0c := clamp(ix0, sb.Min.X, sb.Max.X-2)
			iy0c := clamp(iy0, sb.Min.Y, sb.Max.Y-2)
			c00 := src.NRGBAAt(ix0c, iy0c)
			c10 := src.NRGBAAt(ix0c+1, iy0c)
			c01 := src.NRGBAAt(ix0c, iy0c+1)
			c11 := src.NRGBAAt(ix0c+1, iy0c+1)

			r := bilinear(float64(c00.R), float64(c10.R), float64(c01.R), float64(c11.R), ffx, ffy)
			g := bilinear(float64(c00.G), float64(c10.G), float64(c01.G), float64(c11.G), ffx, ffy)
			bl := bilinear(float64(c00.B), float64(c10.B), float64(c01.B), float64(c11.B), ffx, ffy)
			al := bilinear(float64(c00.A), float64(c10.A), float64(c01.A), float64(c11.A), ffx, ffy)

			dst.SetNRGBA(x, y, color.NRGBA{
				R: clampByte(int(r)),
				G: clampByte(int(g)),
				B: clampByte(int(bl)),
				A: clampByte(int(al)),
			})
		}
	}

	return dst
}

// perspectiveTransformWithMask is like perspectiveTransform but instead of
// clamping out-of-bounds source coordinates it leaves those destination pixels
// transparent and records them in the returned alpha mask (255 = OOB).
// Callers can then decide how to fill the masked region.
func perspectiveTransformWithMask(src *image.NRGBA, srcPts, dstPts [4]image.Point, outW, outH int) (*image.NRGBA, *image.Alpha) {
	H := computeHomography(
		[4][2]float64{
			{float64(srcPts[0].X), float64(srcPts[0].Y)},
			{float64(srcPts[1].X), float64(srcPts[1].Y)},
			{float64(srcPts[2].X), float64(srcPts[2].Y)},
			{float64(srcPts[3].X), float64(srcPts[3].Y)},
		},
		[4][2]float64{
			{float64(dstPts[0].X), float64(dstPts[0].Y)},
			{float64(dstPts[1].X), float64(dstPts[1].Y)},
			{float64(dstPts[2].X), float64(dstPts[2].Y)},
			{float64(dstPts[3].X), float64(dstPts[3].Y)},
		},
	)

	Hinv := invert3x3(H)

	dst := image.NewNRGBA(image.Rect(0, 0, outW, outH))
	oob := image.NewAlpha(image.Rect(0, 0, outW, outH))
	sb := src.Bounds()

	for y := 0; y < outH; y++ {
		for x := 0; x < outW; x++ {
			dx, dy := float64(x)+0.5, float64(y)+0.5
			w := Hinv[6]*dx + Hinv[7]*dy + Hinv[8]
			if math.Abs(w) < 1e-12 {
				oob.Pix[y*oob.Stride+x] = 255
				continue
			}
			sx := (Hinv[0]*dx + Hinv[1]*dy + Hinv[2]) / w
			sy := (Hinv[3]*dx + Hinv[4]*dy + Hinv[5]) / w

			ix0 := int(math.Floor(sx))
			iy0 := int(math.Floor(sy))

			// Mark pixel as out-of-bounds if bilinear neighbourhood is outside src.
			if ix0 < sb.Min.X || ix0 > sb.Max.X-2 || iy0 < sb.Min.Y || iy0 > sb.Max.Y-2 {
				oob.Pix[y*oob.Stride+x] = 255
				continue
			}

			ffx := sx - float64(ix0)
			ffy := sy - float64(iy0)

			c00 := src.NRGBAAt(ix0, iy0)
			c10 := src.NRGBAAt(ix0+1, iy0)
			c01 := src.NRGBAAt(ix0, iy0+1)
			c11 := src.NRGBAAt(ix0+1, iy0+1)

			r := bilinear(float64(c00.R), float64(c10.R), float64(c01.R), float64(c11.R), ffx, ffy)
			g := bilinear(float64(c00.G), float64(c10.G), float64(c01.G), float64(c11.G), ffx, ffy)
			bl := bilinear(float64(c00.B), float64(c10.B), float64(c01.B), float64(c11.B), ffx, ffy)
			al := bilinear(float64(c00.A), float64(c10.A), float64(c01.A), float64(c11.A), ffx, ffy)

			dst.SetNRGBA(x, y, color.NRGBA{
				R: clampByte(int(r)),
				G: clampByte(int(g)),
				B: clampByte(int(bl)),
				A: clampByte(int(al)),
			})
		}
	}

	return dst, oob
}

// ---- Rotation ----

// rotate90 rotates an image 90 degrees. flipCode 0 = CCW, 1 = CW.
// Uses direct Pix slice indexing (no bounds-checked method calls) and
// splits the work across all available CPU cores.
func rotate90(src *image.NRGBA, flipCode int) *image.NRGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dst := image.NewNRGBA(image.Rect(0, 0, h, w))

	nCPU := goruntime.NumCPU()
	var wg sync.WaitGroup
	rowsPer := (h + nCPU - 1) / nCPU
	for i := 0; i < nCPU; i++ {
		y0, y1 := i*rowsPer, (i+1)*rowsPer
		if y1 > h {
			y1 = h
		}
		if y0 >= y1 {
			break
		}
		wg.Add(1)
		go func(y0, y1 int) {
			defer wg.Done()
			for y := y0; y < y1; y++ {
				srcRowBase := (b.Min.Y+y)*src.Stride + b.Min.X*4
				for x := 0; x < w; x++ {
					si := srcRowBase + x*4
					var di int
					if flipCode == 1 {
						// CW: src(x,y) → dst(h-1-y, x)
						di = x*dst.Stride + (h-1-y)*4
					} else {
						// CCW: src(x,y) → dst(y, w-1-x)
						di = (w-1-x)*dst.Stride + y*4
					}
					dst.Pix[di+0] = src.Pix[si+0]
					dst.Pix[di+1] = src.Pix[si+1]
					dst.Pix[di+2] = src.Pix[si+2]
					dst.Pix[di+3] = src.Pix[si+3]
				}
			}
		}(y0, y1)
	}
	wg.Wait()
	return dst
}

// rotateArbitrary rotates an image by an arbitrary angle in degrees.
func rotateArbitrary(src *image.NRGBA, angleDeg float64, bg color.NRGBA) *image.NRGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	cx, cy := float64(w)/2.0, float64(h)/2.0

	rad := angleDeg * math.Pi / 180.0
	cosA := math.Cos(rad)
	sinA := math.Sin(rad)

	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(dst.Pix); i += 4 {
		dst.Pix[i+0] = bg.R
		dst.Pix[i+1] = bg.G
		dst.Pix[i+2] = bg.B
		dst.Pix[i+3] = bg.A
	}

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			ddx := float64(x) - cx
			ddy := float64(y) - cy
			sx := cosA*ddx + sinA*ddy + cx
			sy := -sinA*ddx + cosA*ddy + cy

			ix0 := int(math.Floor(sx))
			iy0 := int(math.Floor(sy))
			if ix0 < 0 || ix0+1 >= w || iy0 < 0 || iy0+1 >= h {
				continue
			}
			fx := sx - float64(ix0)
			fy := sy - float64(iy0)

			c00 := src.NRGBAAt(b.Min.X+ix0, b.Min.Y+iy0)
			c10 := src.NRGBAAt(b.Min.X+ix0+1, b.Min.Y+iy0)
			c01 := src.NRGBAAt(b.Min.X+ix0, b.Min.Y+iy0+1)
			c11 := src.NRGBAAt(b.Min.X+ix0+1, b.Min.Y+iy0+1)

			dst.SetNRGBA(x, y, color.NRGBA{
				R: clampByte(int(bilinear(float64(c00.R), float64(c10.R), float64(c01.R), float64(c11.R), fx, fy))),
				G: clampByte(int(bilinear(float64(c00.G), float64(c10.G), float64(c01.G), float64(c11.G), fx, fy))),
				B: clampByte(int(bilinear(float64(c00.B), float64(c10.B), float64(c01.B), float64(c11.B), fx, fy))),
				A: clampByte(int(bilinear(float64(c00.A), float64(c10.A), float64(c01.A), float64(c11.A), fx, fy))),
			})
		}
	}
	return dst
}

// ---- Circular mask with feathering ----

// applyCircularMaskWithFeather masks the image to a disc with a smooth feathered edge.
// If centerCutoutRadius > 0, a feathered circular hole of that radius is punched out
// at the centre and filled with bg, so the background eyedropper colour shows through.
// The cutout feather width matches featherSize, transitioning from bg at the centre
// outward to full image colour at cutoutRadius + featherSize.
func applyCircularMaskWithFeather(src *image.NRGBA, center image.Point, radius, featherSize, centerCutoutRadius int, bg color.NRGBA) *image.NRGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))

	outerR := float64(radius + featherSize)
	innerR := float64(radius)
	cutoutR := float64(centerCutoutRadius)
	cutoutFeatherR := cutoutR + float64(featherSize)

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			ddx := float64(x) - float64(center.X)
			ddy := float64(y) - float64(center.Y)
			d := math.Sqrt(ddx*ddx + ddy*ddy)

			var alpha float64
			if d >= outerR {
				alpha = 0.0
			} else if d > innerR {
				// Outer feather: 1 → 0
				t := (d - innerR) / float64(featherSize)
				alpha = 0.5 * (1 + math.Cos(t*math.Pi))
			} else if cutoutR <= 0 {
				alpha = 1.0
			} else if d <= cutoutR {
				// Inside cutout hard core — bg.
				alpha = 0.0
			} else if d < cutoutFeatherR {
				// Cutout feather: 0 → 1 as d goes from cutoutR to cutoutFeatherR.
				t := (d - cutoutR) / float64(featherSize)
				alpha = 0.5 * (1 - math.Cos(t*math.Pi))
			} else {
				alpha = 1.0
			}

			sc := src.NRGBAAt(b.Min.X+x, b.Min.Y+y)
			dst.SetNRGBA(x, y, color.NRGBA{
				R: clampByte(int(float64(sc.R)*alpha + float64(bg.R)*(1-alpha))),
				G: clampByte(int(float64(sc.G)*alpha + float64(bg.G)*(1-alpha))),
				B: clampByte(int(float64(sc.B)*alpha + float64(bg.B)*(1-alpha))),
				A: 255,
			})
		}
	}
	return dst
}

// ---- Drawing ----

// drawFilledCircle draws a filled circle onto an NRGBA image.
func drawFilledCircle(img *image.NRGBA, center image.Point, radius int, c color.NRGBA) {
	b := img.Bounds()
	r2 := radius * radius
	for y := center.Y - radius; y <= center.Y+radius; y++ {
		for x := center.X - radius; x <= center.X+radius; x++ {
			dx := x - center.X
			dy := y - center.Y
			if dx*dx+dy*dy <= r2 && x >= b.Min.X && x < b.Max.X && y >= b.Min.Y && y < b.Max.Y {
				img.SetNRGBA(x, y, c)
			}
		}
	}
}

// ---- Resize ----

// resizeGray resizes a grayscale image. Downsampling uses area averaging (box
// filter) to preserve edge energy; upsampling falls back to nearest-neighbor.
func resizeGray(src *image.Gray, newW, newH int) *image.Gray {
	b := src.Bounds()
	origW, origH := b.Dx(), b.Dy()
	if newW <= 0 || newH <= 0 {
		return src
	}
	dst := image.NewGray(image.Rect(0, 0, newW, newH))

	if newW < origW || newH < origH {
		// Area averaging: each output pixel averages the block of source pixels
		// that map onto it. This prevents aliasing from destroying edge gradients
		// at the downsampled scales used by multi-scale corner detection.
		srcStride := src.Stride
		dstStride := dst.Stride
		nCPU := goruntime.NumCPU()
		pFor(newH, nCPU, func(start, end int) {
			for y := start; y < end; y++ {
				srcY0 := y * origH / newH
				srcY1 := (y + 1) * origH / newH
				if srcY1 > origH {
					srcY1 = origH
				}
				if srcY1 == srcY0 {
					srcY1 = srcY0 + 1
				}
				dstRow := y * dstStride
				for x := 0; x < newW; x++ {
					srcX0 := x * origW / newW
					srcX1 := (x + 1) * origW / newW
					if srcX1 > origW {
						srcX1 = origW
					}
					if srcX1 == srcX0 {
						srcX1 = srcX0 + 1
					}
					sum, count := 0, 0
					for sy := srcY0; sy < srcY1; sy++ {
						srcRow := sy * srcStride
						for sx := srcX0; sx < srcX1; sx++ {
							sum += int(src.Pix[srcRow+sx])
							count++
						}
					}
					dst.Pix[dstRow+x] = uint8(sum / count)
				}
			}
		})
		return dst
	}

	// Nearest-neighbor for upsampling (not used in the current detection pipeline).
	for y := 0; y < newH; y++ {
		sy := y * origH / newH
		for x := 0; x < newW; x++ {
			sx := x * origW / newW
			dst.SetGray(x, y, src.GrayAt(sx, sy))
		}
	}
	return dst
}

// resizeNRGBAToGray downsamples an NRGBA image to a grayscale image in a
// single parallelized pass, combining accent adjustment, luma conversion, and
// area-averaging. This avoids the large intermediate NRGBA clone and full-res
// gray buffer that the three-step pipeline (applyAccentAdjustment +
// toGrayscale + resizeGray) would allocate.
func resizeNRGBAToGray(src *image.NRGBA, newW, newH, accentValue int) *image.Gray {
	b := src.Bounds()
	origW, origH := b.Dx(), b.Dy()
	dst := image.NewGray(image.Rect(0, 0, newW, newH))
	if newW <= 0 || newH <= 0 {
		return dst
	}
	srcStride := src.Stride
	dstStride := dst.Stride
	nCPU := goruntime.NumCPU()
	pFor(newH, nCPU, func(start, end int) {
		for outY := start; outY < end; outY++ {
			srcY0 := outY * origH / newH
			srcY1 := (outY + 1) * origH / newH
			if srcY1 > origH {
				srcY1 = origH
			}
			if srcY1 == srcY0 {
				srcY1 = srcY0 + 1
			}
			dstRow := outY * dstStride
			for outX := 0; outX < newW; outX++ {
				srcX0 := outX * origW / newW
				srcX1 := (outX + 1) * origW / newW
				if srcX1 > origW {
					srcX1 = origW
				}
				if srcX1 == srcX0 {
					srcX1 = srcX0 + 1
				}
				sum, count := 0, 0
				for sy := srcY0; sy < srcY1; sy++ {
					srcRow := sy * srcStride
					for sx := srcX0; sx < srcX1; sx++ {
						off := srcRow + sx*4
						r := uint32(clampByte(int(src.Pix[off]) + accentValue))
						g := uint32(clampByte(int(src.Pix[off+1]) + accentValue))
						bl := uint32(clampByte(int(src.Pix[off+2]) + accentValue))
						sum += int((19595*r + 38470*g + 7471*bl + 32768) >> 16)
						count++
					}
				}
				if count > 0 {
					dst.Pix[dstRow+outX] = uint8(sum / count)
				}
			}
		}
	})
	return dst
}

// resizeNRGBA resizes an NRGBA image using nearest-neighbor interpolation.
func resizeNRGBA(src *image.NRGBA, newW, newH int) *image.NRGBA {
	b := src.Bounds()
	origW, origH := b.Dx(), b.Dy()
	if newW <= 0 || newH <= 0 {
		return src
	}
	dst := image.NewNRGBA(image.Rect(0, 0, newW, newH))
	nCPU := goruntime.NumCPU()
	var wg sync.WaitGroup
	rowsPer := (newH + nCPU - 1) / nCPU
	for i := 0; i < nCPU; i++ {
		y0, y1 := i*rowsPer, (i+1)*rowsPer
		if y1 > newH {
			y1 = newH
		}
		if y0 >= y1 {
			break
		}
		wg.Add(1)
		go func(y0, y1 int) {
			defer wg.Done()
			for y := y0; y < y1; y++ {
				sy := b.Min.Y + y*origH/newH
				srcRow := sy * src.Stride
				dstRow := y * dst.Stride
				for x := 0; x < newW; x++ {
					sx := b.Min.X + x*origW/newW
					si := srcRow + sx*4
					di := dstRow + x*4
					dst.Pix[di] = src.Pix[si]
					dst.Pix[di+1] = src.Pix[si+1]
					dst.Pix[di+2] = src.Pix[si+2]
					dst.Pix[di+3] = src.Pix[si+3]
				}
			}
		}(y0, y1)
	}
	wg.Wait()
	return dst
}
