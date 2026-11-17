package sysmem

import (
	"math"

	"golang.org/x/sys/unix"
)

func physical() (int64, bool) {
	n, err := unix.SysctlUint64("hw.memsize")
	if err != nil || n > math.MaxInt64 {
		return 0, false
	}
	return int64(n), true
}
