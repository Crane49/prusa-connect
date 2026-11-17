package sandbox

import "syscall"

// dieWithParent has the kernel kill the slicer when the module dies, even by
// SIGKILL. The kernel tracks the thread that started the slicer, not the
// process, so this holds only while no goroutine locks and drops its thread.
func dieWithParent(attr *syscall.SysProcAttr) {
	attr.Pdeathsig = syscall.SIGKILL
}
