//go:build windows

package compose_test

import (
	"os/exec"
	"strconv"
	"syscall"
)

// inOwnGroup gives the program a process group of its own. Windows has no
// signal to send a group, so the group is not what killGroup uses; the flag is
// here so a Ctrl+C at the console reaches the test and not the simulator.
func inOwnGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}

// killGroup ends `go run` and the simulator it started. Process.Kill ends only
// the one process, so the child keeps the stdout pipe open and Wait never
// returns; taskkill /T walks the tree the way a negated pid does on Unix.
func killGroup(cmd *exec.Cmd) {
	_ = exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run()
	_ = cmd.Wait()
}
