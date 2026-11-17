//go:build unix && !linux

package sandbox

import "syscall"

// dieWithParent does nothing: the module asks the kernel for it on Linux only.
func dieWithParent(*syscall.SysProcAttr) {}
