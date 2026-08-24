package descreen

import (
	"fmt"
	"math"
)

// fftPlan32 is a compact mixed-radix FFT plan for the only transform sizes
// descreening needs: lengths whose prime factors are 2, 3, and 5. The plan is
// immutable after construction and can therefore be shared by all workers.
//
// Input is digit-reversed into scratch once, after which each radix stage runs
// in place. Inverse transforms include the conventional 1/n normalization.
type fftPlan32 struct {
	n           int
	radices     []int
	permutation []int
	stages      []fftStage32
}

type fftStage32 struct {
	radix    int
	span     int
	previous int
	twiddles []complex64 // (radix-1) rows of previous values
}

func newFFTPlan32(n int) (*fftPlan32, error) {
	if n < 1 {
		return nil, fmt.Errorf("invalid FFT length %d", n)
	}
	radices, ok := smoothRadicesFFT(n)
	if !ok {
		return nil, fmt.Errorf("FFT length %d has a prime factor other than 2, 3, or 5", n)
	}

	p := &fftPlan32{n: n, radices: radices}
	p.permutation = make([]int, n)
	digits := make([]int, len(radices))
	for dst := 0; dst < n; dst++ {
		v := dst
		for i, radix := range radices {
			digits[i] = v % radix
			v /= radix
		}
		src, mul := 0, 1
		for i := len(radices) - 1; i >= 0; i-- {
			src += digits[i] * mul
			mul *= radices[i]
		}
		p.permutation[dst] = src
	}

	span := 1
	for _, radix := range radices {
		previous := span
		span *= radix
		stage := fftStage32{
			radix:    radix,
			span:     span,
			previous: previous,
			twiddles: make([]complex64, (radix-1)*previous),
		}
		for q := 1; q < radix; q++ {
			for k := 0; k < previous; k++ {
				angle := -2 * math.Pi * float64(q*k) / float64(span)
				stage.twiddles[(q-1)*previous+k] = complex64(complex(math.Cos(angle), math.Sin(angle)))
			}
		}
		p.stages = append(p.stages, stage)
	}
	return p, nil
}

// smoothRadicesFFT returns a low-stage-count radix schedule. Radix-4 stages
// substantially reduce memory traffic compared with using two radix-2 stages.
func smoothRadicesFFT(n int) ([]int, bool) {
	if n == 1 {
		return nil, true
	}
	remaining := n
	radices := make([]int, 0, 16)
	for remaining%4 == 0 {
		radices = append(radices, 4)
		remaining /= 4
	}
	if remaining%2 == 0 {
		radices = append(radices, 2)
		remaining /= 2
	}
	for remaining%3 == 0 {
		radices = append(radices, 3)
		remaining /= 3
	}
	for remaining%5 == 0 {
		radices = append(radices, 5)
		remaining /= 5
	}
	return radices, remaining == 1
}

func nextSmoothFFT(n int) int {
	if n <= 1 {
		return 1
	}
	for candidate := n; ; candidate++ {
		v := candidate
		for _, factor := range [...]int{2, 3, 5} {
			for v%factor == 0 {
				v /= factor
			}
		}
		if v == 1 {
			return candidate
		}
	}
}

func (p *fftPlan32) transform(data, scratch []complex64, inverse bool) {
	if len(data) != p.n || len(scratch) < p.n {
		panic("fftPlan32: invalid buffer length")
	}
	if p.n == 1 {
		return
	}
	for dst, src := range p.permutation {
		scratch[dst] = data[src]
	}

	for _, stage := range p.stages {
		switch stage.radix {
		case 2:
			fftStageRadix2(scratch[:p.n], stage, inverse)
		case 3:
			fftStageRadix3(scratch[:p.n], stage, inverse)
		case 4:
			fftStageRadix4(scratch[:p.n], stage, inverse)
		case 5:
			fftStageRadix5(scratch[:p.n], stage, inverse)
		default:
			panic("fftPlan32: unsupported radix")
		}
	}

	if inverse {
		scale := float32(1) / float32(p.n)
		for i, v := range scratch[:p.n] {
			data[i] = complex(real(v)*scale, imag(v)*scale)
		}
		return
	}
	copy(data, scratch[:p.n])
}

func fftTwiddle32(v complex64, stage fftStage32, q, k int, inverse bool) complex64 {
	w := stage.twiddles[(q-1)*stage.previous+k]
	if inverse {
		w = complex(real(w), -imag(w))
	}
	return v * w
}

func fftStageRadix2(data []complex64, stage fftStage32, inverse bool) {
	p := stage.previous
	for base := 0; base < len(data); base += stage.span {
		for k := 0; k < p; k++ {
			a := data[base+k]
			b := fftTwiddle32(data[base+p+k], stage, 1, k, inverse)
			data[base+k] = a + b
			data[base+p+k] = a - b
		}
	}
}

func fftStageRadix3(data []complex64, stage fftStage32, inverse bool) {
	const sin120 = float32(0.86602540378443864676)
	p := stage.previous
	for base := 0; base < len(data); base += stage.span {
		for k := 0; k < p; k++ {
			a := data[base+k]
			b := fftTwiddle32(data[base+p+k], stage, 1, k, inverse)
			c := fftTwiddle32(data[base+2*p+k], stage, 2, k, inverse)
			t1 := b + c
			t2 := b - c
			center := a - complex(0.5*real(t1), 0.5*imag(t1))
			rot := complex(sin120*imag(t2), -sin120*real(t2)) // -i*sin(2pi/3)*t2
			if inverse {
				rot = -rot
			}
			data[base+k] = a + t1
			data[base+p+k] = center + rot
			data[base+2*p+k] = center - rot
		}
	}
}

func fftStageRadix4(data []complex64, stage fftStage32, inverse bool) {
	p := stage.previous
	for base := 0; base < len(data); base += stage.span {
		for k := 0; k < p; k++ {
			a := data[base+k]
			b := fftTwiddle32(data[base+p+k], stage, 1, k, inverse)
			c := fftTwiddle32(data[base+2*p+k], stage, 2, k, inverse)
			d := fftTwiddle32(data[base+3*p+k], stage, 3, k, inverse)
			t0 := a + c
			t1 := a - c
			t2 := b + d
			diff := d - b
			rot := complex(-imag(diff), real(diff)) // i*(d-b)
			if inverse {
				rot = -rot
			}
			data[base+k] = t0 + t2
			data[base+p+k] = t1 + rot
			data[base+2*p+k] = t0 - t2
			data[base+3*p+k] = t1 - rot
		}
	}
}

func fftStageRadix5(data []complex64, stage fftStage32, inverse bool) {
	const (
		cos72  = float32(0.30901699437494742410)
		cos144 = float32(-0.80901699437494742410)
		sin72  = float32(0.95105651629515357212)
		sin144 = float32(0.58778525229247312917)
	)
	p := stage.previous
	for base := 0; base < len(data); base += stage.span {
		for k := 0; k < p; k++ {
			a0 := data[base+k]
			a1 := fftTwiddle32(data[base+p+k], stage, 1, k, inverse)
			a2 := fftTwiddle32(data[base+2*p+k], stage, 2, k, inverse)
			a3 := fftTwiddle32(data[base+3*p+k], stage, 3, k, inverse)
			a4 := fftTwiddle32(data[base+4*p+k], stage, 4, k, inverse)
			t1 := a1 + a4
			t2 := a2 + a3
			t3 := a1 - a4
			t4 := a2 - a3
			base14 := a0 + complex(cos72*real(t1)+cos144*real(t2), cos72*imag(t1)+cos144*imag(t2))
			base23 := a0 + complex(cos144*real(t1)+cos72*real(t2), cos144*imag(t1)+cos72*imag(t2))
			v14 := complex(sin72*real(t3)+sin144*real(t4), sin72*imag(t3)+sin144*imag(t4))
			v23 := complex(sin144*real(t3)-sin72*real(t4), sin144*imag(t3)-sin72*imag(t4))
			rot14 := complex(imag(v14), -real(v14))
			rot23 := complex(imag(v23), -real(v23))
			if inverse {
				rot14 = -rot14
				rot23 = -rot23
			}
			data[base+k] = a0 + t1 + t2
			data[base+p+k] = base14 + rot14
			data[base+2*p+k] = base23 + rot23
			data[base+3*p+k] = base23 - rot23
			data[base+4*p+k] = base14 - rot14
		}
	}
}

type realFFTPlan32 struct {
	n        int
	half     int
	complex  *fftPlan32
	twiddles []complex64
}

func newRealFFTPlan32(n int) (*realFFTPlan32, error) {
	if n < 2 || n%2 != 0 {
		return nil, fmt.Errorf("real FFT length must be positive and even, got %d", n)
	}
	cp, err := newFFTPlan32(n / 2)
	if err != nil {
		return nil, err
	}
	p := &realFFTPlan32{n: n, half: n / 2, complex: cp, twiddles: make([]complex64, n/2+1)}
	for k := range p.twiddles {
		angle := -2 * math.Pi * float64(k) / float64(n)
		p.twiddles[k] = complex64(complex(math.Cos(angle), math.Sin(angle)))
	}
	return p, nil
}

// forward converts n real samples into n/2+1 canonical complex bins.
func (p *realFFTPlan32) forward(input []float32, output, packed, scratch []complex64) {
	if len(input) != p.n || len(output) != p.half+1 || len(packed) < p.half || len(scratch) < p.half {
		panic("realFFTPlan32.forward: invalid buffer length")
	}
	for i := 0; i < p.half; i++ {
		packed[i] = complex(input[2*i], input[2*i+1])
	}
	p.complex.transform(packed[:p.half], scratch[:p.half], false)
	for k := 0; k <= p.half; k++ {
		a := packed[k%p.half]
		b := packed[(p.half-k)%p.half]
		b = complex(real(b), -imag(b))
		even := complex(0.5*(real(a)+real(b)), 0.5*(imag(a)+imag(b)))
		diff := a - b
		odd := complex(0.5*imag(diff), -0.5*real(diff)) // (a-b)/(2i)
		output[k] = even + p.twiddles[k]*odd
	}
}

// inverse converts canonical bins back into n real samples. The complex plan
// performs its own normalization, so no additional scale is needed here.
func (p *realFFTPlan32) inverse(input []complex64, output []float32, packed, scratch []complex64) {
	if len(input) != p.half+1 || len(output) != p.n || len(packed) < p.half || len(scratch) < p.half {
		panic("realFFTPlan32.inverse: invalid buffer length")
	}
	for k := 0; k < p.half; k++ {
		x1 := input[k]
		x2 := input[p.half-k]
		x2 = complex(real(x2), -imag(x2))
		even := complex(0.5*(real(x1)+real(x2)), 0.5*(imag(x1)+imag(x2)))
		diff := x1 - x2
		w := p.twiddles[k]
		// odd = (x1-x2)/(2*w); |w|=1, so division is multiplication
		// by conjugate(w).
		odd := complex(0.5*real(diff), 0.5*imag(diff)) * complex(real(w), -imag(w))
		packed[k] = even + complex(-imag(odd), real(odd)) // even + i*odd
	}
	p.complex.transform(packed[:p.half], scratch[:p.half], true)
	for i, v := range packed[:p.half] {
		output[2*i] = real(v)
		output[2*i+1] = imag(v)
	}
}
