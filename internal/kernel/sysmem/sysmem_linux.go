package sysmem

import (
	"math"

	"golang.org/x/sys/unix"
)

// physical uses sysinfo(2): /proc/meminfo is hidden when procfs is mounted
// with subset=pid.
func physical() (int64, bool) {
	var si unix.Sysinfo_t
	if err := unix.Sysinfo(&si); err != nil {
		return 0, false
	}
	n := uint64(si.Totalram) * uint64(si.Unit) //nolint:unconvert // uint32 on 32-bit targets
	if n > math.MaxInt64 {
		return 0, false
	}
	return int64(n), true
}
