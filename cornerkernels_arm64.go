//go:build arm64

package main

import "golang.org/x/sys/cpu"

var cornerUseNEON = cpu.ARM64.HasASIMD

func cornerSobelVectorCount(n int) int {
	if !cornerUseNEON {
		return 0
	}
	return n &^ 15
}

// Go 1.22's ARM64 assembler does not expose packed float64 arithmetic, so the
// eigenvalue stage retains its scalar implementation on ARM64. The integer
// Sobel stage still uses all 128 bits of each NEON vector.
func cornerEigenVectorCount(int) int           { return 0 }
func cornerBlurVectorCount(int) int            { return 0 }
func cornerResizeGrayVectorCount(int, int) int { return 0 }

func cornerSobelSIMD(args *cornerSobelArgs) {
	cornerSobelNEON(args)
}

func cornerEigenSIMD(*cornerEigenArgs)             {}
func cornerBlurSIMD(*cornerBlurArgs)               {}
func cornerTensorEigenSIMD(*cornerTensorEigenArgs) {}
func cornerResizeGray2SIMD(*cornerResizeGrayArgs)  {}
func cornerResizeGray4SIMD(*cornerResizeGrayArgs)  {}

//go:noescape
func cornerSobelNEON(args *cornerSobelArgs)
