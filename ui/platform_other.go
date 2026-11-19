//go:build !windows

package ui

import (
	"os/exec"
	"runtime"

	"github.com/ncruces/zenity"
)

// InitPlatformDPI returns the DPI awareness mode on non-Windows platforms.
func InitPlatformDPI() string { return "native" }

func attachWindowsWindowHooks(_ uintptr, _ []byte, _ func([]string)) {}

func attachWindowOption(_ uintptr) (zenity.Option, bool) {
	return nil, false
}

func openSystemURL(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}

func querySpatialKeyLabels() map[string]string {
	return defaultSpatialKeyLabels()
}
