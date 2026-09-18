//go:build amd64

package patchmatch

import (
	"image"
	"testing"
	"unsafe"
)

var pmKernelBenchmarkSink float32

func pmOpaque7TestArgs() pmOpaqueKernelArgs {
	const w, h = 39, 33
	target := image.NewNRGBA(image.Rect(0, 0, w, h))
	source := image.NewNRGBA(image.Rect(0, 0, w+1, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			for c := 0; c < 3; c++ {
				target.Pix[y*target.Stride+x*4+c] = byte(pmTestHash(uint32(x), uint32(y), uint32(c+1)))
				source.Pix[y*source.Stride+x*4+c] = byte(pmTestHash(uint32(x), uint32(y), uint32(c+7)))
			}
			target.Pix[y*target.Stride+x*4+3] = 255
			source.Pix[y*source.Stride+x*4+3] = 255
		}
	}
	x0, y0 := w-synthesisPatchSize, h-synthesisPatchSize
	return pmOpaqueKernelArgs{
		target:       &target.Pix[y0*target.Stride+x0*4],
		source:       &source.Pix[y0*source.Stride+x0*4],
		targetStride: target.Stride,
		sourceStride: source.Stride,
		patchSize:    synthesisPatchSize,
		limit:        1 << 30,
	}
}

func pmPatchSSD7OpaqueScalar(args *pmOpaqueKernelArgs) float32 {
	target := unsafe.Slice(args.target, (synthesisPatchSize-1)*args.targetStride+synthesisPatchSize*4)
	source := unsafe.Slice(args.source, (synthesisPatchSize-1)*args.sourceStride+synthesisPatchSize*4)
	var sum uint32
	for y := 0; y < synthesisPatchSize; y++ {
		for x := 0; x < synthesisPatchSize; x++ {
			ti := y*args.targetStride + x*4
			si := y*args.sourceStride + x*4
			for c := 0; c < 3; c++ {
				d := int32(target[ti+c]) - int32(source[si+c])
				sum += uint32(d * d)
			}
		}
	}
	return float32(sum)
}

func TestPMOpaqueKernelLayout(t *testing.T) {
	var args pmOpaqueKernelArgs
	offsets := []uintptr{
		unsafe.Offsetof(args.target), unsafe.Offsetof(args.source), unsafe.Offsetof(args.confidence),
		unsafe.Offsetof(args.targetStride), unsafe.Offsetof(args.sourceStride), unsafe.Offsetof(args.confidenceStride),
		unsafe.Offsetof(args.patchSize), unsafe.Offsetof(args.limit),
	}
	for i, offset := range offsets {
		if offset != uintptr(i*8) {
			t.Fatalf("field %d offset %d, expected %d", i, offset, i*8)
		}
	}
	if unsafe.Sizeof(args) != 64 {
		t.Fatalf("opaque args size=%d", unsafe.Sizeof(args))
	}
}

func TestPMOpaque7KernelMatchesScalar(t *testing.T) {
	if !pmUseAVX2 {
		t.Skip("AVX2/FMA unavailable")
	}
	args := pmOpaque7TestArgs()
	want := pmPatchSSD7OpaqueScalar(&args)
	if got := pmPatchSSD7OpaqueAVX2(&args); got != want {
		t.Fatalf("fixed 7x7 opaque=%g scalar=%g", got, want)
	}
	for _, fraction := range []float32{0.1, 0.5, 0.9} {
		args.limit = want * fraction
		got := pmPatchSSD7OpaqueAVX2(&args)
		if got <= args.limit || got > want {
			t.Errorf("limit=%g invalid partial=%g full=%g", args.limit, got, want)
		}
	}
}

func BenchmarkPMOpaqueKernel7(b *testing.B) {
	if !pmUseAVX2 {
		b.Skip("AVX2/FMA unavailable")
	}
	args := pmOpaque7TestArgs()
	for i := 0; i < b.N; i++ {
		pmKernelBenchmarkSink = pmPatchSSD7OpaqueAVX2(&args)
	}
}
