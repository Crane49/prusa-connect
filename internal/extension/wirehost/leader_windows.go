package wirehost

import (
	"fmt"

	"golang.org/x/sys/windows"
)

// leaderExited blocks until the process pid has exited. os/exec holds a
// handle to it, so the pid is not reused in the meantime.
func leaderExited(pid int) error {
	//nolint:gosec // a Windows pid is a DWORD.
	process, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return fmt.Errorf("open process %d: %w", pid, err)
	}
	defer windows.CloseHandle(process) //nolint:errcheck // a wait handle only.

	if _, err = windows.WaitForSingleObject(process, windows.INFINITE); err != nil {
		return fmt.Errorf("wait for process %d: %w", pid, err)
	}

	return nil
}
