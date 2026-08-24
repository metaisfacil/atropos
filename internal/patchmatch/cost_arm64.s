#include "textflag.h"

DATA ·pmNEONConstants+0(SB)/4, $0x3f800000
DATA ·pmNEONConstants+4(SB)/4, $0x3f800000
DATA ·pmNEONConstants+8(SB)/4, $0x3f800000
DATA ·pmNEONConstants+12(SB)/4, $0x3f800000
DATA ·pmNEONConstants+16(SB)/4, $0x80000000
DATA ·pmNEONConstants+20(SB)/4, $0x80000000
DATA ·pmNEONConstants+24(SB)/4, $0x80000000
DATA ·pmNEONConstants+28(SB)/4, $0x80000000
GLOBL ·pmNEONConstants(SB), RODATA|NOPTR, $32

// pmPatchSSDNEON evaluates four premultiplied channels in four-pixel vectors.
// Odd tail pixels use scalar ARM64 FP instructions in the same leaf function.
TEXT ·pmPatchSSDNEON(SB), NOSPLIT, $16-12
	MOVD args+0(FP), R0
	MOVD 72(R0), R6
	LSL $2, R6, R6        // packed stride in bytes
	MOVD 80(R0), R7       // patch size
	FMOVS 88(R0), F28     // raw early-exit limit
	FMOVS ZR, F29         // scalar-tail accumulator
	VEOR V15.B16, V15.B16, V15.B16
	MOVD $·pmNEONConstants(SB), R12
	VLD1 (R12), [V30.S4, V31.S4] // ones, float sign bits
	MOVD $0, R8           // row byte offset
	MOVD $0, R9           // row

neon_row:
	MOVD 0(R0), R1
	ADD R8, R1, R1
	MOVD 8(R0), R2
	ADD R8, R2, R2
	MOVD 16(R0), R3
	ADD R8, R3, R3
	MOVD 24(R0), R4
	ADD R8, R4, R4
	MOVD 64(R0), R5
	ADD R8, R5, R5
	MOVD $0, R11         // column

neon_vector_test:
	ADD $4, R11, R13
	CMP R7, R13
	BGT neon_scalar
	LSL $2, R11, R13
	ADD R13, R5, R14
	VLD1 (R14), [V0.S4]

	// Red channel.
	ADD R13, R1, R14
	VLD1 (R14), [V1.S4]
	MOVD 32(R0), R10
	ADD R8, R10, R10
	ADD R13, R10, R10
	VLD1 (R10), [V2.S4]
	VEOR V31.B16, V2.B16, V2.B16
	VFMLA V2.S4, V30.S4, V1.S4
	VEOR V3.B16, V3.B16, V3.B16
	VFMLA V1.S4, V1.S4, V3.S4
	VFMLA V0.S4, V3.S4, V15.S4

	// Green channel.
	ADD R13, R2, R14
	VLD1 (R14), [V1.S4]
	MOVD 40(R0), R10
	ADD R8, R10, R10
	ADD R13, R10, R10
	VLD1 (R10), [V2.S4]
	VEOR V31.B16, V2.B16, V2.B16
	VFMLA V2.S4, V30.S4, V1.S4
	VEOR V3.B16, V3.B16, V3.B16
	VFMLA V1.S4, V1.S4, V3.S4
	VFMLA V0.S4, V3.S4, V15.S4

	// Blue channel.
	ADD R13, R3, R14
	VLD1 (R14), [V1.S4]
	MOVD 48(R0), R10
	ADD R8, R10, R10
	ADD R13, R10, R10
	VLD1 (R10), [V2.S4]
	VEOR V31.B16, V2.B16, V2.B16
	VFMLA V2.S4, V30.S4, V1.S4
	VEOR V3.B16, V3.B16, V3.B16
	VFMLA V1.S4, V1.S4, V3.S4
	VFMLA V0.S4, V3.S4, V15.S4

	// Alpha channel.
	ADD R13, R4, R14
	VLD1 (R14), [V1.S4]
	MOVD 56(R0), R10
	ADD R8, R10, R10
	ADD R13, R10, R10
	VLD1 (R10), [V2.S4]
	VEOR V31.B16, V2.B16, V2.B16
	VFMLA V2.S4, V30.S4, V1.S4
	VEOR V3.B16, V3.B16, V3.B16
	VFMLA V1.S4, V1.S4, V3.S4
	VFMLA V0.S4, V3.S4, V15.S4

	ADD $4, R11, R11
	B neon_vector_test

neon_scalar:
	CMP R7, R11
	BGE neon_row_sum
	LSL $2, R11, R13
	ADD R13, R5, R14
	FMOVS (R14), F0

	ADD R13, R1, R14
	FMOVS (R14), F1
	MOVD 32(R0), R10
	ADD R8, R10, R10
	ADD R13, R10, R10
	FMOVS (R10), F2
	FSUBS F2, F1, F1
	FMULS F1, F1, F1
	FMULS F0, F1, F1
	FADDS F1, F29, F29

	ADD R13, R2, R14
	FMOVS (R14), F1
	MOVD 40(R0), R10
	ADD R8, R10, R10
	ADD R13, R10, R10
	FMOVS (R10), F2
	FSUBS F2, F1, F1
	FMULS F1, F1, F1
	FMULS F0, F1, F1
	FADDS F1, F29, F29

	ADD R13, R3, R14
	FMOVS (R14), F1
	MOVD 48(R0), R10
	ADD R8, R10, R10
	ADD R13, R10, R10
	FMOVS (R10), F2
	FSUBS F2, F1, F1
	FMULS F1, F1, F1
	FMULS F0, F1, F1
	FADDS F1, F29, F29

	ADD R13, R4, R14
	FMOVS (R14), F1
	MOVD 56(R0), R10
	ADD R8, R10, R10
	ADD R13, R10, R10
	FMOVS (R10), F2
	FSUBS F2, F1, F1
	FMULS F1, F1, F1
	FMULS F0, F1, F1
	FADDS F1, F29, F29

	ADD $1, R11, R11
	B neon_scalar

neon_row_sum:
	VST1 [V15.S4], (RSP)
	FMOVS 0(RSP), F27
	FMOVS 4(RSP), F0
	FADDS F0, F27, F27
	FMOVS 8(RSP), F0
	FADDS F0, F27, F27
	FMOVS 12(RSP), F0
	FADDS F0, F27, F27
	FADDS F29, F27, F27
	FCMPS F28, F27
	BGT neon_done

	ADD $1, R9, R9
	ADD R6, R8, R8
	CMP R7, R9
	BLT neon_row

neon_done:
	FMOVS F27, ret+8(FP)
	RET
