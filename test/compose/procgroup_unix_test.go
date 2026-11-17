//go:build !windows

package compose_test

import (
	"os/exec"
	"syscall"
)

// inOwnGroup puts the program in its own process group, because `go run`
// builds a program and then execs it as a child: killing `go run` leaves the
// simulator running, holding the stdout pipe open, and Wait then blocks on a
// pipe nothing will ever close. The group is what has to be killed, and this
// is what makes that possible.
func inOwnGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killGroup ends the group inOwnGroup created. The negated pid is the process
// group, which is `go run` and the simulator it exec'd. Wait is then the
// ordinary reap.
func killGroup(cmd *exec.Cmd) {
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	_ = cmd.Wait()
}
