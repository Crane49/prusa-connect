//go:build unix

package sandbox

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

func group(command *exec.Cmd) {
	if command.SysProcAttr == nil {
		command.SysProcAttr = &syscall.SysProcAttr{}
	}

	command.SysProcAttr.Setpgid = true
	dieWithParent(command.SysProcAttr)
}

func stop(command *exec.Cmd) error {
	if command.Process == nil {
		return nil
	}

	// The group id is the pid of its leader, which the slicer is.
	err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	if err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}

	return os.ErrProcessDone
}

// Group is the process group a started command leads.
func Group(command *exec.Cmd) int {
	if command.Process == nil {
		return 0
	}

	return command.Process.Pid
}
