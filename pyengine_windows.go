//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// hideConsoleWindow prevents a visible console window from flashing up
// behind the app when the Python engine (or a bundled PyInstaller exe)
// is spawned - required for point 12 of the acceptance checklist:
// "No console window appears for the production build."
func hideConsoleWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
}

func isWindows() bool {
	return true
}
