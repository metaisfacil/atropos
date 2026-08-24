//go:build !amd64 && !arm64

package patchmatch

func pmRunPatchKernel(args *pmKernelArgs) float32 {
	return pmPatchSSDScalar(args)
}

func pmActivePatchKernel() string {
	return "scalar"
}
