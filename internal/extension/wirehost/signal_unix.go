//go:build !windows

package wirehost

import (
	"syscall"

	"golang.org/x/sys/unix"
)

// signalName is the conventional name of sig, such as "SIGKILL".
func signalName(sig syscall.Signal) string {
	return unix.SignalName(sig)
}
