//go:build !linux && !darwin && !windows

package procgroup

// Memory returns ErrUnknown: this platform has no reader.
func (g *Group) Memory() (int64, error) { return 0, ErrUnknown }

// descendants finds nothing: this platform has no reader, so Kill reaches the
// group alone.
func (g *Group) descendants() ([]procStat, error) { return nil, nil }

// procStat is one process, as the walk on other platforms reports it.
type procStat struct{ pid, pgrp int }
