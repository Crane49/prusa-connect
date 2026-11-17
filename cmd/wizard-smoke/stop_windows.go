//go:build windows

package main

import (
	"context"
	"os/exec"
)

// stopProcess ends the server. On Windows the restart starts a new process
// that outlives the one this run started, so the server is found by its file
// name, with every process it started.
func stopProcess(ctx context.Context, cmd *exec.Cmd, name string) {
	// A server that is already gone leaves taskkill nothing to do.
	_ = exec.CommandContext(ctx, "taskkill", "/F", "/T", "/IM", name).Run()

	if cmd != nil && cmd.Process != nil {
		_ = cmd.Wait() // the first process ended at the restart, or just now
	}
}
