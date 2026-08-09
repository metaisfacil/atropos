//go:build amd64

package main

import "golang.org/x/sys/cpu"

var pmUseAVX2 = cpu.X86.HasAVX2 && cpu.X86.HasFMA

func pmRunPatchKernel(args *pmKernelArgs) float32 {
	if pmUseAVX2 {
		return pmPatchSSDAVX2(args)
	}
	return pmPatchSSDScalar(args)
}

func pmActivePatchKernel() string {
	if pmUseAVX2 {
		return "avx2-fma"
	}
	return "scalar"
}

//go:noescape
func pmPatchSSDAVX2(args *pmKernelArgs) float32
