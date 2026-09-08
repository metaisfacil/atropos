//go:build !amd64

package patchmatch

func pmOpaqueKernelAvailable() bool { return false }

func pmRunOpaqueKernel(args *pmOpaqueKernelArgs) float32 {
	panic("opaque PatchMatch kernel is unavailable on this architecture")
}
