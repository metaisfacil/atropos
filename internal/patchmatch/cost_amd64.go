//go:build amd64

package patchmatch

import "golang.org/x/sys/cpu"

var pmUseAVX2 = cpu.X86.HasAVX2 && cpu.X86.HasFMA
var synthesisUseAESHardware = cpu.X86.HasAES

func pmOpaqueKernelAvailable() bool { return pmUseAVX2 }

func pmRunSynthesisOpaqueKernel(args *pmOpaqueKernelArgs) float32 {
	return pmPatchSSD7OpaqueAVX2(args)
}

//go:noescape
func pmPatchSSD7OpaqueAVX2(args *pmOpaqueKernelArgs) float32

//go:noescape
func synthesisEncryptCounterHardware(block *[16]byte, keys *[11][16]byte)

func pmActivePatchKernel() string {
	if pmUseAVX2 {
		return "avx2-fma"
	}
	return "scalar"
}
