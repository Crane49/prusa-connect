package wirehost

import (
	"errors"

	"golang.org/x/sys/unix"
)

// leaderExited blocks until the process pid has exited and leaves it
// unreaped, so its pid and process group cannot be reused yet.
func leaderExited(pid int) error {
	for {
		var info unix.Siginfo

		err := unix.Waitid(unix.P_PID, pid, &info, unix.WEXITED|unix.WNOWAIT, nil)
		if !errors.Is(err, unix.EINTR) {
			return err //nolint:wrapcheck // the caller only checks for nil.
		}
	}
}
