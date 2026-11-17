//go:build !linux && !darwin

package runner

import (
	"errors"
	"os"
)

// groupMemory is not read on this host, so a memory bound is not applied.
func groupMemory(int) (int64, error) {
	return 0, errors.New("the memory of a process group is not read on this host")
}

func maxRSS(*os.ProcessState) int64 { return 0 }
