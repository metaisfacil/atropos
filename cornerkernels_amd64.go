//go:build amd64

package main

import "golang.org/x/sys/cpu"

var cornerUseAVX2 = cpu.X86.HasAVX2

func cornerSobelVectorCount(n int) int {
	if !cornerUseAVX2 {
		return 0
	}
	return n &^ 15
}

func cornerBlurVectorCount(n int) int {
	if !cornerUseAVX2 {
		return 0
	}
	return n &^ 15
}

func cornerEigenVectorCount(n int) int {
	if !cornerUseAVX2 {
		return 0
	}
	return n &^ 3
}

func cornerSobelSIMD(args *cornerSobelArgs) {
	cornerSobelAVX2(args)
}

func cornerEigenSIMD(args *cornerEigenArgs) {
	cornerEigenAVX2(args)
}

func cornerBlurSIMD(args *cornerBlurArgs) {
	cornerBlurAVX2(args)
}

//go:noescape
func cornerSobelAVX2(args *cornerSobelArgs)

//go:noescape
func cornerEigenAVX2(args *cornerEigenArgs)

//go:noescape
func cornerBlurAVX2(args *cornerBlurArgs)
