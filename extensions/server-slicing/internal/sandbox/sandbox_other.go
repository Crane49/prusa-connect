//go:build !unix

package sandbox

import (
	"errors"
	"os"
	"os/exec"
)

func group(*exec.Cmd) {}

func stop(command *exec.Cmd) error {
	if command.Process == nil {
		return nil
	}

	if err := command.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}

	return os.ErrProcessDone
}

// Group is the pid of a started command. A host with no process groups stops
// the slicer alone.
func Group(command *exec.Cmd) int {
	if command.Process == nil {
		return 0
	}

	return command.Process.Pid
}
