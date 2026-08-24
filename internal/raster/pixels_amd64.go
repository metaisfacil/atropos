//go:build amd64

package raster

import "golang.org/x/sys/cpu"

var pixelUseAVX2 = cpu.X86.HasAVX2

func levelsVectorCount(n int) int {
	if !pixelUseAVX2 {
		return 0
	}
	return n &^ 15
}

func grayscaleVectorCount(n int) int {
	if !pixelUseAVX2 {
		return 0
	}
	return n &^ 3
}

func maskBlendVectorCount(n int) int {
	if !pixelUseAVX2 {
		return 0
	}
	return n &^ 3
}

func applyLevelsSIMD(args *levelsKernelArgs)        { applyLevelsAVX2(args) }
func grayscaleAccentSIMD(args *grayscaleKernelArgs) { grayscaleAccentAVX2(args) }
func maskBlendSIMD(args *maskBlendKernelArgs)       { maskBlendAVX2(args) }

//go:noescape
func applyLevelsAVX2(args *levelsKernelArgs)

//go:noescape
func grayscaleAccentAVX2(args *grayscaleKernelArgs)

//go:noescape
func maskBlendAVX2(args *maskBlendKernelArgs)
