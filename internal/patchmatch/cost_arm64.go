//go:build arm64

package patchmatch

import "golang.org/x/sys/cpu"

var pmUseNEON = cpu.ARM64.HasASIMD

func pmRunPatchKernel(args *pmKernelArgs) float32 {
	if pmUseNEON {
		return pmPatchSSDNEON(args)
	}
	return pmPatchSSDScalar(args)
}

func pmActivePatchKernel() string {
	if pmUseNEON {
		return "neon"
	}
	return "scalar"
}

//go:noescape
func pmPatchSSDNEON(args *pmKernelArgs) float32
