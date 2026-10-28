package agent

import (
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// A group is a job object that holds an agent's program and every program it starts.
type group struct {
	job windows.Handle
}

// prepareCommand makes a command start with no console window of its own.
func prepareCommand(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
}

// startGroup starts a command in a job object that ends every program in it when it closes. The programs the command
// starts join the job too; an agent starts none before it reads its first message, which comes after it joins. It
// returns an error if the job cannot be made or the command cannot be started or join it.
func startGroup(command *exec.Cmd) (group, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return group{}, err
	}
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE},
	}
	_, err = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits)))
	if err == nil {
		err = command.Start()
	}
	if err != nil {
		_ = windows.CloseHandle(job)
		return group{}, err
	}
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false,
		uint32(command.Process.Pid))
	if err == nil {
		err = windows.AssignProcessToJobObject(job, process)
		_ = windows.CloseHandle(process)
	}
	if err != nil {
		_ = command.Process.Kill()
		_ = windows.CloseHandle(job)
		return group{}, err
	}
	return group{job: job}, nil
}

// end ends every program in the group.
func (group group) end() { _ = windows.CloseHandle(group.job) }
