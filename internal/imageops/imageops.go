// Package imageops provides state-free image adjustments and geometry transforms.
package imageops

import (
	"image"
	"image/color"
	"math"
	goruntime "runtime"
	"sync"

	"atropos/internal/geometry"
	"atropos/internal/parallel"
	"atropos/internal/raster"
)

// ============================================================
// IMAGE OPERATIONS: pure Go adjustments, perspective warp,
// rotation, masking, and drawing.
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

// ---- Grayscale adjustment ----

// StretchGrayPercentiles remaps the grayscale values so that the lowPct
// percentile maps to 0 and the highPct percentile maps to 255. Useful as
// a pre-processing step to boost contrast on images with non-white
// backgrounds or clipped histograms.
func StretchGrayPercentiles(src *image.Gray, lowPct, highPct float64) *image.Gray {
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
			dst.SetGray(x, y, color.Gray{Y: raster.ClampByte(mapped)})
		}
	}
	return dst
}

// ---- Accent adjustment (brightness shift) ----

// applyAccentAdjustment shifts all pixel values by accentValue, clamping to [0,255].
func applyAccentAdjustment(src *image.NRGBA, accentValue int) *image.NRGBA {
	if accentValue == 0 {
		return raster.CloneNRGBA(src)
	}
	dst := raster.CloneNRGBA(src)
	for i := 0; i < len(dst.Pix); i += 4 {
		dst.Pix[i+0] = raster.ClampByte(int(dst.Pix[i+0]) + accentValue)
		dst.Pix[i+1] = raster.ClampByte(int(dst.Pix[i+1]) + accentValue)
		dst.Pix[i+2] = raster.ClampByte(int(dst.Pix[i+2]) + accentValue)
	}
	return dst
}

// ---- Levels adjustment (Auto Contrast) ----

// AutoContrastPoints scans all pixels for the minimum and maximum
// luminance and returns them as black/white points, matching the behaviour
// of Photoshop's Image > Auto Contrast. Fully-transparent pixels are
// skipped. Falls back to (0, 255) for flat/empty images to avoid a
// divide-by-zero in ApplyLevels.
func AutoContrastPoints(src *image.NRGBA) (blackPt, whitePt int) {
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

// ApplyLevels stretches each colour channel linearly so that blackPt maps
// to 0 and whitePt maps to 255, clamping out-of-range values. Alpha is
// preserved unchanged. Equivalent to Photoshop's Levels black/white points.
func ApplyLevels(src *image.NRGBA, blackPt, whitePt int) *image.NRGBA {
	if blackPt >= whitePt {
		return raster.CloneNRGBA(src)
	}
	dst := raster.CloneNRGBA(src)
	scale := 255.0 / float64(whitePt-blackPt)
	raster.ApplyLevelsPixels(dst.Pix, blackPt, scale)
	return dst
}

// ---- CLAHE (Contrast Limited Adaptive Histogram Equalization) ----

// ApplyCLAHE applies CLAHE to a grayscale image.
func ApplyCLAHE(src *image.Gray, clipLimit float64, tileSize int) *image.Gray {
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
	parallel.For(h, nCPU, func(start, end int) {
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

				dst.Pix[dstRow+x] = uint8(raster.ClampByte(int(val * 255)))
			}
		}
	})

	return dst
}

// ---- Perspective transform ----

// PerspectiveTransform applies a 4-point perspective warp in pure Go.
func PerspectiveTransform(src *image.NRGBA, srcPts, dstPts [4]image.Point, outW, outH int) *image.NRGBA {
	H := geometry.ComputeHomography(
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

	Hinv := geometry.Invert3x3(H)

	dst := image.NewNRGBA(image.Rect(0, 0, outW, outH))
	sb := src.Bounds()

	nCPU := goruntime.NumCPU()
	parallel.For(outH, nCPU, func(start, end int) {
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
					dst.Pix[di+channel] = raster.ClampByte(int(geometry.Bilinear(
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

// PerspectiveTransformWithMask is like PerspectiveTransform but instead of
// clamping out-of-bounds source coordinates it leaves those destination pixels
// transparent and records them in the returned alpha mask (255 = OOB).
// Callers can then decide how to fill the masked region.
func PerspectiveTransformWithMask(src *image.NRGBA, srcPts, dstPts [4]image.Point, outW, outH int) (*image.NRGBA, *image.Alpha) {
	H := geometry.ComputeHomography(
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

	Hinv := geometry.Invert3x3(H)

	dst := image.NewNRGBA(image.Rect(0, 0, outW, outH))
	oob := image.NewAlpha(image.Rect(0, 0, outW, outH))
	sb := src.Bounds()

	nCPU := goruntime.NumCPU()
	parallel.For(outH, nCPU, func(start, end int) {
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
					dst.Pix[di+channel] = raster.ClampByte(int(geometry.Bilinear(
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

// Rotate90 rotates an image 90 degrees. flipCode 0 = CCW, 1 = CW.
// Uses direct Pix slice indexing (no bounds-checked method calls) and
// splits the work across all available CPU cores.
func Rotate90(src *image.NRGBA, flipCode int) *image.NRGBA {
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

// Rotate rotates an image by an arbitrary angle in degrees.
func Rotate(src *image.NRGBA, angleDeg float64, bg color.NRGBA) *image.NRGBA {
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
	parallel.For(h, nCPU, func(start, end int) {
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
					dst.Pix[di+channel] = raster.ClampByte(int(geometry.Bilinear(
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

// ApplyCircularMask masks the image to a disc with a smooth feathered edge.
// If centerCutoutRadius > 0, a feathered circular hole of that radius is punched out
// at the centre and filled with bg, so the background eyedropper colour shows through.
// The cutout feather width matches featherSize, transitioning from bg at the centre
// outward to full image colour at cutoutRadius + featherSize.
func ApplyCircularMask(src *image.NRGBA, center image.Point, radius, featherSize, centerCutoutRadius int, bg color.NRGBA) *image.NRGBA {
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
	parallel.For(h, nCPU, func(start, end int) {
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
			raster.BlendMaskRow(
				src.Pix[srcOffset:srcOffset+w*4], dst.Pix[dstOffset:dstOffset+w*4],
				alphaRow, bg.R, bg.G, bg.B,
			)
		}
	})
	return dst
}

// ---- Drawing ----

// DrawFilledCircle draws a filled circle onto an NRGBA image.
func DrawFilledCircle(img *image.NRGBA, center image.Point, radius int, c color.NRGBA) {
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
