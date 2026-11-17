//go:build !windows

package main

import (
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"github.com/prusa3d/prusa-connect/internal/kernel/procgroup"
)

// takeTerminal makes this process its own foreground group, so Ctrl+C does
// not also hit the make or go run that started it. The result undoes it.
func takeTerminal(tty *os.File) (giveBack func()) {
	fd := int(tty.Fd())
	group := syscall.Getpgrp()

	if foreground, err := unix.IoctlGetInt(fd, unix.TIOCGPGRP); err != nil || foreground != group {
		return func() {}
	}

	// A background group that sets the foreground group gets SIGTTOU. The
	// new group is in the background until TIOCSPGRP below.
	signal.Ignore(syscall.SIGTTOU)

	if syscall.Setpgid(0, 0) != nil || unix.IoctlSetPointerInt(fd, unix.TIOCSPGRP, syscall.Getpid()) != nil {
		_ = syscall.Setpgid(0, group) // back in the group it started in, which still holds the terminal.

		signal.Reset(syscall.SIGTTOU)

		return func() {}
	}

	// Either call fails only when the parent's group is gone. Then there is
	// nothing to give the terminal back to.
	return func() {
		_ = unix.IoctlSetPointerInt(fd, unix.TIOCSPGRP, group)
		_ = syscall.Setpgid(0, group)

		signal.Reset(syscall.SIGTTOU)
	}
}

// ownGroup puts command in its own process group, so a signal reaches its
// children too. Not procgroup.Prepare: a new session would lose the terminal.
func ownGroup(command *exec.Cmd) {
	if command.SysProcAttr == nil {
		command.SysProcAttr = &syscall.SysProcAttr{}
	}

	command.SysProcAttr.Setpgid = true
}

// group returns the group command leads. command must have started.
func group(command *exec.Cmd) *procgroup.Group {
	g, _ := procgroup.Attach(command)

	return g
}

// interruptGroup sends SIGINT to command's group, as Ctrl+C does.
func interruptGroup(command *exec.Cmd) {
	_ = group(command).Signal(syscall.SIGINT) // a group that has gone already is stopped.
}

// endGroup waits for the rest of command's group and kills it after
// stopTimeout. make exits on Ctrl+C before the demo it started has stopped.
func endGroup(command *exec.Cmd) {
	g := group(command)
	_ = g.Signal(syscall.SIGINT) // a group that has gone already is stopped.

	for deadline := time.Now().Add(stopTimeout); time.Now().Before(deadline); {
		if !g.Alive() {
			return
		}

		time.Sleep(answerPoll)
	}

	_ = g.Kill() // only a failure to signal is left, and nothing more can be done.
}
