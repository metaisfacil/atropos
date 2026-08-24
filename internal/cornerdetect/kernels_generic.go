//go:build !amd64 && !arm64

package cornerdetect

func cornerSobelVectorCount(int) int               { return 0 }
func cornerEigenVectorCount(int) int               { return 0 }
func cornerBlurVectorCount(int) int                { return 0 }
func cornerResizeGrayVectorCount(int, int) int     { return 0 }
func cornerSobelSIMD(*cornerSobelArgs)             {}
func cornerEigenSIMD(*cornerEigenArgs)             {}
func cornerBlurSIMD(*cornerBlurArgs)               {}
func cornerTensorEigenSIMD(*cornerTensorEigenArgs) {}
func cornerResizeGray2SIMD(*cornerResizeGrayArgs)  {}
func cornerResizeGray4SIMD(*cornerResizeGrayArgs)  {}
