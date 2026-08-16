package main

import (
	"context"
	"image"
	"image/color"
	"image/draw"
	"math"
	goruntime "runtime"
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

// ---- Grayscale conversion ----

// toGrayscale converts an NRGBA image to grayscale using luminance weights.
func toGrayscale(src *image.NRGBA) *image.Gray {
	return toGrayscaleAccent(src, 0)
}

func toGrayscaleAccent(src *image.NRGBA, accent int) *image.Gray {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dst := image.NewGray(image.Rect(0, 0, w, h))
	srcStride := src.Stride
	nCPU := goruntime.NumCPU()
	pFor(h, nCPU, func(start, end int) {
		for rowIdx := start; rowIdx < end; rowIdx++ {
			srcBase := rowIdx * srcStride
			dstBase := rowIdx * w
			grayscaleAccentRow(src.Pix[srcBase:srcBase+w*4], dst.Pix[dstBase:dstBase+w], accent)
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
	applyLevelsPixels(dst.Pix, blackPt, scale)
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
	nCPU := goruntime.NumCPU()
	blurWorkers := nCPU
	if w*h < 65536 {
		blurWorkers = 1
	}
	pFor(h, blurWorkers, func(start, end int) {
		for y := start; y < end; y++ {
			srcRow := src.Pix[y*src.Stride : y*src.Stride+w]
			dstRow := tmp.Pix[y*tmp.Stride : y*tmp.Stride+w]
			dstRow[0] = uint8((3*int(srcRow[0]) + int(srcRow[1])) / 4)
			cornerBlurRow(srcRow[:w-2], srcRow[1:w-1], srcRow[2:], dstRow[1:w-1])
			dstRow[w-1] = uint8((int(srcRow[w-2]) + 3*int(srcRow[w-1])) / 4)
		}
	})
	dst := image.NewGray(image.Rect(0, 0, w, h))
	// Vertical pass: [1 2 1] / 4
	pFor(h, blurWorkers, func(start, end int) {
		for y := start; y < end; y++ {
			y0 := clamp(y-1, 0, h-1)
			y1 := clamp(y+1, 0, h-1)
			cornerBlurRow(
				tmp.Pix[y0*tmp.Stride:y0*tmp.Stride+w],
				tmp.Pix[y*tmp.Stride:y*tmp.Stride+w],
				tmp.Pix[y1*tmp.Stride:y1*tmp.Stride+w],
				dst.Pix[y*dst.Stride:y*dst.Stride+w],
			)
		}
	})
	return dst
}

// goodFeaturesToTrack implements the Shi-Tomasi corner detector in pure Go.
func goodFeaturesToTrack(ctx context.Context, gray *image.Gray, maxCorners int, qualityLevel float64, minDistance int, blockSize int) ([]image.Point, error) {
	b := gray.Bounds()
	w, h := b.Dx(), b.Dy()
	if w < blockSize || h < blockSize {
		return nil, nil
	}

	// Pre-smooth to suppress noise-driven gradient responses before computing
	// the structure tensor. This mirrors the standard OpenCV implementation.
	gray = gaussianBlurGray(gray)

	nCPU := goruntime.NumCPU()
	pix := gray.Pix
	stride := gray.Stride

	// ---- Sobel + horizontal tensor sums (parallel) -----------------
	// Each worker reuses one gradient row. Only the three exact int32 horizontal
	// tensor planes are retained, avoiding full-image gradient buffers and the
	// former three float64 summed-area tables.
	half := blockSize / 2
	tensorXX := make([]int32, w*h)
	tensorYY := make([]int32, w*h)
	tensorXY := make([]int32, w*h)
	pFor(h, nCPU, func(start, end int) {
		ix := make([]int16, w)
		iy := make([]int16, w)
		for row := start; row < end; row++ {
			if row == 0 || row == h-1 {
				clear(ix)
				clear(iy)
			} else {
				cornerSobelRow(
					pix[(row-1)*stride:],
					pix[row*stride:],
					pix[(row+1)*stride:],
					ix[1:w-1],
					iy[1:w-1],
				)
			}
			rowBase := row * w
			cornerHorizontalTensor(
				ix, iy, blockSize,
				tensorXX[rowBase:rowBase+w],
				tensorYY[rowBase:rowBase+w],
				tensorXY[rowBase:rowBase+w],
			)
		}
	})

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// ---- Vertical rolling sums + min eigenvalue (parallel) ----------
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
		if rowStart >= rowEnd {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := ctx.Err(); err != nil {
				workerErrs[wid] = err
				return
			}
			n := w - 2*half
			accXX := make([]int32, n)
			accYY := make([]int32, n)
			accXY := make([]int32, n)
			for sourceY := rowStart - half; sourceY <= rowStart+half; sourceY++ {
				base := sourceY*w + half
				for x := 0; x < n; x++ {
					accXX[x] += tensorXX[base+x]
					accYY[x] += tensorYY[base+x]
					accXY[x] += tensorXY[base+x]
				}
			}
			lmax := 0.0
			for y := rowStart; y < rowEnd; y++ {
				rowMax := cornerTensorEigenRow(accXX, accYY, accXY, cornerMap[y*w+half:y*w+half+n])
				if rowMax > lmax {
					lmax = rowMax
				}
				if y+1 < rowEnd {
					removeBase := (y-half)*w + half
					addBase := (y+half+1)*w + half
					for x := 0; x < n; x++ {
						accXX[x] += tensorXX[addBase+x] - tensorXX[removeBase+x]
						accYY[x] += tensorYY[addBase+x] - tensorYY[removeBase+x]
						accXY[x] += tensorXY[addBase+x] - tensorXY[removeBase+x]
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

	var candidates []cornerCandidate
	for y := half; y < h-half; y++ {
		for x := half; x < w-half; x++ {
			v := cornerMap[y*w+x]
			if v > threshold {
				candidates = append(candidates, cornerCandidate{image.Pt(x, y), v})
			}
		}
	}

	return selectSpacedCorners(candidates, maxCorners, minDistance), nil
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

	nCPU := goruntime.NumCPU()
	pFor(outH, nCPU, func(start, end int) {
		for y := start; y < end; y++ {
			dstRow := y * dst.Stride
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
				si := (iy0c-src.Rect.Min.Y)*src.Stride + (ix0c-src.Rect.Min.X)*4
				siBottom := si + src.Stride
				di := dstRow + x*4
				for channel := 0; channel < 4; channel++ {
					dst.Pix[di+channel] = clampByte(int(bilinear(
						float64(src.Pix[si+channel]), float64(src.Pix[si+4+channel]),
						float64(src.Pix[siBottom+channel]), float64(src.Pix[siBottom+4+channel]),
						ffx, ffy,
					)))
				}
			}
		}
	})

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

	nCPU := goruntime.NumCPU()
	pFor(outH, nCPU, func(start, end int) {
		for y := start; y < end; y++ {
			dstRow := y * dst.Stride
			maskRow := y * oob.Stride
			for x := 0; x < outW; x++ {
				dx, dy := float64(x)+0.5, float64(y)+0.5
				w := Hinv[6]*dx + Hinv[7]*dy + Hinv[8]
				if math.Abs(w) < 1e-12 {
					oob.Pix[maskRow+x] = 255
					continue
				}
				sx := (Hinv[0]*dx + Hinv[1]*dy + Hinv[2]) / w
				sy := (Hinv[3]*dx + Hinv[4]*dy + Hinv[5]) / w

				ix0 := int(math.Floor(sx))
				iy0 := int(math.Floor(sy))

				// Mark pixel as out-of-bounds if bilinear neighbourhood is outside src.
				if ix0 < sb.Min.X || ix0 > sb.Max.X-2 || iy0 < sb.Min.Y || iy0 > sb.Max.Y-2 {
					oob.Pix[maskRow+x] = 255
					continue
				}

				ffx := sx - float64(ix0)
				ffy := sy - float64(iy0)

				si := (iy0-src.Rect.Min.Y)*src.Stride + (ix0-src.Rect.Min.X)*4
				siBottom := si + src.Stride
				di := dstRow + x*4
				for channel := 0; channel < 4; channel++ {
					dst.Pix[di+channel] = clampByte(int(bilinear(
						float64(src.Pix[si+channel]), float64(src.Pix[si+4+channel]),
						float64(src.Pix[siBottom+channel]), float64(src.Pix[siBottom+4+channel]),
						ffx, ffy,
					)))
				}
			}
		}
	})

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
	if len(dst.Pix) >= 4 {
		dst.Pix[0], dst.Pix[1], dst.Pix[2], dst.Pix[3] = bg.R, bg.G, bg.B, bg.A
		for filled := 4; filled < len(dst.Pix); {
			copied := copy(dst.Pix[filled:], dst.Pix[:filled])
			filled += copied
		}
	}

	nCPU := goruntime.NumCPU()
	pFor(h, nCPU, func(start, end int) {
		for y := start; y < end; y++ {
			dstRow := y * dst.Stride
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

				si := iy0*src.Stride + ix0*4
				siBottom := si + src.Stride
				di := dstRow + x*4
				for channel := 0; channel < 4; channel++ {
					dst.Pix[di+channel] = clampByte(int(bilinear(
						float64(src.Pix[si+channel]), float64(src.Pix[si+4+channel]),
						float64(src.Pix[siBottom+channel]), float64(src.Pix[siBottom+4+channel]),
						fx, fy,
					)))
				}
			}
		}
	})
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
	outerR2 := outerR * outerR
	innerR2 := innerR * innerR
	cutoutR2 := cutoutR * cutoutR
	cutoutFeatherR2 := cutoutFeatherR * cutoutFeatherR

	nCPU := goruntime.NumCPU()
	pFor(h, nCPU, func(start, end int) {
		alphaRow := make([]float64, w)
		for y := start; y < end; y++ {
			for x := 0; x < w; x++ {
				ddx := float64(x) - float64(center.X)
				ddy := float64(y) - float64(center.Y)
				d2 := ddx*ddx + ddy*ddy

				var alpha float64
				if d2 >= outerR2 {
					alpha = 0.0
				} else if d2 > innerR2 {
					// Outer feather: 1 → 0
					d := math.Sqrt(d2)
					t := (d - innerR) / float64(featherSize)
					alpha = 0.5 * (1 + math.Cos(t*math.Pi))
				} else if cutoutR <= 0 {
					alpha = 1.0
				} else if d2 <= cutoutR2 {
					// Inside cutout hard core — bg.
					alpha = 0.0
				} else if d2 < cutoutFeatherR2 {
					// Cutout feather: 0 → 1 as d goes from cutoutR to cutoutFeatherR.
					d := math.Sqrt(d2)
					t := (d - cutoutR) / float64(featherSize)
					alpha = 0.5 * (1 - math.Cos(t*math.Pi))
				} else {
					alpha = 1.0
				}
				alphaRow[x] = alpha
			}
			srcOffset := (b.Min.Y+y-src.Rect.Min.Y)*src.Stride + (b.Min.X-src.Rect.Min.X)*4
			dstOffset := y * dst.Stride
			maskBlendRow(
				src.Pix[srcOffset:srcOffset+w*4], dst.Pix[dstOffset:dstOffset+w*4],
				alphaRow, bg.R, bg.G, bg.B,
			)
		}
	})
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
		if factor := origW / newW; (factor == 2 || factor == 4) &&
			origW == newW*factor && origH == newH*factor {
			nCPU := goruntime.NumCPU()
			pFor(newH, nCPU, func(start, end int) {
				for y := start; y < end; y++ {
					cornerResizeGrayRow(
						src.Pix[y*factor*src.Stride:], src.Stride, factor,
						dst.Pix[y*dst.Stride:y*dst.Stride+newW],
					)
				}
			})
			return dst
		}
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
	dst, _ := resizeNRGBAToGrayInternal(src, newW, newH, accentValue, false)
	return dst
}

// resizeNRGBAToGrayPair produces the normal accent-adjusted grayscale image
// and an unadjusted grayscale image in one source traversal. Corner detection
// uses the raw result for its highlight-boundary curve, which must retain
// distinctions that a positive accent would clip to white.
func resizeNRGBAToGrayPair(src *image.NRGBA, newW, newH, accentValue int) (*image.Gray, *image.Gray) {
	return resizeNRGBAToGrayInternal(src, newW, newH, accentValue, true)
}

func resizeNRGBAToGrayInternal(src *image.NRGBA, newW, newH, accentValue int, includeRaw bool) (*image.Gray, *image.Gray) {
	b := src.Bounds()
	origW, origH := b.Dx(), b.Dy()
	dst := image.NewGray(image.Rect(0, 0, newW, newH))
	var rawDst *image.Gray
	if includeRaw {
		rawDst = image.NewGray(image.Rect(0, 0, newW, newH))
	}
	if newW <= 0 || newH <= 0 {
		return dst, rawDst
	}
	if newW == origW && newH == origH {
		dst = toGrayscaleAccent(src, accentValue)
		if includeRaw {
			rawDst = toGrayscale(src)
		}
		return dst, rawDst
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
				sum, rawSum, count := 0, 0, 0
				for sy := srcY0; sy < srcY1; sy++ {
					srcRow := sy * srcStride
					for sx := srcX0; sx < srcX1; sx++ {
						off := srcRow + sx*4
						if includeRaw {
							rawR := uint32(src.Pix[off])
							rawG := uint32(src.Pix[off+1])
							rawB := uint32(src.Pix[off+2])
							rawSum += int((19595*rawR + 38470*rawG + 7471*rawB + 32768) >> 16)
						}
						r := uint32(clampByte(int(src.Pix[off]) + accentValue))
						g := uint32(clampByte(int(src.Pix[off+1]) + accentValue))
						bl := uint32(clampByte(int(src.Pix[off+2]) + accentValue))
						sum += int((19595*r + 38470*g + 7471*bl + 32768) >> 16)
						count++
					}
				}
				if count > 0 {
					dst.Pix[dstRow+outX] = uint8(sum / count)
					if includeRaw {
						rawDst.Pix[dstRow+outX] = uint8(rawSum / count)
					}
				}
			}
		}
	})
	return dst, rawDst
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
