package main

import (
	"syscall"
	"time"
)

// processCPU is the CPU time the operating system gave this process, kernel
// and user together. The runtime's /cpu/classes metrics overstate it on a busy
// machine: they count a thread that is runnable but descheduled as busy.
func processCPU() (time.Duration, bool) {
	handle, err := syscall.GetCurrentProcess()
	if err != nil {
		return 0, false
	}

	var creation, exit, kernel, user syscall.Filetime
	if err = syscall.GetProcessTimes(handle, &creation, &exit, &kernel, &user); err != nil {
		return 0, false
	}

	return time.Duration(kernel.Nanoseconds() + user.Nanoseconds()), true
}
