#include "textflag.h"

// pmPatchSSD7OpaqueAVX2 is the fixed 7x7, unit-confidence comparator used by
// the reconstructed synthesis engine. Keeping the RGB sums as int32 removes
// the generic kernel's confidence loads, float conversions and FMAs. Both
// inputs are opaque, so the fourth byte of each pixel contributes zero.
TEXT ·pmPatchSSD7OpaqueAVX2(SB), NOSPLIT, $0-12
	MOVQ args+0(FP), BP
	MOVQ 0(BP), R8
	MOVQ 8(BP), R9
	MOVQ 24(BP), SI
	MOVQ 32(BP), R10
	VMOVSS 56(BP), X14
	LEAQ ·pmOddTailMasks+32(SB), AX
	VMOVDQU (AX), X13
	VPXOR X15, X15, X15
	XORQ DX, DX

ssd7_row:
	// Pixels 0..3.
	VMOVDQU (R8), X0
	VMOVDQU (R9), X1
	VPMOVZXBW X0, Y0
	VPMOVZXBW X1, Y1
	VPSUBW Y1, Y0, Y0
	VPMADDWD Y0, Y0, Y2
	VPHADDD Y2, Y2, Y2
	VPERMQ $0xd8, Y2, Y2
	VPADDD X2, X15, X15

	// Pixels 4..6. The masked fourth dword prevents the final row from reading
	// beyond the image allocation when the patch touches the right edge.
	VMASKMOVPS 16(R8), X13, X0
	VMASKMOVPS 16(R9), X13, X1
	VPMOVZXBW X0, Y0
	VPMOVZXBW X1, Y1
	VPSUBW Y1, Y0, Y0
	VPMADDWD Y0, Y0, Y2
	VPHADDD Y2, Y2, Y2
	VPERMQ $0xd8, Y2, Y2
	VPADDD X2, X15, X15

	VPHADDD X15, X15, X3
	VPHADDD X3, X3, X3
	VCVTDQ2PS X3, X3
	VUCOMISS X14, X3
	JA ssd7_done
	INCQ DX
	ADDQ SI, R8
	ADDQ R10, R9
	CMPQ DX, $7
	JL ssd7_row

ssd7_done:
	VMOVSS X3, ret+8(FP)
	VZEROUPPER
	RET
