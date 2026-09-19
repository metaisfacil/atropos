package patchmatch

// pmOpaqueKernelArgs is the ABI shared with the fixed-size amd64 comparator.
type pmOpaqueKernelArgs struct {
	target       *byte
	source       *byte
	targetStride int
	sourceStride int
	limit        uint32
}

// ActiveKernel reports the patch-cost implementation selected for this CPU.
// It is intended for diagnostics and performance logging.
func ActiveKernel() string {
	return pmActivePatchKernel()
}
