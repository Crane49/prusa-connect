//go:build !windows

package compose

import (
	"fmt"
	"os"
	"syscall"

	"github.com/prusa3d/prusa-connect/internal/kernel/procgroup"
)

// restart replaces this process with a new run of the same binary and the same
// arguments, so the server starts again on the file the first-run wizard wrote
// (ADR-20261015-ejz rule 21). The process id stays, so a supervisor, a terminal and a
// container all keep the process they started. It returns only on failure.
func restart() error {
	binary, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find this binary to start it again: %w", err)
	}

	return restartAt(binary)
}

// restartAt starts binary again with this process's arguments minus -safe.
func restartAt(binary string) error {
	procgroup.PassPortRightToExec()

	//nolint:gosec // the binary and the arguments are this process's own.
	if err := syscall.Exec(binary, withoutSafe(os.Args), os.Environ()); err != nil {
		return fmt.Errorf("start %s again: %w", binary, err)
	}

	return nil
}
