//go:build !amd64

package patchmatch

func pmActivePatchKernel() string {
	return "scalar"
}
