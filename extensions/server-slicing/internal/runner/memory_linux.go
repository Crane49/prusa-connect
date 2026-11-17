//go:build linux

package runner

import (
	"bytes"
	"os"
	"strconv"
	"strings"
	"syscall"
)

// kibibyte is the unit Linux counts the peak memory in.
const kibibyte = 1024

// groupMemory is the resident memory of every process in one group, read
// from /proc.
func groupMemory(group int) (int64, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0, err
	}

	page := int64(os.Getpagesize())
	wanted := strconv.Itoa(group)

	var total int64

	for _, entry := range entries {
		if entry.Name()[0] < '0' || entry.Name()[0] > '9' {
			continue
		}

		stat, readErr := os.ReadFile("/proc/" + entry.Name() + "/stat")
		if readErr != nil {
			continue
		}

		// The name in brackets may hold spaces, so the fields are counted
		// from the last closing bracket: state, ppid, pgrp, and rss is the
		// 22nd field after it.
		closing := bytes.LastIndexByte(stat, ')')
		if closing < 0 {
			continue
		}

		fields := strings.Fields(string(stat[closing+1:]))
		if len(fields) < 22 || fields[2] != wanted {
			continue
		}

		pages, parseErr := strconv.ParseInt(fields[21], 10, 64)
		if parseErr == nil {
			total += pages * page
		}
	}

	return total, nil
}

// maxRSS is the most memory the slicer itself held, which Linux counts in
// KiB.
func maxRSS(state *os.ProcessState) int64 {
	if state == nil {
		return 0
	}

	usage, ok := state.SysUsage().(*syscall.Rusage)
	if !ok {
		return 0
	}

	return usage.Maxrss * kibibyte
}
