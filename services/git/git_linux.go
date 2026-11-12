// git_linux.go has no console window to hide.

package git

import "os/exec"

// hideConsole does nothing on Linux.
func hideConsole(*exec.Cmd) {}
