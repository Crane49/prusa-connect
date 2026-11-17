//go:build !linux && !windows && !darwin && !freebsd && !netbsd && !openbsd

package wirehost

import "errors"

// leaderExited cannot watch for an exit here, so a module's other processes
// are killed only by Kill and Close's timeout.
func leaderExited(int) error {
	return errors.ErrUnsupported
}
