package main

import (
	"math"
	"sync"
)

// pFor divides [0, total) into at most workers contiguous chunks and waits
// until every chunk has completed.
func pFor(total, workers int, fn func(start, end int)) {
	if workers <= 1 || total <= 1 {
		fn(0, total)
		return
	}
	if workers > total {
		workers = total
	}

	var wg sync.WaitGroup
	chunk := (total + workers - 1) / workers
	for worker := 0; worker < workers; worker++ {
		start := worker * chunk
		if start >= total {
			break
		}
		end := start + chunk
		if end > total {
			end = total
		}
		wg.Add(1)
		go func(start, end int) {
			defer wg.Done()
			fn(start, end)
		}(start, end)
	}
	wg.Wait()
}

// dilate2dFFT applies a rectangular max filter with replicated borders.
// The two monotonic-queue passes make its cost independent of the radius.
func dilate2dFFT(src []float32, rows, cols, kw, kh, workers int) []float32 {
	dst := make([]float32, rows*cols)
	if kw <= 0 && kh <= 0 {
		copy(dst, src)
		return dst
	}
	tmp := make([]float32, rows*cols)
	dilateHorizontalFFT(src, tmp, rows, cols, kw, workers)
	dilateVerticalFFT(tmp, dst, rows, cols, kh, workers)
	return dst
}

// dilate2dFFTInPlace is the allocation-free form used by the production
// descreen path. scratch must be at least rows*cols elements long.
func dilate2dFFTInPlace(data, scratch []float32, rows, cols, kw, kh, workers int) {
	if kw <= 0 && kh <= 0 {
		return
	}
	dilateHorizontalFFT(data, scratch, rows, cols, kw, workers)
	dilateVerticalFFT(scratch, data, rows, cols, kh, workers)
}

// dilateBinaryMaskFFTInPlace specializes dilation for the 0/255 threshold
// mask. A running count is cheaper than a general sliding maximum.
func dilateBinaryMaskFFTInPlace(data, scratch []float32, rows, cols, kw, kh, workers int) {
	if kw <= 0 && kh <= 0 {
		return
	}

	if kw > 0 {
		pFor(rows, workers, func(start, end int) {
			for y := start; y < end; y++ {
				row := data[y*cols : (y+1)*cols]
				out := scratch[y*cols : (y+1)*cols]
				count := 0
				for dx := -kw; dx <= kw; dx++ {
					x := dx
					if x < 0 {
						x = 0
					} else if x >= cols {
						x = cols - 1
					}
					if row[x] != 0 {
						count++
					}
				}
				for x := 0; x < cols; x++ {
					if count != 0 {
						out[x] = 255
					} else {
						out[x] = 0
					}
					left := x - kw
					if left < 0 {
						left = 0
					} else if left >= cols {
						left = cols - 1
					}
					right := x + kw + 1
					if right < 0 {
						right = 0
					} else if right >= cols {
						right = cols - 1
					}
					if row[left] != 0 {
						count--
					}
					if row[right] != 0 {
						count++
					}
				}
			}
		})
	} else {
		copy(scratch, data)
	}

	if kh > 0 {
		pFor(cols, workers, func(start, end int) {
			counts := make([]int, end-start)
			for dy := -kh; dy <= kh; dy++ {
				y := dy
				if y < 0 {
					y = 0
				} else if y >= rows {
					y = rows - 1
				}
				row := scratch[y*cols+start : y*cols+end]
				for i, value := range row {
					if value != 0 {
						counts[i]++
					}
				}
			}
			for y := 0; y < rows; y++ {
				out := data[y*cols+start : y*cols+end]
				for i, count := range counts {
					if count != 0 {
						out[i] = 255
					} else {
						out[i] = 0
					}
				}

				top := y - kh
				if top < 0 {
					top = 0
				} else if top >= rows {
					top = rows - 1
				}
				bottom := y + kh + 1
				if bottom < 0 {
					bottom = 0
				} else if bottom >= rows {
					bottom = rows - 1
				}
				remove := scratch[top*cols+start : top*cols+end]
				add := scratch[bottom*cols+start : bottom*cols+end]
				for i := range counts {
					if remove[i] != 0 {
						counts[i]--
					}
					if add[i] != 0 {
						counts[i]++
					}
				}
			}
		})
	} else {
		copy(data, scratch)
	}
}

func dilateHorizontalFFT(src, dst []float32, rows, cols, radius, workers int) {
	if radius <= 0 {
		copy(dst, src)
		return
	}
	pFor(rows, workers, func(start, end int) {
		queue := make([]int, cols+2*radius)
		for y := start; y < end; y++ {
			row := src[y*cols : (y+1)*cols]
			out := dst[y*cols : (y+1)*cols]
			head, tail := 0, 0
			for virtual := -radius; virtual < cols+radius; virtual++ {
				x := virtual
				if x < 0 {
					x = 0
				} else if x >= cols {
					x = cols - 1
				}
				for tail > head {
					qx := queue[tail-1]
					clamped := qx
					if clamped < 0 {
						clamped = 0
					} else if clamped >= cols {
						clamped = cols - 1
					}
					if row[clamped] > row[x] {
						break
					}
					tail--
				}
				queue[tail] = virtual
				tail++
				oldest := virtual - 2*radius
				for tail > head && queue[head] < oldest {
					head++
				}
				outputX := virtual - radius
				if outputX >= 0 {
					qx := queue[head]
					if qx < 0 {
						qx = 0
					} else if qx >= cols {
						qx = cols - 1
					}
					out[outputX] = row[qx]
				}
			}
		}
	})
}

func dilateVerticalFFT(src, dst []float32, rows, cols, radius, workers int) {
	if radius <= 0 {
		copy(dst, src)
		return
	}
	pFor(cols, workers, func(start, end int) {
		queue := make([]int, rows+2*radius)
		for x := start; x < end; x++ {
			head, tail := 0, 0
			for virtual := -radius; virtual < rows+radius; virtual++ {
				y := virtual
				if y < 0 {
					y = 0
				} else if y >= rows {
					y = rows - 1
				}
				for tail > head {
					qy := queue[tail-1]
					clamped := qy
					if clamped < 0 {
						clamped = 0
					} else if clamped >= rows {
						clamped = rows - 1
					}
					if src[clamped*cols+x] > src[y*cols+x] {
						break
					}
					tail--
				}
				queue[tail] = virtual
				tail++
				oldest := virtual - 2*radius
				for tail > head && queue[head] < oldest {
					head++
				}
				outputY := virtual - radius
				if outputY >= 0 {
					qy := queue[head]
					if qy < 0 {
						qy = 0
					} else if qy >= rows {
						qy = rows - 1
					}
					dst[outputY*cols+x] = src[qy*cols+x]
				}
			}
		}
	})
}

func gaussianBlur2dFFT(src []float32, rows, cols int, sigma float64, workers int) []float32 {
	if sigma <= 0 {
		return src
	}
	dst := make([]float32, rows*cols)
	scratch := make([]float32, rows*cols)
	copy(dst, src)
	gaussianBlur2dFFTInPlace(dst, scratch, rows, cols, sigma, workers)
	return dst
}

func gaussianBlur2dFFTInPlace(data, scratch []float32, rows, cols int, sigma float64, workers int) {
	if sigma <= 0 {
		return
	}
	kernel := gaussianKernelFFT(sigma)
	gaussianBlurHorizontalFFTInto(data, scratch, rows, cols, kernel, workers)
	gaussianBlurVerticalFFTInto(scratch, data, rows, cols, kernel, workers)
}

func gaussianKernelFFT(sigma float64) []float64 {
	radius := int(math.Ceil(3 * sigma))
	kernel := make([]float64, 2*radius+1)
	sum := 0.0
	for i := range kernel {
		distance := float64(i - radius)
		weight := math.Exp(-distance * distance / (2 * sigma * sigma))
		kernel[i] = weight
		sum += weight
	}
	for i := range kernel {
		kernel[i] /= sum
	}
	return kernel
}

func gaussianBlurHorizontalFFTInto(src, dst []float32, rows, cols int, kernel []float64, workers int) {
	radius := len(kernel) / 2
	pFor(rows, workers, func(start, end int) {
		for y := start; y < end; y++ {
			row := src[y*cols : (y+1)*cols]
			out := dst[y*cols : (y+1)*cols]
			for x := 0; x < cols; x++ {
				acc := float64(row[x]) * kernel[radius]
				for distance := 1; distance <= radius; distance++ {
					left, right := x-distance, x+distance
					if left < 0 {
						left = 0
					}
					if right >= cols {
						right = cols - 1
					}
					acc += (float64(row[left]) + float64(row[right])) * kernel[radius+distance]
				}
				out[x] = float32(acc)
			}
		}
	})
}

func gaussianBlurVerticalFFTInto(src, dst []float32, rows, cols int, kernel []float64, workers int) {
	radius := len(kernel) / 2
	pFor(rows, workers, func(start, end int) {
		for y := start; y < end; y++ {
			out := dst[y*cols : (y+1)*cols]
			center := src[y*cols : (y+1)*cols]
			for x := 0; x < cols; x++ {
				acc := float64(center[x]) * kernel[radius]
				for distance := 1; distance <= radius; distance++ {
					top, bottom := y-distance, y+distance
					if top < 0 {
						top = 0
					}
					if bottom >= rows {
						bottom = rows - 1
					}
					acc += (float64(src[top*cols+x]) + float64(src[bottom*cols+x])) * kernel[radius+distance]
				}
				out[x] = float32(acc)
			}
		}
	})
}
