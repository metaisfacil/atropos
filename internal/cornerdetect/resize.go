package cornerdetect

import (
	"image"
	"runtime"
)

// ResizeGray resizes a grayscale image. Downsampling uses area averaging to
// preserve edge energy; upsampling uses nearest-neighbor interpolation.
func ResizeGray(src *image.Gray, newW, newH int) *image.Gray {
	b := src.Bounds()
	origW, origH := b.Dx(), b.Dy()
	if newW <= 0 || newH <= 0 {
		return src
	}
	dst := image.NewGray(image.Rect(0, 0, newW, newH))

	if newW < origW || newH < origH {
		if factor := origW / newW; (factor == 2 || factor == 4) &&
			origW == newW*factor && origH == newH*factor {
			parallelFor(newH, runtime.NumCPU(), func(start, end int) {
				for y := start; y < end; y++ {
					cornerResizeGrayRow(
						src.Pix[y*factor*src.Stride:], src.Stride, factor,
						dst.Pix[y*dst.Stride:y*dst.Stride+newW],
					)
				}
			})
			return dst
		}

		srcStride := src.Stride
		dstStride := dst.Stride
		parallelFor(newH, runtime.NumCPU(), func(start, end int) {
			for y := start; y < end; y++ {
				srcY0 := y * origH / newH
				srcY1 := min((y+1)*origH/newH, origH)
				if srcY1 == srcY0 {
					srcY1 = srcY0 + 1
				}
				dstRow := y * dstStride
				for x := 0; x < newW; x++ {
					srcX0 := x * origW / newW
					srcX1 := min((x+1)*origW/newW, origW)
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

	for y := 0; y < newH; y++ {
		sy := y * origH / newH
		for x := 0; x < newW; x++ {
			sx := x * origW / newW
			dst.SetGray(x, y, src.GrayAt(sx, sy))
		}
	}
	return dst
}
