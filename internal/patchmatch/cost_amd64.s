#include "textflag.h"

// Masks for odd SIMD tails of 1, 3, 5, and 7 float32 lanes.
DATA ·pmOddTailMasks+0(SB)/4, $0xffffffff
DATA ·pmOddTailMasks+4(SB)/4, $0
DATA ·pmOddTailMasks+8(SB)/4, $0
DATA ·pmOddTailMasks+12(SB)/4, $0
DATA ·pmOddTailMasks+16(SB)/4, $0
DATA ·pmOddTailMasks+20(SB)/4, $0
DATA ·pmOddTailMasks+24(SB)/4, $0
DATA ·pmOddTailMasks+28(SB)/4, $0

DATA ·pmOddTailMasks+32(SB)/4, $0xffffffff
DATA ·pmOddTailMasks+36(SB)/4, $0xffffffff
DATA ·pmOddTailMasks+40(SB)/4, $0xffffffff
DATA ·pmOddTailMasks+44(SB)/4, $0
DATA ·pmOddTailMasks+48(SB)/4, $0
DATA ·pmOddTailMasks+52(SB)/4, $0
DATA ·pmOddTailMasks+56(SB)/4, $0
DATA ·pmOddTailMasks+60(SB)/4, $0

DATA ·pmOddTailMasks+64(SB)/4, $0xffffffff
DATA ·pmOddTailMasks+68(SB)/4, $0xffffffff
DATA ·pmOddTailMasks+72(SB)/4, $0xffffffff
DATA ·pmOddTailMasks+76(SB)/4, $0xffffffff
DATA ·pmOddTailMasks+80(SB)/4, $0xffffffff
DATA ·pmOddTailMasks+84(SB)/4, $0
DATA ·pmOddTailMasks+88(SB)/4, $0
DATA ·pmOddTailMasks+92(SB)/4, $0

DATA ·pmOddTailMasks+96(SB)/4, $0xffffffff
DATA ·pmOddTailMasks+100(SB)/4, $0xffffffff
DATA ·pmOddTailMasks+104(SB)/4, $0xffffffff
DATA ·pmOddTailMasks+108(SB)/4, $0xffffffff
DATA ·pmOddTailMasks+112(SB)/4, $0xffffffff
DATA ·pmOddTailMasks+116(SB)/4, $0xffffffff
DATA ·pmOddTailMasks+120(SB)/4, $0xffffffff
DATA ·pmOddTailMasks+124(SB)/4, $0
GLOBL ·pmOddTailMasks(SB), RODATA|NOPTR, $128

// pmPatchSSDAVX2 evaluates four premultiplied channels eight pixels at a time.
// The packed rows are padded, so masked tail loads never cross an allocation.
TEXT ·pmPatchSSDAVX2(SB), NOSPLIT, $0-12
	MOVQ args+0(FP), BP
	MOVQ 72(BP), SI          // packed row stride, float32 elements
	SHLQ $2, SI              // bytes
	MOVQ 80(BP), CX          // patch size
	VMOVSS 88(BP), X14       // raw early-exit limit
	VXORPS Y15, Y15, Y15     // accumulated weighted SSD
	XORQ R12, R12            // row byte offset
	XORQ DX, DX              // row

row_loop:
	MOVQ 0(BP), R8
	ADDQ R12, R8
	MOVQ 8(BP), R9
	ADDQ R12, R9
	MOVQ 16(BP), R10
	ADDQ R12, R10
	MOVQ 24(BP), R11
	ADDQ R12, R11
	MOVQ 64(BP), BX
	ADDQ R12, BX
	XORQ DI, DI              // column

column_loop:
	MOVQ CX, AX
	SUBQ DI, AX              // remaining pixels
	VMOVUPS (BX)(DI*4), Y0   // confidence
	CMPQ AX, $8
	JGE channels

	// Every supported patch size is odd. Map tails 1/3/5/7 to mask slots 0..3.
	MOVQ AX, R13
	SHRQ $1, R13
	SHLQ $5, R13
	LEAQ ·pmOddTailMasks(SB), AX
	ADDQ R13, AX
	VMOVUPS (AX), Y3
	VANDPS Y3, Y0, Y0

channels:
	MOVQ 32(BP), AX
	ADDQ R12, AX
	VMOVUPS (R8)(DI*4), Y1
	VSUBPS (AX)(DI*4), Y1, Y1
	VMULPS Y0, Y1, Y2
	VFMADD231PS Y1, Y2, Y15

	MOVQ 40(BP), AX
	ADDQ R12, AX
	VMOVUPS (R9)(DI*4), Y1
	VSUBPS (AX)(DI*4), Y1, Y1
	VMULPS Y0, Y1, Y2
	VFMADD231PS Y1, Y2, Y15

	MOVQ 48(BP), AX
	ADDQ R12, AX
	VMOVUPS (R10)(DI*4), Y1
	VSUBPS (AX)(DI*4), Y1, Y1
	VMULPS Y0, Y1, Y2
	VFMADD231PS Y1, Y2, Y15

	MOVQ 56(BP), AX
	ADDQ R12, AX
	VMOVUPS (R11)(DI*4), Y1
	VSUBPS (AX)(DI*4), Y1, Y1
	VMULPS Y0, Y1, Y2
	VFMADD231PS Y1, Y2, Y15

	ADDQ $8, DI
	CMPQ DI, CX
	JL column_loop

	// Check the monotonically increasing partial sum once per patch row.
	VEXTRACTF128 $1, Y15, X3
	VADDPS X15, X3, X3
	VHADDPS X3, X3, X3
	VHADDPS X3, X3, X3
	VUCOMISS X14, X3
	JA done

	INCQ DX
	ADDQ SI, R12
	CMPQ DX, CX
	JL row_loop

done:
	VMOVSS X3, ret+8(FP)
	VZEROUPPER
	RET
