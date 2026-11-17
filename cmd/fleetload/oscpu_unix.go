//go:build !windows

package main

import (
	"syscall"
	"time"
)

// processCPU is the CPU time the operating system gave this process, kernel
// and user together. The runtime's /cpu/classes metrics overstate it on a busy
// machine: they count a thread that is runnable but descheduled as busy.
func processCPU() (time.Duration, bool) {
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		return 0, false
	}

	return time.Duration(usage.Utime.Nano() + usage.Stime.Nano()), true
}
