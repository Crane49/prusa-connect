package resourcehost_test

import (
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

// childProbe holds a handle on the child, opened while the child runs. The
// handle names this process and no other, even once the system gives its pid
// to a new process.
type childProbe struct {
	handle syscall.Handle
}

// watchChild opens the child so the case can later ask whether it ended.
func watchChild(t *testing.T, pid int) childProbe {
	t.Helper()

	handle, err := syscall.OpenProcess(syscall.SYNCHRONIZE, false, uint32(pid))
	require.NoError(t, err, "the child %d is not running before the stop", pid)

	t.Cleanup(func() { _ = syscall.CloseHandle(handle) })

	return childProbe{handle: handle}
}

// gone reports whether the child has already ended. A process handle is
// signalled when the process exits, and a wait of zero only reads that state,
// so a stop that returned before its child ended fails here.
func (p childProbe) gone() bool {
	event, err := syscall.WaitForSingleObject(p.handle, 0)

	return err == nil && event == syscall.WAIT_OBJECT_0
}
