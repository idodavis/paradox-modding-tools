// process_linux.go holds an agent's programs in a process group.

package agent

import (
	"os/exec"
	"syscall"
)

type group struct {
	pid int
}

func prepareCommand(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// startGroup starts a command as the leader of its group.
func startGroup(command *exec.Cmd) (group, error) {
	if err := command.Start(); err != nil {
		return group{}, err
	}

	return group{pid: command.Process.Pid}, nil
}

func (g group) end() { _ = syscall.Kill(-g.pid, syscall.SIGKILL) }
