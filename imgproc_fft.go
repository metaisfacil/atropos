package main

import (
	"image"
	"math"
	"runtime"
	"sync"
)

// ============================================================
// FFT-BASED DESCREEN FILTER
// Implements an FFT magnitude-spectrum notch filter that
// suppresses halftone screen patterns (moire / dot patterns)
// from scanned printed images.
//
// Algorithm mirrors the Python/OpenCV reference script:
//   1. Pad each channel to the next power-of-2 dimensions.
//   2. Compute 2D DFT and shift DC to centre.
//   3. Compute a distance-weighted log-magnitude spectrum.
//   4. Threshold bright peaks; protect the DC neighbourhood
//      with an elliptical middle-preservation mask.
//   5. Dilate and Gaussian-blur the binary peak mask.
//   6. Build a suppression filter  (1 – mask/255)  and multiply
//      it element-wise into the complex FFT plane.
//   7. Inverse-shift and inverse-DFT; take magnitudes.
//
// Parallelism:
//   The three colour channels are processed concurrently.
//   Within each channel, expensive steps (FFT row/column
//   passes, dilation, Gaussian blur, pixel-level loops) are
//   further split across nCPU worker goroutines.
// ============================================================

// ---- Parallel helper ----------------------------------------

// pFor divides the integer range [0, total) into at most workers
// contiguous chunks and calls fn(start, end) for each chunk
// concurrently.  It blocks until all chunks have finished.
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
	for w := 0; w < workers; w++ {
		s := w * chunk
		if s >= total {
			break
		}
		e := s + chunk
		if e > total {
			e = total
		}
		wg.Add(1)
		go func(s, e int) {
			defer wg.Done()
			fn(s, e)
		}(s, e)
	}
	wg.Wait()
}

// ---- 1-D Cooley-Tukey radix-2 FFT -------------------------

// fft1d performs an in-place 1-D FFT (or IFFT when invert=true).
// len(x) must be a power of 2.
func fft1d(x []complex128, invert bool) {
	n := len(x)
	if n <= 1 {
		return
	}
	// Bit-reversal permutation.
	j := 0
	for i := 1; i < n; i++ {
		bit := n >> 1
		for ; j&bit != 0; bit >>= 1 {
			j ^= bit
		}
		j ^= bit
		if i < j {
			x[i], x[j] = x[j], x[i]
		}
	}
	// Butterfly passes.
	for length := 2; length <= n; length <<= 1 {
		ang := 2.0 * math.Pi / float64(length)
		if invert {
			ang = -ang
		}
		wlen := complex(math.Cos(ang), math.Sin(ang))
		half := length / 2
		for i := 0; i < n; i += length {
			w := complex(1.0, 0.0)
			for k := 0; k < half; k++ {
				u := x[i+k]
				v := x[i+k+half] * w
				x[i+k] = u + v
				x[i+k+half] = u - v
				w *= wlen
			}
		}
	}
	if invert {
		invN := 1.0 / float64(n)
		for i := range x {
			x[i] = complex(real(x[i])*invN, imag(x[i])*invN)
		}
	}
}

// ---- 2-D FFT via parallel row/column passes ----------------

// fft2d performs a 2-D FFT (or IFFT) in-place on the flat
// row-major slice data of size rows×cols.  rows and cols must
// both be powers of 2.  workers controls how many goroutines
// are used for the row pass and again for the column pass.
func fft2d(data []complex128, rows, cols int, invert bool, workers int) {
	// Row passes: each row is independent.
	pFor(rows, workers, func(s, e int) {
		for i := s; i < e; i++ {
			fft1d(data[i*cols:(i+1)*cols], invert)
		}
	})
	// Column passes: each column is independent.
	// Each goroutine owns its own scratch buffer to avoid contention.
	pFor(cols, workers, func(s, e int) {
		col := make([]complex128, rows)
		for j := s; j < e; j++ {
			for i := 0; i < rows; i++ {
				col[i] = data[i*cols+j]
			}
			fft1d(col, invert)
			for i := 0; i < rows; i++ {
				data[i*cols+j] = col[i]
			}
		}
	})
}

// ---- FFT-shift helpers -------------------------------------

// fftShift2d rearranges a rows×cols flat array (row-major) so
// that the DC component moves from corner (0,0) to the centre.
// For even-sized arrays (always true here since we pad to
// powers of 2) this operation is self-inverse.
func fftShift2d(data []complex128, rows, cols int) {
	hr, hc := rows/2, cols/2
	for i := 0; i < hr; i++ {
		for j := 0; j < hc; j++ {
			// Top-left ↔ Bottom-right
			a := i*cols + j
			b := (i+hr)*cols + (j + hc)
			data[a], data[b] = data[b], data[a]
			// Top-right ↔ Bottom-left
			c := i*cols + (j + hc)
			d := (i+hr)*cols + j
			data[c], data[d] = data[d], data[c]
		}
	}
}

// ---- Geometric helpers -------------------------------------

// nextPow2FFT returns the smallest power of 2 that is >= n.
func nextPow2FFT(n int) int {
	p := 1
	for p < n {
		p <<= 1
	}
	return p
}

// ---- Morphological dilation --------------------------------

// dilate2dFFT applies morphological dilation to the float32 array
// src (rows×cols, row-major) using an elliptical structuring element
// with half-radii (kw, kh).  Boundary pixels are clamped.
// workers controls how many goroutines process row chunks concurrently.
func dilate2dFFT(src []float32, rows, cols, kw, kh, workers int) []float32 {
	dst := make([]float32, rows*cols)

	// Precompute which (dy,dx) offsets are inside the ellipse.
	// This is done once outside the parallel region.
	type pt struct{ dy, dx int }
	var se []pt
	var offset float64
	if kw > 0 && kh > 0 {
		offset = float64(kw+kh) / 2.0 / float64(kw*kh)
	}
	for dy := -kh; dy <= kh; dy++ {
		for dx := -kw; dx <= kw; dx++ {
			var ex, ey float64
			if kw > 0 {
				ex = float64(dx) / float64(kw)
			}
			if kh > 0 {
				ey = float64(dy) / float64(kh)
			}
			if ex*ex+ey*ey-offset <= 1.0 {
				se = append(se, pt{dy, dx})
			}
		}
	}
	if len(se) == 0 {
		copy(dst, src)
		return dst
	}

	// Each row of the output depends only on src (read-only), so rows
	// can be processed independently across worker goroutines.
	pFor(rows, workers, func(sy, ey int) {
		for y := sy; y < ey; y++ {
			for x := 0; x < cols; x++ {
				maxVal := float32(0)
				for _, p := range se {
					ny := y + p.dy
					nx := x + p.dx
					if ny < 0 {
						ny = 0
					} else if ny >= rows {
						ny = rows - 1
					}
					if nx < 0 {
						nx = 0
					} else if nx >= cols {
						nx = cols - 1
					}
					if v := src[ny*cols+nx]; v > maxVal {
						maxVal = v
					}
				}
				dst[y*cols+x] = maxVal
			}
		}
	})
	return dst
}

// ---- Gaussian blur -----------------------------------------

// gaussianBlur2dFFT applies a separable Gaussian blur (BORDER_REPLICATE)
// to the float32 array src (rows×cols, row-major).
// sigma is the standard deviation; kernel half-radius is ceil(3·sigma).
// workers controls concurrency for each of the two passes.
func gaussianBlur2dFFT(src []float32, rows, cols int, sigma float64, workers int) []float32 {
	if sigma <= 0 {
		return src
	}
	radius := int(math.Ceil(3 * sigma))
	ks := 2*radius + 1
	kernel := make([]float64, ks)
	sum := 0.0
	for i := 0; i < ks; i++ {
		d := float64(i - radius)
		kernel[i] = math.Exp(-d * d / (2 * sigma * sigma))
		sum += kernel[i]
	}
	for i := range kernel {
		kernel[i] /= sum
	}

	// Horizontal pass (src → tmp): rows are independent.
	tmp := make([]float32, rows*cols)
	pFor(rows, workers, func(sy, ey int) {
		for y := sy; y < ey; y++ {
			for x := 0; x < cols; x++ {
				acc := 0.0
				for k := 0; k < ks; k++ {
					nx := x + k - radius
					if nx < 0 {
						nx = 0
					} else if nx >= cols {
						nx = cols - 1
					}
					acc += float64(src[y*cols+nx]) * kernel[k]
				}
				tmp[y*cols+x] = float32(acc)
			}
		}
	})

	// Vertical pass (tmp → dst): rows of dst are independent
	// (each reads a vertical strip of tmp, no overlap in writes).
	dst := make([]float32, rows*cols)
	pFor(rows, workers, func(sy, ey int) {
		for y := sy; y < ey; y++ {
			for x := 0; x < cols; x++ {
				acc := 0.0
				for k := 0; k < ks; k++ {
					ny := y + k - radius
					if ny < 0 {
						ny = 0
					} else if ny >= rows {
						ny = rows - 1
					}
					acc += float64(tmp[ny*cols+x]) * kernel[k]
				}
				dst[y*cols+x] = float32(acc)
			}
		}
	})
	return dst
}

// ---- Main descreen function ---------------------------------

// applyDescreen applies the FFT-based halftone descreen filter to src
// and returns the result as a new *image.NRGBA.
//
// Parameters:
//   thresh — threshold for the distance-weighted log-magnitude spectrum
//            (0–200; higher = less aggressive filtering; default 92)
//   radius — dilation/blur radius for the peak mask (1–20; default 6)
//   middle — DC neighbourhood preservation ratio (1–10; default 4)
//            larger = larger protected region around DC
func applyDescreen(src *image.NRGBA, thresh, radius, middle int) *image.NRGBA {
	b := src.Bounds()
	origRows := b.Dy()
	origCols := b.Dx()

	// Pad to powers of 2 for the radix-2 FFT.
	paddedRows := nextPow2FFT(origRows)
	paddedCols := nextPow2FFT(origCols)
	N := paddedRows * paddedCols

	nCPU := runtime.NumCPU()
	// Each of the 3 channel goroutines gets nCPU workers for its inner loops.
	// Go's scheduler keeps actual CPU usage at ~nCPU total.
	innerW := nCPU

	// --- Normalization coefficients (computed once, shared read-only) ---
	// coef[y][x] = max( (√|x−cx| + √|y−cy|)², 0.01 )
	// Mirrors the Python normalize(h, w) helper.
	coefs := make([]float64, N)
	cy0 := paddedRows / 2
	cx0 := paddedCols / 2
	pFor(paddedRows, nCPU, func(sy, ey int) {
		for y := sy; y < ey; y++ {
			cy := math.Sqrt(math.Abs(float64(y - cy0)))
			for x := 0; x < paddedCols; x++ {
				cx := math.Sqrt(math.Abs(float64(x - cx0)))
				e := cx + cy
				v := e * e
				if v < 0.01 {
					v = 0.01
				}
				coefs[y*paddedCols+x] = v
			}
		}
	})

	// --- Middle-preservation mask (computed once, shared read-only) ---
	mid := middle * 2
	ew := paddedCols / mid
	eh := paddedRows / mid
	if ew < 1 {
		ew = 1
	}
	if eh < 1 {
		eh = 1
	}
	var middleOffset float64
	if ew > 0 && eh > 0 {
		middleOffset = float64(ew+eh) / 2.0 / float64(ew*eh)
	}
	middleMask := make([]float32, N)
	pFor(paddedRows, nCPU, func(sy, ey int) {
		for y := sy; y < ey; y++ {
			dy := float64(y-cy0) / float64(eh)
			for x := 0; x < paddedCols; x++ {
				dx := float64(x-cx0) / float64(ew)
				if dx*dx+dy*dy-middleOffset <= 1.0 {
					middleMask[y*paddedCols+x] = 1.0
				}
			}
		}
	})

	// --- Extract R, G, B channels as float32 (parallel by row) ---
	channels := [3][]float32{}
	for ch := 0; ch < 3; ch++ {
		channels[ch] = make([]float32, origRows*origCols)
	}
	pFor(origRows, nCPU, func(sy, ey int) {
		for y := sy; y < ey; y++ {
			for x := 0; x < origCols; x++ {
				off := (b.Min.Y+y)*src.Stride + (b.Min.X+x)*4
				channels[0][y*origCols+x] = float32(src.Pix[off])
				channels[1][y*origCols+x] = float32(src.Pix[off+1])
				channels[2][y*origCols+x] = float32(src.Pix[off+2])
			}
		}
	})

	threshF := float32(thresh)

	// --- Process the three colour channels concurrently ---
	results := [3][]float32{}
	var chanWG sync.WaitGroup
	for ch := 0; ch < 3; ch++ {
		chanWG.Add(1)
		go func(ch int) {
			defer chanWG.Done()

			// Each channel goroutine owns its own FFT scratch buffer.
			fftData := make([]complex128, N)

			// Fill padded complex array (zero-pad right/bottom).
			pFor(origRows, innerW, func(sy, ey int) {
				for y := sy; y < ey; y++ {
					// Zero the whole padded row first.
					row := fftData[y*paddedCols : (y+1)*paddedCols]
					for i := origCols; i < paddedCols; i++ {
						row[i] = 0
					}
					// Copy image data into the left portion.
					for x := 0; x < origCols; x++ {
						row[x] = complex(float64(channels[ch][y*origCols+x]), 0)
					}
				}
			})
			// Zero the padding rows (below origRows).
			pFor(paddedRows-origRows, innerW, func(s, e int) {
				for r := origRows + s; r < origRows+e; r++ {
					row := fftData[r*paddedCols : (r+1)*paddedCols]
					for i := range row {
						row[i] = 0
					}
				}
			})

			// Forward 2-D FFT.
			fft2d(fftData, paddedRows, paddedCols, false, innerW)

			// Shift DC to centre.
			fftShift2d(fftData, paddedRows, paddedCols)

			// Compute distance-weighted log-magnitude spectrum and threshold.
			threshMask := make([]float32, N)
			pFor(N, innerW, func(s, e int) {
				for i := s; i < e; i++ {
					re := real(fftData[i])
					im := imag(fftData[i])
					mag := math.Sqrt(re*re + im*im)
					spec := float32(20.0 * math.Log(math.Max(mag*coefs[i], 1e-10)))
					if spec < 0 {
						spec = 0
					}
					if spec > threshF {
						threshMask[i] = 255.0
					}
				}
			})

			// Zero out the DC neighbourhood (middle preservation).
			pFor(N, innerW, func(s, e int) {
				for i := s; i < e; i++ {
					threshMask[i] *= 1.0 - middleMask[i]
				}
			})

			// Dilate and Gaussian-blur the peak mask.
			if radius > 0 {
				threshMask = dilate2dFFT(threshMask, paddedRows, paddedCols, radius, radius, innerW)
				sigma := float64(radius) / 3.0
				threshMask = gaussianBlur2dFFT(threshMask, paddedRows, paddedCols, sigma, innerW)
			}

			// Build suppression filter and apply to the complex FFT plane.
			pFor(N, innerW, func(s, e int) {
				for i := s; i < e; i++ {
					filter := 1.0 - float64(threshMask[i])/255.0
					fftData[i] = complex(real(fftData[i])*filter, imag(fftData[i])*filter)
				}
			})

			// Inverse shift and inverse FFT.
			fftShift2d(fftData, paddedRows, paddedCols) // self-inverse for even sizes
			fft2d(fftData, paddedRows, paddedCols, true, innerW)

			// Extract magnitudes for the original (unpadded) region.
			out := make([]float32, origRows*origCols)
			pFor(origRows, innerW, func(sy, ey int) {
				for y := sy; y < ey; y++ {
					for x := 0; x < origCols; x++ {
						c := fftData[y*paddedCols+x]
						re := real(c)
						im := imag(c)
						out[y*origCols+x] = float32(math.Sqrt(re*re + im*im))
					}
				}
			})
			results[ch] = out
		}(ch)
	}
	chanWG.Wait()

	// --- Write results to a new NRGBA image (parallel by row) ---
	dst := image.NewNRGBA(b)
	pFor(origRows, nCPU, func(sy, ey int) {
		for y := sy; y < ey; y++ {
			for x := 0; x < origCols; x++ {
				srcOff := (b.Min.Y+y)*src.Stride + (b.Min.X+x)*4
				dstOff := (b.Min.Y+y)*dst.Stride + (b.Min.X+x)*4
				dst.Pix[dstOff] = clampByte(int(results[0][y*origCols+x] + 0.5))
				dst.Pix[dstOff+1] = clampByte(int(results[1][y*origCols+x] + 0.5))
				dst.Pix[dstOff+2] = clampByte(int(results[2][y*origCols+x] + 0.5))
				dst.Pix[dstOff+3] = src.Pix[srcOff+3] // preserve alpha
			}
		}
	})
	return dst
}
