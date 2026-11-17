//go:build !linux && !darwin && !windows

package sysmem

func physical() (int64, bool) { return 0, false }
