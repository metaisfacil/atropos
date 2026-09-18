package patchmatch

// pmOpaqueKernelArgs is the ABI shared with the fixed-size amd64 comparator.
type pmOpaqueKernelArgs struct {
	target           *byte
	source           *byte
	confidence       *float32
	targetStride     int
	sourceStride     int
	confidenceStride int
	patchSize        int
	limit            float32
}

// ActiveKernel reports the patch-cost implementation selected for this CPU.
// It is intended for diagnostics and performance logging.
func ActiveKernel() string {
	return pmActivePatchKernel()
}
