//go:build !linux

package procgroup

import (
	"context"
	"fmt"
	"os/exec"
)

// Reaper does nothing outside Linux, which alone lets a process adopt
// orphans.
type Reaper struct{}

// NewReaper returns an inert Reaper.
func NewReaper() *Reaper { return &Reaper{} }

// Run returns when ctx is done.
func (r *Reaper) Run(ctx context.Context) { <-ctx.Done() }

// Start starts cmd. release does nothing.
func (r *Reaper) Start(cmd *exec.Cmd) (release func(), err error) {
	if err = cmd.Start(); err != nil {
		return nil, fmt.Errorf("procgroup: start: %w", err)
	}

	return func() {}, nil
}
