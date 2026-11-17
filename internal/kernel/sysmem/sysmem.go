// Package sysmem reports how much memory the process may use: the machine's
// physical memory, capped by the container's cgroup limit where one applies.
package sysmem

import "os"

// Total returns the smaller of the physical memory and the container limit.
// known is false when the physical size cannot be read.
func Total() (bytes int64, known bool) {
	bytes, known = physical()
	if !known {
		return 0, false
	}
	if self, err := os.ReadFile("/proc/self/cgroup"); err == nil {
		if limit, ok := cgroupLimit(string(self), os.ReadFile); ok && limit < bytes {
			bytes = limit
		}
	}
	return bytes, true
}
