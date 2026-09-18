//go:build !amd64

package patchmatch

const synthesisUseAESHardware = false

func synthesisEncryptCounterHardware(_ *[16]byte, _ *[11][16]byte) {}

func pmOpaqueKernelAvailable() bool { return false }

func pmRunSynthesisOpaqueKernel(args *pmOpaqueKernelArgs) float32 {
	panic("opaque PatchMatch kernel is unavailable on this architecture")
}
