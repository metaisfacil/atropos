package raster

import (
	"image"
	"image/draw"
	"runtime"
	"sync"
)

// CloneNRGBA returns an independent image with the same bounds and pixels.
func CloneNRGBA(src *image.NRGBA) *image.NRGBA {
	dst := image.NewNRGBA(src.Bounds())
	copy(dst.Pix, src.Pix)
	return dst
}

// CropNRGBA returns an independent, zero-origin crop clipped to src bounds.
func CropNRGBA(src *image.NRGBA, rect image.Rectangle) *image.NRGBA {
	rect = rect.Intersect(src.Bounds())
	dst := image.NewNRGBA(image.Rect(0, 0, rect.Dx(), rect.Dy()))
	draw.Draw(dst, dst.Bounds(), src, rect.Min, draw.Src)
	return dst
}

// ToNRGBA converts any image to an independent, zero-origin NRGBA image.
func ToNRGBA(src image.Image) *image.NRGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))

	switch source := src.(type) {
	case *image.NRGBA:
		if source.Stride == w*4 {
			copy(dst.Pix, source.Pix[:w*h*4])
		} else {
			for y := 0; y < h; y++ {
				srcOff := (b.Min.Y+y-source.Rect.Min.Y)*source.Stride + (b.Min.X-source.Rect.Min.X)*4
				dstOff := y * dst.Stride
				copy(dst.Pix[dstOff:dstOff+w*4], source.Pix[srcOff:srcOff+w*4])
			}
		}
		return dst

	case *image.RGBA:
		parallelFor(h, runtime.NumCPU(), func(start, end int) {
			for y := start; y < end; y++ {
				srcOff := (b.Min.Y+y-source.Rect.Min.Y)*source.Stride + (b.Min.X-source.Rect.Min.X)*4
				dstOff := y * dst.Stride
				for x := 0; x < w; x++ {
					si := srcOff + x*4
					di := dstOff + x*4
					a := uint32(source.Pix[si+3])
					switch a {
					case 0:
					case 255:
						dst.Pix[di] = source.Pix[si]
						dst.Pix[di+1] = source.Pix[si+1]
						dst.Pix[di+2] = source.Pix[si+2]
						dst.Pix[di+3] = 255
					default:
						dst.Pix[di] = uint8(uint32(source.Pix[si]) * 255 / a)
						dst.Pix[di+1] = uint8(uint32(source.Pix[si+1]) * 255 / a)
						dst.Pix[di+2] = uint8(uint32(source.Pix[si+2]) * 255 / a)
						dst.Pix[di+3] = uint8(a)
					}
				}
			}
		})
		return dst

	default:
		parallelFor(h, runtime.NumCPU(), func(start, end int) {
			for y := start; y < end; y++ {
				dstOff := y * dst.Stride
				for x := 0; x < w; x++ {
					r, g, b, a := src.At(x+b.Min.X, y+b.Min.Y).RGBA()
					di := dstOff + x*4
					switch a {
					case 0:
					case 0xffff:
						dst.Pix[di] = uint8(r >> 8)
						dst.Pix[di+1] = uint8(g >> 8)
						dst.Pix[di+2] = uint8(b >> 8)
						dst.Pix[di+3] = 0xff
					default:
						dst.Pix[di] = uint8(((r * 0xffff) / a) >> 8)
						dst.Pix[di+1] = uint8(((g * 0xffff) / a) >> 8)
						dst.Pix[di+2] = uint8(((b * 0xffff) / a) >> 8)
						dst.Pix[di+3] = uint8(a >> 8)
					}
				}
			}
		})
		return dst
	}
}

// ToGrayscale converts NRGBA pixels to luminance.
func ToGrayscale(src *image.NRGBA) *image.Gray {
	return ToGrayscaleAccent(src, 0)
}

// ToGrayscaleAccent adjusts RGB channels before converting to luminance.
func ToGrayscaleAccent(src *image.NRGBA, accent int) *image.Gray {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dst := image.NewGray(image.Rect(0, 0, w, h))
	parallelFor(h, runtime.NumCPU(), func(start, end int) {
		for row := start; row < end; row++ {
			srcBase := row * src.Stride
			dstBase := row * w
			grayscaleAccentRow(src.Pix[srcBase:srcBase+w*4], dst.Pix[dstBase:dstBase+w], accent)
		}
	})
	return dst
}

// ResizeNRGBAToGray combines area resampling, optional accent adjustment, and
// luminance conversion.
func ResizeNRGBAToGray(src *image.NRGBA, newW, newH, accent int) *image.Gray {
	dst, _ := resizeNRGBAToGray(src, newW, newH, accent, false)
	return dst
}

// ResizeNRGBAToGrayPair also returns an unadjusted grayscale raster.
func ResizeNRGBAToGrayPair(src *image.NRGBA, newW, newH, accent int) (*image.Gray, *image.Gray) {
	return resizeNRGBAToGray(src, newW, newH, accent, true)
}

func resizeNRGBAToGray(src *image.NRGBA, newW, newH, accent int, includeRaw bool) (*image.Gray, *image.Gray) {
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
		dst = ToGrayscaleAccent(src, accent)
		if includeRaw {
			rawDst = ToGrayscale(src)
		}
		return dst, rawDst
	}
	parallelFor(newH, runtime.NumCPU(), func(start, end int) {
		for outY := start; outY < end; outY++ {
			srcY0 := outY * origH / newH
			srcY1 := min((outY+1)*origH/newH, origH)
			if srcY1 == srcY0 {
				srcY1 = srcY0 + 1
			}
			dstRow := outY * dst.Stride
			for outX := 0; outX < newW; outX++ {
				srcX0 := outX * origW / newW
				srcX1 := min((outX+1)*origW/newW, origW)
				if srcX1 == srcX0 {
					srcX1 = srcX0 + 1
				}
				sum, rawSum, count := 0, 0, 0
				for sy := srcY0; sy < srcY1; sy++ {
					srcRow := sy * src.Stride
					for sx := srcX0; sx < srcX1; sx++ {
						off := srcRow + sx*4
						if includeRaw {
							r := uint32(src.Pix[off])
							g := uint32(src.Pix[off+1])
							b := uint32(src.Pix[off+2])
							rawSum += int((19595*r + 38470*g + 7471*b + 32768) >> 16)
						}
						r := uint32(ClampByte(int(src.Pix[off]) + accent))
						g := uint32(ClampByte(int(src.Pix[off+1]) + accent))
						b := uint32(ClampByte(int(src.Pix[off+2]) + accent))
						sum += int((19595*r + 38470*g + 7471*b + 32768) >> 16)
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

// ResizeNRGBA resizes using nearest-neighbor interpolation.
func ResizeNRGBA(src *image.NRGBA, newW, newH int) *image.NRGBA {
	b := src.Bounds()
	origW, origH := b.Dx(), b.Dy()
	if newW <= 0 || newH <= 0 {
		return src
	}
	dst := image.NewNRGBA(image.Rect(0, 0, newW, newH))
	var wg sync.WaitGroup
	workers := runtime.NumCPU()
	rowsPer := (newH + workers - 1) / workers
	for worker := 0; worker < workers; worker++ {
		y0, y1 := worker*rowsPer, min((worker+1)*rowsPer, newH)
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
					copy(dst.Pix[di:di+4], src.Pix[si:si+4])
				}
			}
		}(y0, y1)
	}
	wg.Wait()
	return dst
}
