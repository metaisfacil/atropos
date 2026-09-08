//go:build amd64

package patchmatch

import (
	"image"
	"math"
	"testing"
	"unsafe"
)

func pmOpaqueTestArgs(patchSize int) (pmKernelArgs, pmOpaqueKernelArgs) {
	const w, h = 39, 33
	target := image.NewNRGBA(image.Rect(0, 0, w, h))
	source := image.NewNRGBA(image.Rect(0, 0, w+1, h)) // a different byte stride
	mask := image.NewAlpha(target.Bounds())
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			for c := 0; c < 3; c++ {
				target.Pix[y*target.Stride+x*4+c] = byte(pmHash(uint32(x), uint32(y), uint32(c+1)))
				source.Pix[y*source.Stride+x*4+c] = byte(pmHash(uint32(x), uint32(y), uint32(c+7)))
			}
			target.Pix[y*target.Stride+x*4+3] = 255
			source.Pix[y*source.Stride+x*4+3] = 255
			mask.Pix[y*mask.Stride+x] = byte((x*19 + y*31) % 256)
		}
	}
	// End at the last pixel of the target allocation. The raw-byte kernel must
	// mask its final vector load rather than rely on packed-plane padding.
	x0, y0 := w-patchSize, h-patchSize
	tp, sp := packPMPixels(target), packPMPixels(source)
	// The float kernel requires equal plane strides; these widths both pad to 48.
	confidence, stride, _ := packPMConfidence(mask)
	ti, si := y0*tp.stride+x0, y0*sp.stride+x0
	full := pmKernelArgs{
		targetR: &tp.channel[0][ti], targetG: &tp.channel[1][ti], targetB: &tp.channel[2][ti], targetA: &tp.channel[3][ti],
		sourceR: &sp.channel[0][si], sourceG: &sp.channel[1][si], sourceB: &sp.channel[2][si], sourceA: &sp.channel[3][si],
		confidence: &confidence[y0*stride+x0], stride: stride, patchSize: patchSize, limit: float32(math.Inf(1)),
	}
	raw := pmOpaqueKernelArgs{
		target: &target.Pix[y0*target.Stride+x0*4], source: &source.Pix[y0*source.Stride+x0*4],
		confidence: full.confidence, targetStride: target.Stride, sourceStride: source.Stride,
		confidenceStride: stride, patchSize: patchSize, limit: full.limit,
	}
	return full, raw
}

func TestPMOpaqueKernelLayout(t *testing.T) {
	var a pmOpaqueKernelArgs
	offsets := []uintptr{unsafe.Offsetof(a.target), unsafe.Offsetof(a.source), unsafe.Offsetof(a.confidence), unsafe.Offsetof(a.targetStride), unsafe.Offsetof(a.sourceStride), unsafe.Offsetof(a.confidenceStride), unsafe.Offsetof(a.patchSize), unsafe.Offsetof(a.limit)}
	for i, offset := range offsets {
		if offset != uintptr(i*8) {
			t.Fatalf("field %d offset %d, expected %d", i, offset, i*8)
		}
	}
	if unsafe.Sizeof(a) != 64 {
		t.Fatalf("opaque args size=%d", unsafe.Sizeof(a))
	}
}

func TestPMOpaqueKernelMatchesScalar(t *testing.T) {
	if !pmUseAVX2 {
		t.Skip("AVX2/FMA unavailable")
	}
	for patch := 1; patch <= 15; patch += 2 {
		full, raw := pmOpaqueTestArgs(patch)
		want := pmPatchSSDScalar(&full)
		got := pmPatchSSDOpaqueAVX2(&raw)
		tolerance := float32(math.Max(0.01, float64(want)*2e-6))
		if absFloat32(got-want) > tolerance {
			t.Errorf("patch %d: opaque=%g scalar=%g", patch, got, want)
		}
		for _, fraction := range []float32{0.1, 0.5, 0.9} {
			raw.limit = want * fraction
			got = pmPatchSSDOpaqueAVX2(&raw)
			if got <= raw.limit || got > want+tolerance {
				t.Errorf("patch %d limit=%g invalid partial=%g full=%g", patch, raw.limit, got, want)
			}
		}
	}
}

func TestPMPackedOpacityTracksReusedImages(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 12, 9))
	for i := 3; i < len(src.Pix); i += 4 {
		src.Pix[i] = 255
	}
	planes := packPMPixels(src)
	if !planes.opaque {
		t.Fatal("opaque source did not select byte-compatible metadata")
	}
	// A single translucent pixel, even in the last row, must retain the
	// premultiplied float path. Reusing buffers must not retain stale opacity.
	src.Pix[len(src.Pix)-1] = 254
	planes = packPMPixelsInto(src, planes)
	if planes.opaque {
		t.Fatal("translucent source was classified as opaque")
	}
	src.Pix[len(src.Pix)-1] = 255
	planes = packPMPixelsInto(src, planes)
	if !planes.opaque {
		t.Fatal("opaque source retained stale translucent metadata")
	}
}

func BenchmarkPMOpaqueKernel15(b *testing.B) {
	if !pmUseAVX2 {
		b.Skip("AVX2/FMA unavailable")
	}
	full, raw := pmOpaqueTestArgs(15)
	b.Run("float_planes", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			pmKernelBenchmarkSink = pmPatchSSDAVX2(&full)
		}
	})
	b.Run("packed_bytes", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			pmKernelBenchmarkSink = pmPatchSSDOpaqueAVX2(&raw)
		}
	})
}
