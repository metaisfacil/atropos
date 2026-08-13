//go:build !amd64 && !arm64

package main

func levelsVectorCount(int) int                { return 0 }
func applyLevelsSIMD(*levelsKernelArgs)        {}
func grayscaleVectorCount(int) int             { return 0 }
func grayscaleAccentSIMD(*grayscaleKernelArgs) {}
func maskBlendVectorCount(int) int             { return 0 }
func maskBlendSIMD(*maskBlendKernelArgs)       {}
