//go:build !windows

package app

import (
	"errors"
	"syscall"
)

// gone reports a process that has exited and been reaped. A child that exited
// and nobody waited on still answers signal 0, as a zombie.
func gone(pid int) bool {
	return errors.Is(syscall.Kill(pid, syscall.Signal(0)), syscall.ESRCH)
}
