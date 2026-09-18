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

TEXT ·synthesisEncryptCounterHardware(SB), NOSPLIT, $0-16
	MOVQ block+0(FP), AX
	MOVQ keys+8(FP), BX
	MOVOU (AX), X0
	PXOR 0(BX), X0
	AESENC 16(BX), X0
	AESENC 32(BX), X0
	AESENC 48(BX), X0
	AESENC 64(BX), X0
	AESENC 80(BX), X0
	AESENC 96(BX), X0
	AESENC 112(BX), X0
	AESENC 128(BX), X0
	AESENC 144(BX), X0
	AESENCLAST 160(BX), X0
	MOVOU X0, (AX)
	RET
