//go:build amd64

package patchmatch

import "golang.org/x/sys/cpu"

var pmUseAVX2 = cpu.X86.HasAVX2
var synthesisUseAESHardware = cpu.X86.HasAES

func pmOpaqueKernelAvailable() bool { return pmUseAVX2 }

func pmRunSynthesisOpaqueKernel(args *pmOpaqueKernelArgs) uint32 {
	if args.limit == ^uint32(0) {
		return pmPatchSSD7OpaqueFullAVX2(args)
	}
	return pmPatchSSD7OpaqueBoundedAVX2(args)
}

//go:noescape
func pmPatchSSD7OpaqueFullAVX2(args *pmOpaqueKernelArgs) uint32

//go:noescape
func pmPatchSSD7OpaqueBoundedAVX2(args *pmOpaqueKernelArgs) uint32

//go:noescape
func synthesisEncryptCounterHardware(block *[16]byte, keys *[11][16]byte)

func pmActivePatchKernel() string {
	if pmUseAVX2 {
		return "avx2"
	}
	return "scalar"
}
