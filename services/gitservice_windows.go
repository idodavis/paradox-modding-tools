//go:build windows

// gitservice_windows.go keeps git's console window from flashing up: PMT is a windowed program, and
// Windows gives a console program it starts a window of its own.

package services

import (
	"os/exec"
	"syscall"
)

const createNoWindow = 0x08000000 // CREATE_NO_WINDOW

func init() {
	hideConsole = func(cmd *exec.Cmd) {
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	}
}
