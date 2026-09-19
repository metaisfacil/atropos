//go:build amd64

package patchmatch

import (
	"image"
	"testing"
	"unsafe"
)

var pmKernelBenchmarkSink uint32

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
		limit:        ^uint32(0),
	}
}

func pmPatchSSD7OpaqueScalar(args *pmOpaqueKernelArgs) uint32 {
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
	return sum
}

func TestPMOpaqueKernelLayout(t *testing.T) {
	var args pmOpaqueKernelArgs
	offsets := []uintptr{
		unsafe.Offsetof(args.target), unsafe.Offsetof(args.source),
		unsafe.Offsetof(args.targetStride), unsafe.Offsetof(args.sourceStride),
		unsafe.Offsetof(args.limit),
	}
	want := []uintptr{0, 8, 16, 24, 32}
	for i, offset := range offsets {
		if offset != want[i] {
			t.Fatalf("field %d offset %d, expected %d", i, offset, want[i])
		}
	}
	if unsafe.Sizeof(args) != 40 {
		t.Fatalf("opaque args size=%d", unsafe.Sizeof(args))
	}
}

func TestPMOpaque7KernelsMatchScalar(t *testing.T) {
	if !pmUseAVX2 {
		t.Skip("AVX2 unavailable")
	}
	args := pmOpaque7TestArgs()
	want := pmPatchSSD7OpaqueScalar(&args)
	if got := pmPatchSSD7OpaqueFullAVX2(&args); got != want {
		t.Fatalf("full 7x7 opaque=%d scalar=%d", got, want)
	}
	if got := pmRunSynthesisOpaqueKernel(&args); got != want {
		t.Fatalf("full dispatch=%d scalar=%d", got, want)
	}
	for _, limit := range []uint32{0, want / 10, want / 2, want - 1, want, want + 1, ^uint32(0) - 1} {
		args.limit = limit
		got := pmPatchSSD7OpaqueBoundedAVX2(&args)
		wantBounded := want
		if want >= limit {
			wantBounded = limit
		}
		if got != wantBounded {
			t.Errorf("limit=%d bounded=%d want=%d (full=%d)", limit, got, wantBounded, want)
		}
		if dispatched := pmRunSynthesisOpaqueKernel(&args); dispatched != wantBounded {
			t.Errorf("limit=%d dispatch=%d want=%d", limit, dispatched, wantBounded)
		}
	}
}

func BenchmarkPMOpaqueKernel7(b *testing.B) {
	if !pmUseAVX2 {
		b.Skip("AVX2 unavailable")
	}
	args := pmOpaque7TestArgs()
	want := pmPatchSSD7OpaqueScalar(&args)
	b.Run("full", func(b *testing.B) {
		args.limit = ^uint32(0)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			pmKernelBenchmarkSink = pmPatchSSD7OpaqueFullAVX2(&args)
		}
	})
	b.Run("bounded-half", func(b *testing.B) {
		args.limit = want / 2
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			pmKernelBenchmarkSink = pmPatchSSD7OpaqueBoundedAVX2(&args)
		}
	})
}
