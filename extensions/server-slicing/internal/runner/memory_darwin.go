//go:build darwin

package runner

import (
	"bufio"
	"bytes"
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// psTimeout bounds one reading of the process list.
const psTimeout = 2 * time.Second

// kibibyte is the unit ps reports in.
const kibibyte = 1024

// groupMemory is the resident memory of every process in one group, as ps
// reports it in KiB.
func groupMemory(group int) (int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), psTimeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, "/bin/ps", "-A", "-o", "pgid=,rss=").Output()
	if err != nil {
		return 0, err
	}

	wanted := strconv.Itoa(group)

	var total int64

	lines := bufio.NewScanner(bytes.NewReader(out))
	for lines.Scan() {
		fields := strings.Fields(lines.Text())
		if len(fields) != 2 || fields[0] != wanted {
			continue
		}

		kib, parseErr := strconv.ParseInt(fields[1], 10, 64)
		if parseErr == nil {
			total += kib * kibibyte
		}
	}

	return total, nil
}

// maxRSS is the most memory the slicer itself held, which macOS counts in
// bytes.
func maxRSS(state *os.ProcessState) int64 {
	if state == nil {
		return 0
	}

	usage, ok := state.SysUsage().(*syscall.Rusage)
	if !ok {
		return 0
	}

	return usage.Maxrss
}
