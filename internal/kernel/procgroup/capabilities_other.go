//go:build !linux

package procgroup

// KeepPortRightFromChildren does nothing outside Linux, which alone has an
// ambient capability set.
func KeepPortRightFromChildren() {}

// PassPortRightToExec does nothing outside Linux.
func PassPortRightToExec() {}
