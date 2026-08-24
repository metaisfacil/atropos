//go:build amd64

package patchmatch

import (
	"context"
	"math"
	"testing"
)

func TestPMPatchSSDAVX2MatchesScalar(t *testing.T) {
	if !pmUseAVX2 {
		t.Skip("AVX2/FMA is unavailable on this CPU")
	}
	for _, patchSize := range []int{1, 3, 5, 7, 9, 15} {
		args := makePMKernelTestArgs(patchSize)
		want := pmPatchSSDScalar(&args)
		got := pmPatchSSDAVX2(&args)
		tolerance := float32(math.Max(0.01, float64(want)*2e-6))
		if delta := absFloat32(got - want); delta > tolerance {
			t.Errorf("patch %d: AVX2=%g scalar=%g delta=%g tolerance=%g", patchSize, got, want, delta, tolerance)
		}
	}
}

func TestPMPatchSSDAVX2EarlyExit(t *testing.T) {
	if !pmUseAVX2 {
		t.Skip("AVX2/FMA is unavailable on this CPU")
	}
	args := makePMKernelTestArgs(15)
	full := pmPatchSSDScalar(&args)
	args.limit = full * 0.1
	partial := pmPatchSSDAVX2(&args)
	if partial <= args.limit {
		t.Fatalf("early-exit result %g did not exceed threshold %g", partial, args.limit)
	}
	if partial >= full {
		t.Fatalf("kernel evaluated the full patch despite threshold: partial=%g full=%g", partial, full)
	}
}

func TestPMPatchKernelDispatchAMD64(t *testing.T) {
	got := pmActivePatchKernel()
	if pmUseAVX2 && got != "avx2-fma" {
		t.Fatalf("AVX2/FMA CPU selected %q", got)
	}
	if !pmUseAVX2 && got != "scalar" {
		t.Fatalf("non-AVX2 CPU selected %q", got)
	}
}

func BenchmarkPatchMatchFillAMD64(b *testing.B) {
	src, mask := makeFilledSrc(512, 384)
	original := pmUseAVX2
	defer func() { pmUseAVX2 = original }()

	for _, benchmark := range []struct {
		name string
		simd bool
	}{
		{name: "scalar", simd: false},
		{name: "avx2-fma", simd: original},
	} {
		if benchmark.name == "avx2-fma" && !original {
			continue
		}
		b.Run(benchmark.name, func(b *testing.B) {
			pmUseAVX2 = benchmark.simd
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := Fill(context.Background(), src, mask, 7, 4); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkPMPatchSSDAMD64(b *testing.B) {
	args := makePMKernelTestArgs(7)
	b.Run("scalar", func(b *testing.B) {
		var result float32
		for i := 0; i < b.N; i++ {
			result = pmPatchSSDScalar(&args)
		}
		pmKernelBenchmarkSink = result
	})
	if pmUseAVX2 {
		b.Run("avx2-fma", func(b *testing.B) {
			var result float32
			for i := 0; i < b.N; i++ {
				result = pmPatchSSDAVX2(&args)
			}
			pmKernelBenchmarkSink = result
		})
	}
}

var pmKernelBenchmarkSink float32
