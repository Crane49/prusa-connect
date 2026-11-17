// Package procgroup starts a command as a group that can be signalled, killed
// and measured as one: a process group on unix, a job object on Windows. It
// also carries a Reaper, which collects the zombies of orphans this process
// adopts as a Linux subreaper.
package procgroup

import "errors"

// ErrUnknown is returned by Memory where the platform gives no way to read it.
var ErrUnknown = errors.New("procgroup: memory cannot be read here")
