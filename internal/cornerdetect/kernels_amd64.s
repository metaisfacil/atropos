#include "textflag.h"

// cornerSobelAVX2 evaluates sixteen adjacent Sobel pixels using signed 16-bit
// arithmetic. Source rows include the one-pixel halo on both sides.
TEXT ·cornerSobelAVX2(SB), NOSPLIT, $0-8
	MOVQ args+0(FP), BP
	MOVQ 0(BP), R8
	MOVQ 8(BP), R9
	MOVQ 16(BP), R10
	MOVQ 24(BP), R11
	MOVQ 32(BP), R12
	MOVQ 40(BP), CX
	XORQ DI, DI

sobel_avx2_loop:
	// Left and right column sums for Gx.
	VPMOVZXBW 0(R8)(DI*1), Y0
	VPMOVZXBW 0(R9)(DI*1), Y1
	VPMOVZXBW 0(R10)(DI*1), Y2
	VPMOVZXBW 2(R8)(DI*1), Y3
	VPMOVZXBW 2(R9)(DI*1), Y4
	VPMOVZXBW 2(R10)(DI*1), Y5

	VMOVDQU Y0, Y6
	VPADDW Y1, Y1, Y7
	VPADDW Y7, Y6, Y6
	VPADDW Y2, Y6, Y6
	VMOVDQU Y3, Y8
	VPADDW Y4, Y4, Y7
	VPADDW Y7, Y8, Y8
	VPADDW Y5, Y8, Y8
	VPSUBW Y6, Y8, Y9

	// Top and bottom row sums for Gy.
	VPMOVZXBW 1(R8)(DI*1), Y12
	VMOVDQU Y0, Y10
	VPADDW Y12, Y12, Y12
	VPADDW Y12, Y10, Y10
	VPADDW Y3, Y10, Y10
	VPMOVZXBW 1(R10)(DI*1), Y13
	VMOVDQU Y2, Y11
	VPADDW Y13, Y13, Y13
	VPADDW Y13, Y11, Y11
	VPADDW Y5, Y11, Y11
	VPSUBW Y10, Y11, Y14

	VMOVDQU Y9, (R11)(DI*2)
	VMOVDQU Y14, (R12)(DI*2)
	ADDQ $16, DI
	CMPQ DI, CX
	JL sobel_avx2_loop
	VZEROUPPER
	RET

// cornerBlurAVX2 applies [1 2 1]/4 to sixteen byte lanes per iteration.
TEXT ·cornerBlurAVX2(SB), NOSPLIT, $0-8
	MOVQ args+0(FP), BP
	MOVQ 0(BP), R8
	MOVQ 8(BP), R9
	MOVQ 16(BP), R10
	MOVQ 24(BP), R11
	MOVQ 32(BP), CX
	XORQ DI, DI

blur_avx2_loop:
	VPMOVZXBW (R8)(DI*1), Y0
	VPMOVZXBW (R9)(DI*1), Y1
	VPMOVZXBW (R10)(DI*1), Y2
	VPADDW Y1, Y1, Y1
	VPADDW Y1, Y0, Y0
	VPADDW Y2, Y0, Y0
	VPSRLW $2, Y0, Y0
	VEXTRACTI128 $1, Y0, X1
	VPACKUSWB X1, X0, X0
	VMOVDQU X0, (R11)(DI*1)
	ADDQ $16, DI
	CMPQ DI, CX
	JL blur_avx2_loop
	VZEROUPPER
	RET

// cornerEigenAVX2 evaluates four adjacent structure tensors per iteration.
TEXT ·cornerEigenAVX2(SB), NOSPLIT, $0-8
	MOVQ args+0(FP), BP
	MOVQ 96(BP), R15        // destination
	MOVQ 104(BP), CX        // vectorized element count
	VXORPD Y15, Y15, Y15    // lane-local maxima
	VXORPD Y14, Y14, Y14    // zero for discriminant clamp
	XORQ DI, DI

eigen_avx2_loop:
	// sxx
	MOVQ 0(BP), AX
	VMOVUPD (AX)(DI*8), Y0
	MOVQ 8(BP), AX
	VSUBPD (AX)(DI*8), Y0, Y0
	MOVQ 16(BP), AX
	VSUBPD (AX)(DI*8), Y0, Y0
	MOVQ 24(BP), AX
	VADDPD (AX)(DI*8), Y0, Y0

	// syy
	MOVQ 32(BP), AX
	VMOVUPD (AX)(DI*8), Y1
	MOVQ 40(BP), AX
	VSUBPD (AX)(DI*8), Y1, Y1
	MOVQ 48(BP), AX
	VSUBPD (AX)(DI*8), Y1, Y1
	MOVQ 56(BP), AX
	VADDPD (AX)(DI*8), Y1, Y1

	// sxy
	MOVQ 64(BP), AX
	VMOVUPD (AX)(DI*8), Y2
	MOVQ 72(BP), AX
	VSUBPD (AX)(DI*8), Y2, Y2
	MOVQ 80(BP), AX
	VSUBPD (AX)(DI*8), Y2, Y2
	MOVQ 88(BP), AX
	VADDPD (AX)(DI*8), Y2, Y2

	// trace, determinant and minimum eigenvalue. Avoid FMA so operation
	// ordering matches the scalar Go implementation.
	VADDPD Y1, Y0, Y3
	VMULPD Y1, Y0, Y4
	VMULPD Y2, Y2, Y2
	VSUBPD Y2, Y4, Y4
	VMULPD Y3, Y3, Y5
	VMULPD ·cornerQuarter<>(SB), Y5, Y5
	VSUBPD Y4, Y5, Y5
	VMAXPD Y14, Y5, Y5
	VSQRTPD Y5, Y5
	VMULPD ·cornerHalf<>(SB), Y3, Y3
	VSUBPD Y5, Y3, Y3
	VMOVUPD Y3, (R15)(DI*8)
	VMAXPD Y3, Y15, Y15

	ADDQ $4, DI
	CMPQ DI, CX
	JL eigen_avx2_loop

	VEXTRACTF128 $1, Y15, X13
	VMAXPD X13, X15, X15
	VPERMILPD $1, X15, X13
	VMAXSD X13, X15, X15
	VMOVSD X15, 112(BP)
	VZEROUPPER
	RET

DATA ·cornerQuarter<>+0(SB)/8, $0x3fd0000000000000
DATA ·cornerQuarter<>+8(SB)/8, $0x3fd0000000000000
DATA ·cornerQuarter<>+16(SB)/8, $0x3fd0000000000000
DATA ·cornerQuarter<>+24(SB)/8, $0x3fd0000000000000
GLOBL ·cornerQuarter<>(SB), RODATA|NOPTR, $32

DATA ·cornerHalf<>+0(SB)/8, $0x3fe0000000000000
DATA ·cornerHalf<>+8(SB)/8, $0x3fe0000000000000
DATA ·cornerHalf<>+16(SB)/8, $0x3fe0000000000000
DATA ·cornerHalf<>+24(SB)/8, $0x3fe0000000000000
GLOBL ·cornerHalf<>(SB), RODATA|NOPTR, $32

// cornerTensorEigenAVX2 evaluates four tensors held as exact int32 sums.
TEXT ·cornerTensorEigenAVX2(SB), NOSPLIT, $0-8
	MOVQ args+0(FP), BP
	MOVQ 0(BP), R8
	MOVQ 8(BP), R9
	MOVQ 16(BP), R10
	MOVQ 24(BP), R11
	MOVQ 32(BP), CX
	VXORPD Y15, Y15, Y15
	VXORPD Y14, Y14, Y14
	XORQ DI, DI

tensor_eigen_avx2_loop:
	VCVTDQ2PD (R8)(DI*4), Y0
	VCVTDQ2PD (R9)(DI*4), Y1
	VCVTDQ2PD (R10)(DI*4), Y2
	VADDPD Y1, Y0, Y3
	VMULPD Y1, Y0, Y4
	VMULPD Y2, Y2, Y2
	VSUBPD Y2, Y4, Y4
	VMULPD Y3, Y3, Y5
	VMULPD ·cornerQuarter<>(SB), Y5, Y5
	VSUBPD Y4, Y5, Y5
	VMAXPD Y14, Y5, Y5
	VSQRTPD Y5, Y5
	VMULPD ·cornerHalf<>(SB), Y3, Y3
	VSUBPD Y5, Y3, Y3
	VMOVUPD Y3, (R11)(DI*8)
	VMAXPD Y3, Y15, Y15
	ADDQ $4, DI
	CMPQ DI, CX
	JL tensor_eigen_avx2_loop

	VEXTRACTF128 $1, Y15, X13
	VMAXPD X13, X15, X15
	VPERMILPD $1, X15, X13
	VMAXSD X13, X15, X15
	VMOVSD X15, 40(BP)
	VZEROUPPER
	RET

// cornerResizeGray2AVX2 averages sixteen 2x2 source blocks per iteration.
TEXT ·cornerResizeGray2AVX2(SB), NOSPLIT, $0-8
	MOVQ args+0(FP), BP
	MOVQ 0(BP), R8
	MOVQ 8(BP), R9
	MOVQ 16(BP), R10
	MOVQ 24(BP), CX
	LEAQ ·cornerByteOnes<>(SB), R11
	VMOVDQU (R11), Y15
	XORQ DI, DI
	XORQ DX, DX

resize_gray2_loop:
	VMOVDQU (R8)(DX*1), Y0
	VPMADDUBSW Y15, Y0, Y0
	MOVQ R8, AX
	ADDQ R10, AX
	VMOVDQU (AX)(DX*1), Y1
	VPMADDUBSW Y15, Y1, Y1
	VPADDW Y1, Y0, Y0
	VPSRLW $2, Y0, Y0
	VEXTRACTI128 $1, Y0, X1
	VPACKUSWB X1, X0, X0
	VMOVDQU X0, (R9)(DI*1)
	ADDQ $16, DI
	ADDQ $32, DX
	CMPQ DI, CX
	JL resize_gray2_loop
	VZEROUPPER
	RET

// cornerResizeGray4AVX2 averages eight 4x4 source blocks per iteration.
TEXT ·cornerResizeGray4AVX2(SB), NOSPLIT, $0-8
	MOVQ args+0(FP), BP
	MOVQ 0(BP), R8
	MOVQ 8(BP), R9
	MOVQ 16(BP), R10
	MOVQ 24(BP), CX
	LEAQ ·cornerByteOnes<>(SB), R11
	VMOVDQU (R11), Y15
	LEAQ ·cornerWordOnes<>(SB), R11
	VMOVDQU (R11), Y14
	XORQ DI, DI
	XORQ DX, DX

resize_gray4_loop:
	VMOVDQU (R8)(DX*1), Y0
	VPMADDUBSW Y15, Y0, Y0
	VPMADDWD Y14, Y0, Y0
	MOVQ R8, AX
	ADDQ R10, AX
	VMOVDQU (AX)(DX*1), Y1
	VPMADDUBSW Y15, Y1, Y1
	VPMADDWD Y14, Y1, Y1
	VPADDD Y1, Y0, Y0
	ADDQ R10, AX
	VMOVDQU (AX)(DX*1), Y1
	VPMADDUBSW Y15, Y1, Y1
	VPMADDWD Y14, Y1, Y1
	VPADDD Y1, Y0, Y0
	ADDQ R10, AX
	VMOVDQU (AX)(DX*1), Y1
	VPMADDUBSW Y15, Y1, Y1
	VPMADDWD Y14, Y1, Y1
	VPADDD Y1, Y0, Y0
	VPSRLD $4, Y0, Y0
	VEXTRACTI128 $1, Y0, X1
	VPACKUSDW X1, X0, X0
	VPXOR X2, X2, X2
	VPACKUSWB X2, X0, X0
	VMOVQ X0, (R9)(DI*1)
	ADDQ $8, DI
	ADDQ $32, DX
	CMPQ DI, CX
	JL resize_gray4_loop
	VZEROUPPER
	RET

DATA ·cornerByteOnes<>+0(SB)/8, $0x0101010101010101
DATA ·cornerByteOnes<>+8(SB)/8, $0x0101010101010101
DATA ·cornerByteOnes<>+16(SB)/8, $0x0101010101010101
DATA ·cornerByteOnes<>+24(SB)/8, $0x0101010101010101
GLOBL ·cornerByteOnes<>(SB), RODATA|NOPTR, $32

DATA ·cornerWordOnes<>+0(SB)/8, $0x0001000100010001
DATA ·cornerWordOnes<>+8(SB)/8, $0x0001000100010001
DATA ·cornerWordOnes<>+16(SB)/8, $0x0001000100010001
DATA ·cornerWordOnes<>+24(SB)/8, $0x0001000100010001
GLOBL ·cornerWordOnes<>(SB), RODATA|NOPTR, $32
