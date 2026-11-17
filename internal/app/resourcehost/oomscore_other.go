//go:build !linux

package resourcehost

// raiseOOMScore does nothing where the kernel has no oom_score_adj.
func raiseOOMScore(int) error { return nil }
