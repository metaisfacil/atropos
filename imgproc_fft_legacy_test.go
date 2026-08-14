package main

import (
	"image"
	"math"
	"runtime"
	"sync"
	"time"
)

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

// ---- Main descreen function ---------------------------------

// applyDescreen applies the FFT-based halftone descreen filter to src
// and returns the result as a new *image.NRGBA.
//
// Parameters:
//
//	thresh           — threshold for the distance-weighted log-magnitude spectrum
//	                   (0–200; higher = less aggressive filtering; default 92)
//	radius           — dilation/blur radius for the peak mask (1–20; default 6)
//	middle           — DC neighbourhood preservation ratio (1–10; default 4)
//	                   larger = larger protected region around DC
//	highlightRestore — highlight restoration (0–100; 0 = pure descreen output;
//	                   higher values blend original highlights back over the
//	                   descreened result to hide screen-pattern artifacts in
//	                   near-white areas; default 0)
//	logf             — optional logger (may be nil); receives per-phase timing lines
func applyDescreenLegacy(src *image.NRGBA, thresh, radius, middle, highlightRestore int, logf func(string, ...interface{})) *image.NRGBA {
	totalStart := time.Now()

	b := src.Bounds()
	origRows := b.Dy()
	origCols := b.Dx()

	// Pad to powers of 2 for the radix-2 FFT.
	paddedRows := nextPow2FFT(origRows)
	paddedCols := nextPow2FFT(origCols)
	N := paddedRows * paddedCols

	nCPU := runtime.NumCPU()
	// Divide workers evenly across the 3 concurrent channel goroutines so
	// that total goroutines ≈ nCPU rather than 3×nCPU competing for the
	// same CPUs (especially important for memory-bound steps like dilation).
	innerW := nCPU / 3
	if innerW < 1 {
		innerW = 1
	}

	if logf != nil {
		logf("Descreen: start %dx%d → padded %dx%d, nCPU=%d",
			origCols, origRows, paddedCols, paddedRows, nCPU)
	}

	// --- Normalization coefficients (computed once, shared read-only) ---
	// coef[y][x] = max( (√|x−cx| + √|y−cy|)², 0.01 )
	// Mirrors the Python normalize(h, w) helper.
	t := time.Now()
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
	if logf != nil {
		logf("Descreen: setup (coefs+middleMask) %s", time.Since(t).Round(time.Millisecond))
	}

	// --- Extract R, G, B channels as float32 (parallel by row) ---
	t = time.Now()
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
	if logf != nil {
		logf("Descreen: channel extract %s", time.Since(t).Round(time.Millisecond))
	}

	threshF := float32(thresh)

	// --- Process the three colour channels concurrently ---
	chNames := [3]string{"R", "G", "B"}
	results := [3][]float32{}
	var chanWG sync.WaitGroup
	channelsStart := time.Now()
	for ch := 0; ch < 3; ch++ {
		chanWG.Add(1)
		go func(ch int) {
			defer chanWG.Done()
			chStart := time.Now()

			// Each channel goroutine owns its own FFT scratch buffer.
			fftData := make([]complex128, N)

			// Fill padded complex array (zero-pad right/bottom).
			t := time.Now()
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
			if logf != nil {
				logf("Descreen: [%s] pad fill %s", chNames[ch], time.Since(t).Round(time.Millisecond))
			}

			// Forward 2-D FFT.
			t = time.Now()
			fft2d(fftData, paddedRows, paddedCols, false, innerW)
			if logf != nil {
				logf("Descreen: [%s] fwd FFT %s", chNames[ch], time.Since(t).Round(time.Millisecond))
			}

			// Shift DC to centre.
			fftShift2d(fftData, paddedRows, paddedCols)

			// Compute distance-weighted log-magnitude spectrum and threshold.
			// Dividing by N normalises for image size: bins with below-average
			// energy yield spec ≤ 0 (not detected) regardless of resolution,
			// so the threshold slider range 50–150 is meaningful for any image.
			t = time.Now()
			invN := 1.0 / float64(N)
			threshMask := make([]float32, N)
			pFor(N, innerW, func(s, e int) {
				for i := s; i < e; i++ {
					re := real(fftData[i])
					im := imag(fftData[i])
					mag := math.Sqrt(re*re + im*im)
					spec := float32(20.0 * math.Log(math.Max(mag*coefs[i]*invN, 1e-10)))
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
			if logf != nil {
				// Count peaks remaining after DC exclusion — if zero, dilation and
				// blur have nothing to work with and threshold/radius have no effect.
				peakCount := 0
				for _, v := range threshMask {
					if v > 0 {
						peakCount++
					}
				}
				logf("Descreen: [%s] threshold+mask %s — %d peaks (%.2f%% of spectrum)",
					chNames[ch], time.Since(t).Round(time.Millisecond),
					peakCount, 100*float64(peakCount)/float64(N))
			}

			// Dilate and Gaussian-blur the peak mask.
			if radius > 0 {
				t = time.Now()
				threshMask = dilate2dFFT(threshMask, paddedRows, paddedCols, radius, radius, innerW)
				if logf != nil {
					logf("Descreen: [%s] dilate %s", chNames[ch], time.Since(t).Round(time.Millisecond))
				}
				t = time.Now()
				sigma := float64(radius) / 3.0
				threshMask = gaussianBlur2dFFT(threshMask, paddedRows, paddedCols, sigma, innerW)
				if logf != nil {
					logf("Descreen: [%s] blur %s", chNames[ch], time.Since(t).Round(time.Millisecond))
				}
			}

			// Build suppression filter and apply to the complex FFT plane.
			t = time.Now()
			pFor(N, innerW, func(s, e int) {
				for i := s; i < e; i++ {
					filter := 1.0 - float64(threshMask[i])/255.0
					fftData[i] = complex(real(fftData[i])*filter, imag(fftData[i])*filter)
				}
			})

			// Inverse shift and inverse FFT.
			fftShift2d(fftData, paddedRows, paddedCols) // self-inverse for even sizes
			fft2d(fftData, paddedRows, paddedCols, true, innerW)
			if logf != nil {
				logf("Descreen: [%s] filter+inv FFT %s", chNames[ch], time.Since(t).Round(time.Millisecond))
			}

			// Extract magnitudes for the original (unpadded) region.
			t = time.Now()
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
			if logf != nil {
				logf("Descreen: [%s] magnitude extract %s", chNames[ch], time.Since(t).Round(time.Millisecond))
				logf("Descreen: [%s] total %s", chNames[ch], time.Since(chStart).Round(time.Millisecond))
			}
			results[ch] = out
		}(ch)
	}
	chanWG.Wait()
	if logf != nil {
		logf("Descreen: all channels wall time %s", time.Since(channelsStart).Round(time.Millisecond))
	}

	// --- Write results to a new NRGBA image (parallel by row) ---
	// When highlightRestore < 100, bright pixels are blended back toward the
	// original so that near-white areas do not show the inverse screen artifact.
	// blendStrength ranges from 0 (no effect at highlightRestore=0) to 1
	// (full highlight restoration at highlightRestore=100).  The blend ramps
	// linearly from 0 at luma=128 to blendStrength at luma=255, covering
	// the top 50% of the tonal range.
	t = time.Now()
	blendStrength := float64(highlightRestore) / 100.0
	dst := image.NewNRGBA(b)
	pFor(origRows, nCPU, func(sy, ey int) {
		for y := sy; y < ey; y++ {
			for x := 0; x < origCols; x++ {
				srcOff := (b.Min.Y+y)*src.Stride + (b.Min.X+x)*4
				dstOff := (b.Min.Y+y)*dst.Stride + (b.Min.X+x)*4
				dr := clampByte(int(results[0][y*origCols+x] + 0.5))
				dg := clampByte(int(results[1][y*origCols+x] + 0.5))
				db := clampByte(int(results[2][y*origCols+x] + 0.5))
				if blendStrength > 0 {
					origR := float64(src.Pix[srcOff])
					origG := float64(src.Pix[srcOff+1])
					origB := float64(src.Pix[srcOff+2])
					luma := 0.299*origR + 0.587*origG + 0.114*origB
					// Ramp: 0 at luma≤128, blendStrength at luma=255.
					hf := (luma - 128.0) / 127.0
					if hf > 0 {
						if hf > 1 {
							hf = 1
						}
						blend := blendStrength * hf
						dr = clampByte(int(float64(dr)*(1-blend) + origR*blend + 0.5))
						dg = clampByte(int(float64(dg)*(1-blend) + origG*blend + 0.5))
						db = clampByte(int(float64(db)*(1-blend) + origB*blend + 0.5))
					}
				}
				dst.Pix[dstOff] = dr
				dst.Pix[dstOff+1] = dg
				dst.Pix[dstOff+2] = db
				dst.Pix[dstOff+3] = src.Pix[srcOff+3] // preserve alpha
			}
		}
	})
	if logf != nil {
		logf("Descreen: write output %s", time.Since(t).Round(time.Millisecond))
		logf("Descreen: total %s", time.Since(totalStart).Round(time.Millisecond))
	}
	return dst
}
