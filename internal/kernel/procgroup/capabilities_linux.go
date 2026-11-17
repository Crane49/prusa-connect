package procgroup

import "golang.org/x/sys/unix"

// KeepPortRightFromChildren clears the ambient capability set, so no process
// this one starts inherits the right a service manager granted the core to
// open a port below 1024. The core keeps the right itself.
// A set that is already empty is left as it is.
func KeepPortRightFromChildren() {
	_ = unix.Prctl(unix.PR_CAP_AMBIENT, unix.PR_CAP_AMBIENT_CLEAR_ALL, 0, 0, 0) // an old kernel has no ambient set
}

// PassPortRightToExec raises that right in the ambient set again, so the run
// this process replaces itself with can open the port too. It fails quietly
// when the service manager never granted the right.
func PassPortRightToExec() {
	_ = unix.Prctl(unix.PR_CAP_AMBIENT, unix.PR_CAP_AMBIENT_RAISE, unix.CAP_NET_BIND_SERVICE, 0, 0) // not granted
}
