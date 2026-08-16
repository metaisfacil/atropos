package main

import (
	"image"
	"math"
	"runtime"
	"sync"
	"time"
)

type descreenFFTPlan32 struct {
	rows     int
	cols     int
	halfCols int
	row      *realFFTPlan32
	column   *fftPlan32
}

func newDescreenFFTPlan32(rows, cols int) (*descreenFFTPlan32, error) {
	row, err := newRealFFTPlan32(cols)
	if err != nil {
		return nil, err
	}
	column, err := newFFTPlan32(rows)
	if err != nil {
		return nil, err
	}
	return &descreenFFTPlan32{
		rows:     rows,
		cols:     cols,
		halfCols: cols/2 + 1,
		row:      row,
		column:   column,
	}, nil
}

// forwardChannel transforms one NRGBA colour channel. Only the unique half of
// the real spectrum is retained, cutting both FFT work and resident memory
// nearly in half.
func (p *descreenFFTPlan32) forwardChannel(src *image.NRGBA, channel, workers int) []complex64 {
	b := src.Bounds()
	origRows, origCols := b.Dy(), b.Dx()
	spectrum := make([]complex64, p.rows*p.halfCols)

	pFor(origRows, workers, func(sy, ey int) {
		rowInput := make([]float32, p.cols)
		packed := make([]complex64, p.cols/2)
		scratch := make([]complex64, p.cols/2)
		for y := sy; y < ey; y++ {
			for x := 0; x < origCols; x++ {
				off := (b.Min.Y+y)*src.Stride + (b.Min.X+x)*4 + channel
				rowInput[x] = float32(src.Pix[off])
			}
			clear(rowInput[origCols:])
			rowOut := spectrum[y*p.halfCols : (y+1)*p.halfCols]
			p.row.forward(rowInput, rowOut, packed, scratch)
		}
	})

	// Column transforms. A contiguous per-worker buffer avoids running the FFT
	// itself over cache-hostile strided memory.
	pFor(p.halfCols, workers, func(sx, ex int) {
		column := make([]complex64, p.rows)
		scratch := make([]complex64, p.rows)
		for x := sx; x < ex; x++ {
			for y := 0; y < p.rows; y++ {
				column[y] = spectrum[y*p.halfCols+x]
			}
			p.column.transform(column, scratch, false)
			for y := 0; y < p.rows; y++ {
				spectrum[y*p.halfCols+x] = column[y]
			}
		}
	})
	return spectrum
}

// forwardLuminance transforms a single Rec. 601 luminance plane. The fast
// descreen path filters this plane once instead of transforming all three RGB
// channels independently.
func (p *descreenFFTPlan32) forwardLuminance(src *image.NRGBA, workers int) []complex64 {
	b := src.Bounds()
	origRows, origCols := b.Dy(), b.Dx()
	spectrum := make([]complex64, p.rows*p.halfCols)

	pFor(origRows, workers, func(sy, ey int) {
		rowInput := make([]float32, p.cols)
		packed := make([]complex64, p.cols/2)
		scratch := make([]complex64, p.cols/2)
		for y := sy; y < ey; y++ {
			for x := 0; x < origCols; x++ {
				off := y*src.Stride + x*4
				rowInput[x] = 0.299*float32(src.Pix[off]) +
					0.587*float32(src.Pix[off+1]) +
					0.114*float32(src.Pix[off+2])
			}
			clear(rowInput[origCols:])
			rowOut := spectrum[y*p.halfCols : (y+1)*p.halfCols]
			p.row.forward(rowInput, rowOut, packed, scratch)
		}
	})

	pFor(p.halfCols, workers, func(sx, ex int) {
		column := make([]complex64, p.rows)
		scratch := make([]complex64, p.rows)
		for x := sx; x < ex; x++ {
			for y := 0; y < p.rows; y++ {
				column[y] = spectrum[y*p.halfCols+x]
			}
			p.column.transform(column, scratch, false)
			for y := 0; y < p.rows; y++ {
				spectrum[y*p.halfCols+x] = column[y]
			}
		}
	})
	return spectrum
}

// inverseChannel writes one filtered channel directly into dst. Avoiding a
// full-size float result for every colour channel keeps the working set bounded.
func (p *descreenFFTPlan32) inverseChannel(spectrum []complex64, dst *image.NRGBA, channel, workers int) {
	if len(spectrum) != p.rows*p.halfCols {
		panic("descreenFFTPlan32.inverseChannel: invalid spectrum length")
	}

	pFor(p.halfCols, workers, func(sx, ex int) {
		column := make([]complex64, p.rows)
		scratch := make([]complex64, p.rows)
		for x := sx; x < ex; x++ {
			for y := 0; y < p.rows; y++ {
				column[y] = spectrum[y*p.halfCols+x]
			}
			p.column.transform(column, scratch, true)
			for y := 0; y < p.rows; y++ {
				spectrum[y*p.halfCols+x] = column[y]
			}
		}
	})

	b := dst.Bounds()
	origRows, origCols := b.Dy(), b.Dx()
	pFor(origRows, workers, func(sy, ey int) {
		rowOutput := make([]float32, p.cols)
		packed := make([]complex64, p.cols/2)
		scratch := make([]complex64, p.cols/2)
		for y := sy; y < ey; y++ {
			rowIn := spectrum[y*p.halfCols : (y+1)*p.halfCols]
			p.row.inverse(rowIn, rowOutput, packed, scratch)
			for x := 0; x < origCols; x++ {
				// The legacy complex inverse extracts magnitude. A real inverse has
				// only a signed real component, so abs is the equivalent operation.
				value := float64(rowOutput[x])
				if value < 0 {
					value = -value
				}
				off := (b.Min.Y+y)*dst.Stride + (b.Min.X+x)*4 + channel
				dst.Pix[off] = clampByte(int(value + 0.5))
			}
		}
	})
}

// inverseLuminance reconstructs the filtered luminance plane and combines it
// with the source RGB channels by adding the luminance delta. This preserves
// the source chroma while removing screen frequencies from luminance. Highlight
// restoration operates on the filtered luminance before it is recombined.
func (p *descreenFFTPlan32) inverseLuminance(spectrum []complex64, src, dst *image.NRGBA, highlightRestore, workers int) {
	if len(spectrum) != p.rows*p.halfCols {
		panic("descreenFFTPlan32.inverseLuminance: invalid spectrum length")
	}

	pFor(p.halfCols, workers, func(sx, ex int) {
		column := make([]complex64, p.rows)
		scratch := make([]complex64, p.rows)
		for x := sx; x < ex; x++ {
			for y := 0; y < p.rows; y++ {
				column[y] = spectrum[y*p.halfCols+x]
			}
			p.column.transform(column, scratch, true)
			for y := 0; y < p.rows; y++ {
				spectrum[y*p.halfCols+x] = column[y]
			}
		}
	})

	b := src.Bounds()
	origRows, origCols := b.Dy(), b.Dx()
	blendStrength := float64(highlightRestore) / 100
	pFor(origRows, workers, func(sy, ey int) {
		rowOutput := make([]float32, p.cols)
		packed := make([]complex64, p.cols/2)
		scratch := make([]complex64, p.cols/2)
		for y := sy; y < ey; y++ {
			rowIn := spectrum[y*p.halfCols : (y+1)*p.halfCols]
			p.row.inverse(rowIn, rowOutput, packed, scratch)
			for x := 0; x < origCols; x++ {
				srcOff := y*src.Stride + x*4
				dstOff := y*dst.Stride + x*4
				origR := float64(src.Pix[srcOff])
				origG := float64(src.Pix[srcOff+1])
				origB := float64(src.Pix[srcOff+2])
				origY := 0.299*origR + 0.587*origG + 0.114*origB
				filteredY := math.Abs(float64(rowOutput[x]))

				if blendStrength > 0 {
					hf := (origY - 128) / 127
					if hf > 0 {
						if hf > 1 {
							hf = 1
						}
						blend := blendStrength * hf
						filteredY = filteredY*(1-blend) + origY*blend
					}
				}

				delta := filteredY - origY
				dst.Pix[dstOff] = clampByte(int(math.Round(origR + delta)))
				dst.Pix[dstOff+1] = clampByte(int(math.Round(origG + delta)))
				dst.Pix[dstOff+2] = clampByte(int(math.Round(origB + delta)))
				dst.Pix[dstOff+3] = src.Pix[srcOff+3]
			}
		}
	})
}

// buildDescreenThresholdMask constructs the same centered, full-plane mask as
// the legacy fftShift-based implementation while leaving the half-spectrum in
// its natural order. Squaring the threshold comparison removes a sqrt and log
// from every frequency bin.
func (p *descreenFFTPlan32) buildDescreenThresholdMask(spectrum []complex64, thresh, middle, workers int) ([]float32, int) {
	mask := make([]float32, p.rows*p.cols)
	cy, cx := p.rows/2, p.cols/2
	mid := middle * 2
	ew, eh := p.cols/mid, p.rows/mid
	if ew < 1 {
		ew = 1
	}
	if eh < 1 {
		eh = 1
	}
	middleOffset := float64(ew+eh) / 2 / float64(ew*eh)
	thresholdScale := math.Exp(float64(thresh)/20) * float64(p.rows*p.cols)

	counts := make([]int, workers)
	if len(counts) == 0 {
		counts = make([]int, 1)
	}
	var workerMu syncCounter
	pFor(p.rows, workers, func(sy, ey int) {
		id := workerMu.next()
		local := 0
		for shiftedY := sy; shiftedY < ey; shiftedY++ {
			dyInt := shiftedY - cy
			dyEllipse := float64(dyInt) / float64(eh)
			ky := shiftedY + cy
			if ky >= p.rows {
				ky -= p.rows
			}
			for shiftedX := 0; shiftedX < p.cols; shiftedX++ {
				dxInt := shiftedX - cx
				dxEllipse := float64(dxInt) / float64(ew)
				if dxEllipse*dxEllipse+dyEllipse*dyEllipse-middleOffset <= 1 {
					continue
				}
				kx := shiftedX + cx
				if kx >= p.cols {
					kx -= p.cols
				}
				readY, readX := ky, kx
				if readX > p.cols/2 {
					readX = p.cols - readX
					if readY != 0 {
						readY = p.rows - readY
					}
				}
				v := spectrum[readY*p.halfCols+readX]
				coefRoot := math.Sqrt(math.Abs(float64(dxInt))) + math.Sqrt(math.Abs(float64(dyInt)))
				coef := coefRoot * coefRoot
				if coef < 0.01 {
					coef = 0.01
				}
				limit := thresholdScale / coef
				re, im := float64(real(v)), float64(imag(v))
				if re*re+im*im > limit*limit {
					mask[shiftedY*p.cols+shiftedX] = 255
					local++
				}
			}
		}
		counts[id] = local
	})
	count := 0
	for _, n := range counts {
		count += n
	}
	return mask, count
}

// syncCounter only assigns a small stable slot to each pFor callback. It is
// deliberately simpler than atomically incrementing the peak count per bin.
type syncCounter struct {
	mu sync.Mutex
	n  int
}

func (c *syncCounter) next() int {
	c.mu.Lock()
	id := c.n
	c.n++
	c.mu.Unlock()
	return id
}

func (p *descreenFFTPlan32) applyMask(spectrum []complex64, mask []float32, workers int) {
	pFor(p.rows, workers, func(sy, ey int) {
		for ky := sy; ky < ey; ky++ {
			shiftedY := ky + p.rows/2
			if shiftedY >= p.rows {
				shiftedY -= p.rows
			}
			for kx := 0; kx < p.halfCols; kx++ {
				shiftedX := kx + p.cols/2
				if shiftedX >= p.cols {
					shiftedX -= p.cols
				}
				filter := 1 - mask[shiftedY*p.cols+shiftedX]/255
				spectrum[ky*p.halfCols+kx] *= complex(filter, 0)
			}
		}
	})
}

func nextSmoothEvenFFT(n int) int {
	for candidate := n; ; candidate++ {
		if candidate%2 == 0 && nextSmoothFFT(candidate) == candidate {
			return candidate
		}
	}
}

// applyDescreen is the production pure-Go descreen path. It uses planned
// single-precision real transforms and 2/3/5-smooth padding instead of three
// concurrent, double-precision, power-of-two complex transforms.
func applyDescreen(src *image.NRGBA, thresh, radius, middle, highlightRestore int, logf func(string, ...interface{})) *image.NRGBA {
	totalStart := time.Now()
	b := src.Bounds()
	origRows, origCols := b.Dy(), b.Dx()
	paddedRows := nextSmoothFFT(origRows)
	paddedCols := nextSmoothEvenFFT(origCols)
	n := paddedRows * paddedCols
	workers := runtime.GOMAXPROCS(0)
	if workers < 1 {
		workers = 1
	}

	if logf != nil {
		logf("Descreen: start %dx%d → smooth %dx%d, nCPU=%d",
			origCols, origRows, paddedCols, paddedRows, workers)
	}

	t := time.Now()
	plan, err := newDescreenFFTPlan32(paddedRows, paddedCols)
	if err != nil {
		// nextSmoothFFT guarantees supported dimensions, so reaching this path
		// indicates an internal programming error rather than bad user input.
		panic(err)
	}
	if logf != nil {
		logf("Descreen: setup (FFT plans) %s", time.Since(t).Round(time.Millisecond))
	}

	dst := image.NewNRGBA(b)
	channelNames := [...]string{"R", "G", "B"}
	channelsStart := time.Now()
	for channel := 0; channel < 3; channel++ {
		channelStart := time.Now()

		t = time.Now()
		spectrum := plan.forwardChannel(src, channel, workers)
		if logf != nil {
			logf("Descreen: [%s] real fwd FFT %s", channelNames[channel], time.Since(t).Round(time.Millisecond))
		}

		t = time.Now()
		mask, peakCount := plan.buildDescreenThresholdMask(spectrum, thresh, middle, workers)
		if logf != nil {
			logf("Descreen: [%s] threshold+mask %s — %d peaks (%.2f%% of spectrum)",
				channelNames[channel], time.Since(t).Round(time.Millisecond),
				peakCount, 100*float64(peakCount)/float64(n))
		}

		if radius > 0 {
			scratchMask := make([]float32, len(mask))
			t = time.Now()
			dilateBinaryMaskFFTInPlace(mask, scratchMask, paddedRows, paddedCols, radius, radius, workers)
			if logf != nil {
				logf("Descreen: [%s] dilate %s", channelNames[channel], time.Since(t).Round(time.Millisecond))
			}
			t = time.Now()
			gaussianBlur2dFFTInPlace(mask, scratchMask, paddedRows, paddedCols, float64(radius)/3, workers)
			if logf != nil {
				logf("Descreen: [%s] blur %s", channelNames[channel], time.Since(t).Round(time.Millisecond))
			}
		}

		t = time.Now()
		plan.applyMask(spectrum, mask, workers)
		plan.inverseChannel(spectrum, dst, channel, workers)
		if logf != nil {
			logf("Descreen: [%s] filter+real inv FFT %s", channelNames[channel], time.Since(t).Round(time.Millisecond))
			logf("Descreen: [%s] total %s", channelNames[channel], time.Since(channelStart).Round(time.Millisecond))
		}
	}
	if logf != nil {
		logf("Descreen: all channels wall time %s", time.Since(channelsStart).Round(time.Millisecond))
	}

	// Restore highlights and alpha after all three filtered channels have been
	// written. Keeping channel output in dst avoids three large float buffers.
	t = time.Now()
	blendStrength := float64(highlightRestore) / 100
	pFor(origRows, workers, func(sy, ey int) {
		for y := sy; y < ey; y++ {
			for x := 0; x < origCols; x++ {
				srcOff := (b.Min.Y+y)*src.Stride + (b.Min.X+x)*4
				dstOff := (b.Min.Y+y)*dst.Stride + (b.Min.X+x)*4
				if blendStrength > 0 {
					origR := float64(src.Pix[srcOff])
					origG := float64(src.Pix[srcOff+1])
					origB := float64(src.Pix[srcOff+2])
					luma := 0.299*origR + 0.587*origG + 0.114*origB
					hf := (luma - 128) / 127
					if hf > 0 {
						if hf > 1 {
							hf = 1
						}
						blend := blendStrength * hf
						for channel := 0; channel < 3; channel++ {
							filtered := float64(dst.Pix[dstOff+channel])
							original := float64(src.Pix[srcOff+channel])
							dst.Pix[dstOff+channel] = clampByte(int(filtered*(1-blend) + original*blend + 0.5))
						}
					}
				}
				dst.Pix[dstOff+3] = src.Pix[srcOff+3]
			}
		}
	})
	if logf != nil {
		logf("Descreen: write output %s", time.Since(t).Round(time.Millisecond))
		logf("Descreen: total %s", time.Since(totalStart).Round(time.Millisecond))
	}
	return dst
}

// applyDescreenLuminance is the fast descreen path. It detects and suppresses
// screen frequencies in one luminance plane, then adds the filtered luminance
// delta back to RGB so the source chroma and alpha are retained.
func applyDescreenLuminance(src *image.NRGBA, thresh, radius, middle, highlightRestore int, logf func(string, ...interface{})) *image.NRGBA {
	totalStart := time.Now()
	b := src.Bounds()
	origRows, origCols := b.Dy(), b.Dx()
	paddedRows := nextSmoothFFT(origRows)
	paddedCols := nextSmoothEvenFFT(origCols)
	n := paddedRows * paddedCols
	workers := runtime.GOMAXPROCS(0)
	if workers < 1 {
		workers = 1
	}

	if logf != nil {
		logf("Descreen fast: start %dx%d -> smooth %dx%d, nCPU=%d",
			origCols, origRows, paddedCols, paddedRows, workers)
	}

	t := time.Now()
	plan, err := newDescreenFFTPlan32(paddedRows, paddedCols)
	if err != nil {
		panic(err)
	}
	if logf != nil {
		logf("Descreen fast: setup (FFT plans) %s", time.Since(t).Round(time.Millisecond))
	}

	t = time.Now()
	spectrum := plan.forwardLuminance(src, workers)
	if logf != nil {
		logf("Descreen fast: [Y] real fwd FFT %s", time.Since(t).Round(time.Millisecond))
	}

	t = time.Now()
	mask, peakCount := plan.buildDescreenThresholdMask(spectrum, thresh, middle, workers)
	if logf != nil {
		logf("Descreen fast: [Y] threshold+mask %s - %d peaks (%.2f%% of spectrum)",
			time.Since(t).Round(time.Millisecond), peakCount, 100*float64(peakCount)/float64(n))
	}

	if radius > 0 {
		scratchMask := make([]float32, len(mask))
		t = time.Now()
		dilateBinaryMaskFFTInPlace(mask, scratchMask, paddedRows, paddedCols, radius, radius, workers)
		if logf != nil {
			logf("Descreen fast: [Y] dilate %s", time.Since(t).Round(time.Millisecond))
		}
		t = time.Now()
		gaussianBlur2dFFTInPlace(mask, scratchMask, paddedRows, paddedCols, float64(radius)/3, workers)
		if logf != nil {
			logf("Descreen fast: [Y] blur %s", time.Since(t).Round(time.Millisecond))
		}
	}

	t = time.Now()
	plan.applyMask(spectrum, mask, workers)
	dst := image.NewNRGBA(b)
	plan.inverseLuminance(spectrum, src, dst, highlightRestore, workers)
	if logf != nil {
		logf("Descreen fast: [Y] filter+real inv FFT+recombine %s", time.Since(t).Round(time.Millisecond))
		logf("Descreen fast: total %s", time.Since(totalStart).Round(time.Millisecond))
	}
	return dst
}
