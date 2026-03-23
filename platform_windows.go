package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/windows"
)

// hideCommandWindow sets the Windows-specific CREATE_NO_WINDOW flag so that
// shelling out to ImageMagick doesn't flash a console window.
func hideCommandWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}

// webviewUserDataPath returns the WebView2 user data directory for this
// instance.  It tries the stable per-user path first so that localStorage
// preferences survive across launches.  If that directory is already locked by
// another running instance, a PID-keyed temporary path is returned instead —
// keyboard shortcuts work correctly in all instances, and only the extra
// instances lose persistent settings for that session.
func webviewUserDataPath() string {
	stablePath := stableWebviewDataPath()
	if !webviewDataPathLocked(stablePath) {
		return stablePath
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("atropos-webview-%d", os.Getpid()))
}

// stableWebviewDataPath returns the canonical per-user data directory.
func stableWebviewDataPath() string {
	if dir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(dir, "atropos")
	}
	return filepath.Join(os.TempDir(), "atropos-webview")
}

// webviewDataPathLocked reports whether another process already holds the
// WebView2 lock inside dir.  It attempts to open the lock file with an
// exclusive share mode; if the open fails with ERROR_SHARING_VIOLATION the
// directory is in use by another instance.
func webviewDataPathLocked(dir string) bool {
	lockFile := filepath.Join(dir, "EBWebView", "lockfile")
	p, err := windows.UTF16PtrFromString(lockFile)
	if err != nil {
		return false
	}
	h, err := windows.CreateFile(
		p,
		windows.GENERIC_READ,
		0, // no sharing — exclusive
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		// ERROR_SHARING_VIOLATION (0x20) means another process has it open.
		// Any other error (e.g. file not found) means no lock is held yet.
		if errno, ok := err.(syscall.Errno); ok && errno == windows.ERROR_SHARING_VIOLATION {
			return true
		}
		return false
	}
	windows.CloseHandle(h)
	return false
}
