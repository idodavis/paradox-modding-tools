// git_windows.go hides the console window Windows opens for git.

package git

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// hideConsole starts the command with no console window.
func hideConsole(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
}
