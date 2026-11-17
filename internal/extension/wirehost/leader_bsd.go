//go:build darwin || freebsd || netbsd || openbsd

package wirehost

import (
	"errors"

	"golang.org/x/sys/unix"
)

// leaderExited blocks until the process pid has exited and leaves it
// unreaped, so its pid and process group cannot be reused yet.
func leaderExited(pid int) error {
	queue, err := unix.Kqueue()
	if err != nil {
		return err //nolint:wrapcheck // the caller only checks for nil.
	}
	defer unix.Close(queue)

	var watch unix.Kevent_t
	unix.SetKevent(&watch, pid, unix.EVFILT_PROC, unix.EV_ADD|unix.EV_ONESHOT)
	watch.Fflags = unix.NOTE_EXIT

	fired := make([]unix.Kevent_t, 1)

	for {
		_, err = unix.Kevent(queue, []unix.Kevent_t{watch}, fired, nil)

		switch {
		// A process that has already exited cannot be watched.
		case errors.Is(err, unix.ESRCH):
			return nil
		case errors.Is(err, unix.EINTR):
			continue
		case err != nil:
			return err //nolint:wrapcheck // the caller only checks for nil.
		case fired[0].Flags&unix.EV_ERROR == 0:
			return nil
		}

		// An error on the event is an errno.
		if errno := unix.Errno(fired[0].Data); errno != unix.ESRCH { //nolint:gosec // an errno fits.
			return errno
		}

		return nil
	}
}
