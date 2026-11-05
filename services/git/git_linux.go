// git_linux.go is git's start on Linux, where a program opens no console window.

package git

import "os/exec"

// hideConsole does nothing: Linux opens no console window for a program.
func hideConsole(*exec.Cmd) {}
