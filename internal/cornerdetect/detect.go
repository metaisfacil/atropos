// Package cornerdetect implements Shi-Tomasi feature detection and its
// architecture-optimized image kernels.
package cornerdetect

import (
	"context"
	"image"
	"runtime"
	"sync"
)

// Options configures Shi-Tomasi corner detection.
type Options struct {
	MaxCorners   int
	QualityLevel float64
	MinDistance  int
	BlockSize    int
}

// BlurGray applies a separable 3-tap Gaussian blur [1 2 1]/4 with replicated
// borders.
func BlurGray(src *image.Gray) *image.Gray {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w < 3 || h < 3 {
		return src
	}

	tmp := image.NewGray(image.Rect(0, 0, w, h))
	nCPU := runtime.NumCPU()
	workers := nCPU
	if w*h < 65536 {
		workers = 1
	}
	parallelFor(h, workers, func(start, end int) {
		for y := start; y < end; y++ {
			srcRow := src.Pix[y*src.Stride : y*src.Stride+w]
			dstRow := tmp.Pix[y*tmp.Stride : y*tmp.Stride+w]
			dstRow[0] = uint8((3*int(srcRow[0]) + int(srcRow[1])) / 4)
			cornerBlurRow(srcRow[:w-2], srcRow[1:w-1], srcRow[2:], dstRow[1:w-1])
			dstRow[w-1] = uint8((int(srcRow[w-2]) + 3*int(srcRow[w-1])) / 4)
		}
	})
	dst := image.NewGray(image.Rect(0, 0, w, h))
	parallelFor(h, workers, func(start, end int) {
		for y := start; y < end; y++ {
			y0 := max(y-1, 0)
			y1 := min(y+1, h-1)
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

// Detect returns spatially separated Shi-Tomasi corners in descending response
// order.
func Detect(ctx context.Context, gray *image.Gray, options Options) ([]image.Point, error) {
	b := gray.Bounds()
	w, h := b.Dx(), b.Dy()
	if w < options.BlockSize || h < options.BlockSize {
		return nil, nil
	}

	gray = BlurGray(gray)
	nCPU := runtime.NumCPU()
	pix := gray.Pix
	stride := gray.Stride
	half := options.BlockSize / 2
	tensorXX := make([]int32, w*h)
	tensorYY := make([]int32, w*h)
	tensorXY := make([]int32, w*h)
	parallelFor(h, nCPU, func(start, end int) {
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
				ix, iy, options.BlockSize,
				tensorXX[rowBase:rowBase+w],
				tensorYY[rowBase:rowBase+w],
				tensorXY[rowBase:rowBase+w],
			)
		}
	})

	if err := ctx.Err(); err != nil {
		return nil, err
	}

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
		rowEnd := min(rowStart+chunk, h-half)
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
	for _, value := range localMax {
		if value > maxEig {
			maxEig = value
		}
	}

	threshold := maxEig * options.QualityLevel
	candidates := make([]cornerCandidate, 0)
	for y := half; y < h-half; y++ {
		for x := half; x < w-half; x++ {
			value := cornerMap[y*w+x]
			if value > threshold {
				candidates = append(candidates, cornerCandidate{pt: image.Pt(x, y), val: value})
			}
		}
	}

	return selectSpacedCorners(candidates, options.MaxCorners, options.MinDistance), nil
}

func parallelFor(total, workers int, fn func(start, end int)) {
	if workers <= 1 || total <= 1 {
		fn(0, total)
		return
	}
	if workers > total {
		workers = total
	}
	chunk := (total + workers - 1) / workers
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		start := worker * chunk
		if start >= total {
			break
		}
		end := min(start+chunk, total)
		wg.Add(1)
		go func(start, end int) {
			defer wg.Done()
			fn(start, end)
		}(start, end)
	}
	wg.Wait()
}
