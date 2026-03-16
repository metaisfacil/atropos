package main

import (
	"bytes"
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
						if a == 0 {
							// leave dst as zero
						} else if a == 255 {
							dst.Pix[di] = s.Pix[si]
							dst.Pix[di+1] = s.Pix[si+1]
							dst.Pix[di+2] = s.Pix[si+2]
							dst.Pix[di+3] = 255
						} else {
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
						if a == 0 {
							// leave as zero
						} else if a == 0xffff {
							dst.Pix[di] = uint8(r >> 8)
							dst.Pix[di+1] = uint8(g >> 8)
							dst.Pix[di+2] = uint8(bl >> 8)
							dst.Pix[di+3] = 0xff
						} else {
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
	dst := image.NewGray(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := src.At(x, y).RGBA()
			lum := uint8((19595*r + 38470*g + 7471*bl + 1<<15) >> 24)
			dst.SetGray(x-b.Min.X, y-b.Min.Y, color.Gray{Y: lum})
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

	for ty := 0; ty < th; ty++ {
		for tx := 0; tx < tw; tx++ {
			x0 := tx * w / tw
			y0 := ty * h / th
			x1 := (tx + 1) * w / tw
			y1 := (ty + 1) * h / th

			var hist [256]int
			n := 0
			for yy := y0; yy < y1; yy++ {
				for xx := x0; xx < x1; xx++ {
					hist[src.GrayAt(xx, yy).Y]++
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

	for y := 0; y < h; y++ {
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

			v := src.GrayAt(x, y).Y
			c00 := cdfs[ty0*tw+tx0].cdf[v]
			c10 := cdfs[ty0*tw+tx1].cdf[v]
			c01 := cdfs[ty1*tw+tx0].cdf[v]
			c11 := cdfs[ty1*tw+tx1].cdf[v]

			top := c00*(1-wx) + c10*wx
			bot := c01*(1-wx) + c11*wx
			val := top*(1-wy) + bot*wy

			dst.SetGray(x, y, color.Gray{Y: clampByte(int(val * 255))})
		}
	}

	return dst
}

// ---- Shi-Tomasi corner detection (goodFeaturesToTrack) ----

// goodFeaturesToTrack implements the Shi-Tomasi corner detector in pure Go.
func goodFeaturesToTrack(gray *image.Gray, maxCorners int, qualityLevel float64, minDistance int, blockSize int) []image.Point {
	b := gray.Bounds()
	w, h := b.Dx(), b.Dy()

	ix := make([]float64, w*h)
	iy := make([]float64, w*h)

	for y := 1; y < h-1; y++ {
		for x := 1; x < w-1; x++ {
			gx := -float64(gray.GrayAt(x-1, y-1).Y) - 2*float64(gray.GrayAt(x-1, y).Y) - float64(gray.GrayAt(x-1, y+1).Y) +
				float64(gray.GrayAt(x+1, y-1).Y) + 2*float64(gray.GrayAt(x+1, y).Y) + float64(gray.GrayAt(x+1, y+1).Y)
			gy := -float64(gray.GrayAt(x-1, y-1).Y) - 2*float64(gray.GrayAt(x, y-1).Y) - float64(gray.GrayAt(x+1, y-1).Y) +
				float64(gray.GrayAt(x-1, y+1).Y) + 2*float64(gray.GrayAt(x, y+1).Y) + float64(gray.GrayAt(x+1, y+1).Y)
			ix[y*w+x] = gx
			iy[y*w+x] = gy
		}
	}

	half := blockSize / 2
	cornerMap := make([]float64, w*h)
	maxEig := 0.0

	for y := half; y < h-half; y++ {
		for x := half; x < w-half; x++ {
			var sxx, syy, sxy float64
			for dy := -half; dy <= half; dy++ {
				for dx := -half; dx <= half; dx++ {
					idx := (y+dy)*w + (x + dx)
					gx := ix[idx]
					gy := iy[idx]
					sxx += gx * gx
					syy += gy * gy
					sxy += gx * gy
				}
			}

			trace := sxx + syy
			det := sxx*syy - sxy*sxy
			disc := trace*trace/4.0 - det
			if disc < 0 {
				disc = 0
			}
			minEigenvalue := trace/2.0 - math.Sqrt(disc)

			cornerMap[y*w+x] = minEigenvalue
			if minEigenvalue > maxEig {
				maxEig = minEigenvalue
			}
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

	return result
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

// ---- Rotation ----

// rotate90 rotates an image 90 degrees. flipCode 0 = CCW, 1 = CW.
func rotate90(src *image.NRGBA, flipCode int) *image.NRGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dst := image.NewNRGBA(image.Rect(0, 0, h, w))

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := src.NRGBAAt(b.Min.X+x, b.Min.Y+y)
			if flipCode == 1 {
				dst.SetNRGBA(h-1-y, x, c)
			} else {
				dst.SetNRGBA(y, w-1-x, c)
			}
		}
	}
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
func applyCircularMaskWithFeather(src *image.NRGBA, center image.Point, radius, featherSize int, bg color.NRGBA) *image.NRGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))

	outerR := float64(radius + featherSize)
	innerR := float64(radius)

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			ddx := float64(x) - float64(center.X)
			ddy := float64(y) - float64(center.Y)
			d := math.Sqrt(ddx*ddx + ddy*ddy)

			var alpha float64
			if d <= innerR {
				alpha = 1.0
			} else if d >= outerR {
				alpha = 0.0
			} else {
				t := (d - innerR) / float64(featherSize)
				alpha = 0.5 * (1 + math.Cos(t*math.Pi))
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

// resizeGray resizes a grayscale image using nearest-neighbor.
func resizeGray(src *image.Gray, newW, newH int) *image.Gray {
	b := src.Bounds()
	origW, origH := b.Dx(), b.Dy()
	if newW <= 0 || newH <= 0 {
		return src
	}
	dst := image.NewGray(image.Rect(0, 0, newW, newH))
	for y := 0; y < newH; y++ {
		sy := y * origH / newH
		for x := 0; x < newW; x++ {
			sx := x * origW / newW
			dst.SetGray(x, y, src.GrayAt(sx, sy))
		}
	}
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
