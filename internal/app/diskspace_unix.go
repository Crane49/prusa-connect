//go:build !windows

package app

import "syscall"

func availableDisk(path string) (int64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, err
	}

	// The kernel returns unsigned counts whose product fits the signed byte
	// range on every supported filesystem. The types differ between Unix
	// targets.
	available := int64(stat.Bavail) * int64(stat.Bsize) //nolint:gosec,unconvert,nolintlint // Unix types vary.

	return available, nil
}
