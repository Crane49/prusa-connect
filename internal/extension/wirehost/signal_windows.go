package wirehost

import "syscall"

// signalName names sig. A Windows process never ends by a signal, so nothing
// reaches it in practice.
func signalName(sig syscall.Signal) string {
	return sig.String()
}
