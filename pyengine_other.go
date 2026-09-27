//go:build !windows

package main

import "os/exec"

// hideConsoleWindow is a no-op on non-Windows platforms; this build tag
// exists purely so the project can be edited/linted on macOS/Linux even
// though the shipped application targets Windows 10/11 exclusively.
func hideConsoleWindow(_ *exec.Cmd) {}

func isWindows() bool {
	return false
}
