//go:build !amd64 && !arm64

package main

func cornerSobelVectorCount(int) int   { return 0 }
func cornerEigenVectorCount(int) int   { return 0 }
func cornerBlurVectorCount(int) int    { return 0 }
func cornerSobelSIMD(*cornerSobelArgs) {}
func cornerEigenSIMD(*cornerEigenArgs) {}
func cornerBlurSIMD(*cornerBlurArgs)   {}
