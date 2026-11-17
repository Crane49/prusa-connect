//go:build !windows

package main

import (
	"context"
	"os/exec"
	"syscall"
	"time"
)

// stopProcess ends the server. On Linux and macOS the restart replaces the
// process in place, so the process this run started is still the server.
func stopProcess(ctx context.Context, cmd *exec.Cmd, _ string) {
	if cmd == nil || cmd.Process == nil {
		return
	}

	// A process that already ended has nothing to signal.
	_ = cmd.Process.Signal(syscall.SIGTERM)

	done := make(chan struct{})

	go func() {
		_ = cmd.Wait() // its exit status says nothing a check reads
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		_ = cmd.Process.Kill() // the run is ending anyway
	case <-time.After(stopLimit):
		_ = cmd.Process.Kill() // a server that ignores SIGTERM still has to go
		<-done
	}
}
