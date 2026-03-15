package main

import (
	"os/exec"
	"syscall"
)

// hideCommandWindow sets the Windows-specific CREATE_NO_WINDOW flag so that
// shelling out to ImageMagick doesn't flash a console window.
func hideCommandWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}
