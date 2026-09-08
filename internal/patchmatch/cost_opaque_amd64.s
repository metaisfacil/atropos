#include "textflag.h"

// Four RGBA pixels per vector. Expand bytes, square pairwise channel
// differences, then gather each pixel's RGB sum before applying confidence.
// Tail loads are masked because raw image rows have no SIMD padding.
TEXT ·pmPatchSSDOpaqueAVX2(SB), NOSPLIT, $0-12
	MOVQ args+0(FP), BP
	MOVQ 0(BP), R8
	MOVQ 8(BP), R9
	MOVQ 16(BP), BX
	MOVQ 24(BP), SI
	MOVQ 32(BP), R10
	MOVQ 40(BP), R11
	SHLQ $2, R11
	MOVQ 48(BP), CX
	VMOVSS 56(BP), X14
	VXORPS X15, X15, X15
	MOVQ CX, R13
	ANDQ $3, R13
	SHRQ $1, R13
	SHLQ $5, R13
	LEAQ ·pmOddTailMasks(SB), AX
	VMOVDQU (AX)(R13*1), X13
	XORQ DX, DX

opaque_row:
	XORQ DI, DI
opaque_column:
	MOVQ CX, AX
	SUBQ DI, AX
	CMPQ AX, $4
	JL opaque_tail
	VMOVDQU (R8)(DI*4), X0
	VMOVDQU (R9)(DI*4), X1
	JMP opaque_channels
opaque_tail:
	VMASKMOVPS (R8)(DI*4), X13, X0
	VMASKMOVPS (R9)(DI*4), X13, X1
opaque_channels:
	VPMOVZXBW X0, Y0
	VPMOVZXBW X1, Y1
	VPSUBW Y1, Y0, Y0
	VPMADDWD Y0, Y0, Y2
	VPHADDD Y2, Y2, Y2
	VPERMQ $0xd8, Y2, Y2
	VCVTDQ2PS X2, X2
	VMOVUPS (BX)(DI*4), X3
	VFMADD231PS X2, X3, X15
	ADDQ $4, DI
	CMPQ DI, CX
	JL opaque_column
	VHADDPS X15, X15, X3
	VHADDPS X3, X3, X3
	VUCOMISS X14, X3
	JA opaque_done
	INCQ DX
	ADDQ SI, R8
	ADDQ R10, R9
	ADDQ R11, BX
	CMPQ DX, CX
	JL opaque_row
opaque_done:
	VMOVSS X3, ret+8(FP)
	VZEROUPPER
	RET
