//go:build arm64

package patchmatch

import (
	"math"
	"testing"
)

func TestPMPatchSSDNEONMatchesScalar(t *testing.T) {
	if !pmUseNEON {
		t.Skip("NEON/ASIMD is unavailable on this CPU")
	}
	for _, patchSize := range []int{1, 3, 5, 7, 9, 15} {
		args := makePMKernelTestArgs(patchSize)
		want := pmPatchSSDScalar(&args)
		got := pmPatchSSDNEON(&args)
		tolerance := float32(math.Max(0.01, float64(want)*2e-6))
		if delta := absFloat32(got - want); delta > tolerance {
			t.Errorf("patch %d: NEON=%g scalar=%g delta=%g tolerance=%g", patchSize, got, want, delta, tolerance)
		}
	}
}

func TestPMPatchSSDNEONEarlyExit(t *testing.T) {
	if !pmUseNEON {
		t.Skip("NEON/ASIMD is unavailable on this CPU")
	}
	args := makePMKernelTestArgs(15)
	full := pmPatchSSDScalar(&args)
	args.limit = full * 0.1
	partial := pmPatchSSDNEON(&args)
	if partial <= args.limit || partial >= full {
		t.Fatalf("NEON threshold exit failed: partial=%g threshold=%g full=%g", partial, args.limit, full)
	}
}

func TestPMPatchKernelDispatchARM64(t *testing.T) {
	got := pmActivePatchKernel()
	if pmUseNEON && got != "neon" {
		t.Fatalf("NEON CPU selected %q", got)
	}
	if !pmUseNEON && got != "scalar" {
		t.Fatalf("non-NEON CPU selected %q", got)
	}
}
