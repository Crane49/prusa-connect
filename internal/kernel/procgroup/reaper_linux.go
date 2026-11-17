package procgroup

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"

	"golang.org/x/sys/unix"
)

// Reaper makes this process the Linux subreaper of everything it starts and
// collects the zombies of orphans it adopts. Only zombies outside this
// process's own process group are collected, so a plain exec.Command child
// is never touched, and children started through Start are left to their
// cmd.Wait.
type Reaper struct {
	mu    sync.Mutex
	owned map[int]struct{}
}

// NewReaper marks this process as a child subreaper. If the kernel refuses,
// orphans go to init as before and Run finds nothing to collect.
func NewReaper() *Reaper {
	_ = unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 1, 0, 0, 0) // refusal leaves the reaper with nothing to do.

	return &Reaper{owned: map[int]struct{}{}}
}

// Run collects orphan zombies on SIGCHLD until ctx is done.
func (r *Reaper) Run(ctx context.Context) {
	sigchld := make(chan os.Signal, 1)
	signal.Notify(sigchld, syscall.SIGCHLD)
	defer signal.Stop(sigchld)

	for {
		r.reap()

		select {
		case <-ctx.Done():
			return
		case <-sigchld:
		}
	}
}

// reap collects every zombie child that is neither owned nor in this
// process's own group. It holds the lock across the scan, so a child started
// through Start is always owned by the time reap can see it.
func (r *Reaper) reap() {
	r.mu.Lock()
	defer r.mu.Unlock()

	procs, err := readProcs()
	if err != nil {
		return // the next SIGCHLD tries again.
	}

	self, group := os.Getpid(), syscall.Getpgrp()

	for _, p := range procs {
		if p.ppid != self || p.state != 'Z' || p.pgrp == group {
			continue
		}

		if _, owned := r.owned[p.pid]; owned {
			continue
		}

		var status unix.WaitStatus
		_, _ = unix.Wait4(p.pid, &status, unix.WNOHANG, nil) // an orphan's exit status has no reader.
	}
}

// Start starts cmd and keeps the Reaper away from it until release is called.
// Call release after cmd.Wait.
func (r *Reaper) Start(cmd *exec.Cmd) (release func(), err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err = cmd.Start(); err != nil {
		return nil, fmt.Errorf("procgroup: start: %w", err)
	}

	pid := cmd.Process.Pid
	r.owned[pid] = struct{}{}

	return func() {
		r.mu.Lock()
		defer r.mu.Unlock()

		delete(r.owned, pid)
	}, nil
}
