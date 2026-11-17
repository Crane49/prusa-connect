//go:build !windows

package procgroup

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// Group is a started command together with every process it starts.
type Group struct {
	pid int
}

// Prepare makes cmd the leader of a session, and so of a process group, of
// its own. The session holds a process that moves to another group, so Kill
// can still find it on Linux once its parent has exited. Call it before
// cmd.Start.
func Prepare(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}

	cmd.SysProcAttr.Setsid = true
}

// Attach returns the group cmd leads. Call it after cmd.Start.
func Attach(cmd *exec.Cmd) (*Group, error) {
	if cmd.Process == nil {
		return nil, errors.New("procgroup: attach before the command started")
	}

	return &Group{pid: cmd.Process.Pid}, nil
}

// Kill sends SIGKILL to every process in the group and to every process the
// walk in descendants finds, and to the group of each of those that leads one
// of its own. A process that has gone already is not an error. When the
// descendants cannot be listed, the group is still killed.
func (g *Group) Kill() error {
	// The walk comes first: once the group dies its descendants lose their
	// parent links and the walk no longer finds them.
	procs, err := g.descendants()
	err = errors.Join(err, sigkill(-g.pid))

	for _, p := range procs {
		err = errors.Join(err, sigkill(p.pid))

		if p.pid == p.pgrp && p.pgrp != g.pid {
			err = errors.Join(err, sigkill(-p.pgrp))
		}
	}

	return err
}

// sigkill sends SIGKILL to target, a pid, or a process group when negative.
func sigkill(target int) error {
	if err := syscall.Kill(target, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("procgroup: kill %d: %w", target, err)
	}

	return nil
}

// Signal sends sig to every process in the group.
func (g *Group) Signal(sig os.Signal) error {
	s, ok := sig.(syscall.Signal)
	if !ok {
		return fmt.Errorf("procgroup: %v is not a unix signal", sig)
	}

	if err := syscall.Kill(-g.pid, s); err != nil {
		return fmt.Errorf("procgroup: signal group %d: %w", g.pid, err)
	}

	return nil
}

// Alive reports whether any process is left in the group. A leader that has
// exited but was not waited for still counts.
func (g *Group) Alive() bool {
	return !errors.Is(syscall.Kill(-g.pid, 0), syscall.ESRCH)
}

// LimitMemory does nothing on unix: a process group has no memory limit.
func (g *Group) LimitMemory(int64) error { return nil }

// Close does nothing on unix: a process group holds no handle.
func (g *Group) Close() error { return nil }
