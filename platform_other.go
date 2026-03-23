//go:build !windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
)

// hideCommandWindow is a no-op on non-Windows platforms.
func hideCommandWindow(_ *exec.Cmd) {}

// webviewUserDataPath returns a stable per-user WebView2 data directory so
// that user preferences (localStorage) persist across launches.  On non-Windows
// platforms there is no inter-process folder lock, so the same path is always
// safe to reuse regardless of how many instances are running.
func webviewUserDataPath() string {
	if dir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(dir, "atropos")
	}
	return filepath.Join(os.TempDir(), "atropos-webview")
}
