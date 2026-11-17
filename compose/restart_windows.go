//go:build windows

package compose

import (
	"fmt"
	"os"
	"os/exec"
)

// restart starts a new run of the same binary with the same arguments on the
// same console, and returns so this process can exit (ADR-20261015-ejz rule 21).
// Windows has no call that replaces a running process, so the new one is a
// child that outlives its parent.
func restart() error {
	binary, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find this binary to start it again: %w", err)
	}

	return restartAt(binary)
}

// restartAt starts binary again with this process's arguments minus -safe.
func restartAt(binary string) error {
	//nolint:gosec,noctx // the binary and the arguments are this process's own, and the child outlives this one.
	next := exec.Command(binary, withoutSafe(os.Args)[1:]...)
	next.Stdin, next.Stdout, next.Stderr = os.Stdin, os.Stdout, os.Stderr

	if err := next.Start(); err != nil {
		return fmt.Errorf("start %s again: %w", binary, err)
	}

	return nil
}
