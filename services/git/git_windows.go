// git_windows.go starts git with no console window, which Windows opens for each console program a GUI app starts.

package git

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// hideConsole makes a command start with no console window of its own.
func hideConsole(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
}
