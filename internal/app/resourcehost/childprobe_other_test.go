//go:build !windows

package resourcehost_test

import (
	"os"
	"syscall"
	"testing"
)

// childProbe remembers the child's pid. On these systems the check is made
// after the stop, by pid.
type childProbe struct {
	pid int
}

// watchChild records the child so the case can later ask whether it ended.
func watchChild(t *testing.T, pid int) childProbe {
	t.Helper()

	return childProbe{pid: pid}
}

// gone reports a pid that names no running process. The lookup always
// succeeds here, so the null signal is the question. It asks after the
// process without touching it, and it fails once the process is reaped.
func (p childProbe) gone() bool {
	process, err := os.FindProcess(p.pid)
	if err != nil {
		return true
	}

	return process.Signal(syscall.Signal(0)) != nil
}
