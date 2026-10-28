package agent

import (
	"os/exec"
	"syscall"
)

// A group is the process group that holds an agent's program and every program it starts.
type group struct {
	pid int // the group's id, which is its first program's
}

// prepareCommand makes a command start in a process group of its own.
func prepareCommand(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// startGroup starts a command, which leads its own process group. It returns an error if the command cannot be
// started.
func startGroup(command *exec.Cmd) (group, error) {
	if err := command.Start(); err != nil {
		return group{}, err
	}
	return group{pid: command.Process.Pid}, nil
}

// end ends every program in the group.
func (group group) end() { _ = syscall.Kill(-group.pid, syscall.SIGKILL) }
