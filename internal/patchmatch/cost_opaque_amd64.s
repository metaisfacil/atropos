#include "textflag.h"

// Accumulate one 7-pixel RGB row into X15. Both loads are wholly inside the
// patch: the second covers pixels 3..6, then shifts away the repeated pixel 3.
#define ACCUMULATE_SSD7_ROW \
	VPMOVZXBW (R8), Y0; \
	VPMOVZXBW (R9), Y1; \
	VPSUBW Y1, Y0, Y0; \
	VPMADDWD Y0, Y0, Y2; \
	VPHADDD Y2, Y2, Y2; \
	VPERMQ $0xd8, Y2, Y2; \
	VPADDD X2, X15, X15; \
	VPMOVZXBW 12(R8), Y0; \
	VPMOVZXBW 12(R9), Y1; \
	VPSUBW Y1, Y0, Y0; \
	VPMADDWD Y0, Y0, Y2; \
	VPHADDD Y2, Y2, Y2; \
	VPERMQ $0xd8, Y2, Y2; \
	VPSRLDQ $4, X2, X2; \
	VPADDD X2, X15, X15

// pmPatchSSD7OpaqueFullAVX2 computes all seven rows before reducing the four
// integer lanes. Full-cost callers use this path with no threshold checks.
TEXT ·pmPatchSSD7OpaqueFullAVX2(SB), NOSPLIT, $0-12
	MOVQ args+0(FP), BP
	MOVQ 0(BP), R8
	MOVQ 8(BP), R9
	MOVQ 16(BP), SI
	MOVQ 24(BP), R10
	VPXOR X15, X15, X15

	ACCUMULATE_SSD7_ROW
	ADDQ SI, R8
	ADDQ R10, R9
	ACCUMULATE_SSD7_ROW
	ADDQ SI, R8
	ADDQ R10, R9
	ACCUMULATE_SSD7_ROW
	ADDQ SI, R8
	ADDQ R10, R9
	ACCUMULATE_SSD7_ROW
	ADDQ SI, R8
	ADDQ R10, R9
	ACCUMULATE_SSD7_ROW
	ADDQ SI, R8
	ADDQ R10, R9
	ACCUMULATE_SSD7_ROW
	ADDQ SI, R8
	ADDQ R10, R9
	ACCUMULATE_SSD7_ROW

	VPHADDD X15, X15, X3
	VPHADDD X3, X3, X3
	VMOVD X3, AX
	MOVL AX, ret+8(FP)
	VZEROUPPER
	RET

// pmPatchSSD7OpaqueBoundedAVX2 rejects a candidate as soon as its row-wise
// partial sum reaches the incumbent. Returning the limit is sufficient because
// bounded results are used only in a strict less-than comparison.
TEXT ·pmPatchSSD7OpaqueBoundedAVX2(SB), NOSPLIT, $0-12
	MOVQ args+0(FP), BP
	MOVQ 0(BP), R8
	MOVQ 8(BP), R9
	MOVQ 16(BP), SI
	MOVQ 24(BP), R10
	MOVL 32(BP), R11
	VPXOR X15, X15, X15

	ACCUMULATE_SSD7_ROW
	VPHADDD X15, X15, X3
	VPHADDD X3, X3, X3
	VMOVD X3, AX
	CMPL AX, R11
	JAE ssd7_bounded_reject
	ADDQ SI, R8
	ADDQ R10, R9

	ACCUMULATE_SSD7_ROW
	VPHADDD X15, X15, X3
	VPHADDD X3, X3, X3
	VMOVD X3, AX
	CMPL AX, R11
	JAE ssd7_bounded_reject
	ADDQ SI, R8
	ADDQ R10, R9

	ACCUMULATE_SSD7_ROW
	VPHADDD X15, X15, X3
	VPHADDD X3, X3, X3
	VMOVD X3, AX
	CMPL AX, R11
	JAE ssd7_bounded_reject
	ADDQ SI, R8
	ADDQ R10, R9

	ACCUMULATE_SSD7_ROW
	VPHADDD X15, X15, X3
	VPHADDD X3, X3, X3
	VMOVD X3, AX
	CMPL AX, R11
	JAE ssd7_bounded_reject
	ADDQ SI, R8
	ADDQ R10, R9

	ACCUMULATE_SSD7_ROW
	VPHADDD X15, X15, X3
	VPHADDD X3, X3, X3
	VMOVD X3, AX
	CMPL AX, R11
	JAE ssd7_bounded_reject
	ADDQ SI, R8
	ADDQ R10, R9

	ACCUMULATE_SSD7_ROW
	VPHADDD X15, X15, X3
	VPHADDD X3, X3, X3
	VMOVD X3, AX
	CMPL AX, R11
	JAE ssd7_bounded_reject
	ADDQ SI, R8
	ADDQ R10, R9

	ACCUMULATE_SSD7_ROW
	VPHADDD X15, X15, X3
	VPHADDD X3, X3, X3
	VMOVD X3, AX
	CMPL AX, R11
	JAE ssd7_bounded_reject

	MOVL AX, ret+8(FP)
	VZEROUPPER
	RET

ssd7_bounded_reject:
	MOVL R11, ret+8(FP)
	VZEROUPPER
	RET
