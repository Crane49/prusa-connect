package main

import (
	"sync/atomic"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	fractalnode "github.com/prusa3d/prusa-connect/sdk/fractal/node"
	"github.com/prusa3d/prusa-connect/sdk/linkapi"
)

// counters counts the messages the Fractal node handled and the messages it
// dropped.
//
// The host has no way for a module to add a series to its /metrics page. Every
// series there is written by the host itself, from a table compiled into it, so
// a module cannot invent a name or a label. The counts go into the reading this
// module reports instead, and the links page and GET /api/v1/links show them.
//
// One counter per pair, in a slice built once at start, read and written with
// atomic adds. The engine calls Message and Drop while it handles a message and
// from several goroutines, so a map behind a mutex would put every message of
// this node through one lock.
type counters struct {
	kinds    []fractal.Kind
	outcomes []fractalnode.Outcome
	reasons  []fractalnode.DropReason

	// messages is one counter per kind and outcome, and drops one per kind and
	// reason. Both are flat: the row is the kind and the column is the outcome
	// or the reason.
	messages []atomic.Uint64
	drops    []atomic.Uint64
}

var _ fractalnode.Metrics = (*counters)(nil)

// newCounters builds the counters for every kind, outcome and reason the SDK
// defines. Nothing is added later, so no call after this allocates.
func newCounters() *counters {
	kinds := fractal.Kinds()
	outcomes := fractalnode.Outcomes()
	reasons := fractalnode.DropReasons()

	return &counters{
		kinds:    kinds,
		outcomes: outcomes,
		reasons:  reasons,
		messages: make([]atomic.Uint64, len(kinds)*len(outcomes)),
		drops:    make([]atomic.Uint64, len(kinds)*len(reasons)),
	}
}

// Message counts one message of kind k that the engine processed, forwarded or
// dropped.
func (c *counters) Message(k fractal.Kind, o fractalnode.Outcome) {
	row, ok := indexOf(len(c.kinds), int(k))
	if !ok {
		return
	}

	column, ok := indexOf(len(c.outcomes), int(o))
	if !ok {
		return
	}

	c.messages[row*len(c.outcomes)+column].Add(1)
}

// Drop counts one message of kind k that the engine dropped for reason r.
func (c *counters) Drop(k fractal.Kind, r fractalnode.DropReason) {
	row, ok := indexOf(len(c.kinds), int(k))
	if !ok {
		return
	}

	column, ok := indexOf(len(c.reasons), int(r))
	if !ok {
		return
	}

	c.drops[row*len(c.reasons)+column].Add(1)
}

// indexOf turns a value of one of the SDK's small sets into a row or a column.
// Each set numbers from one, so the value minus one is the index. A value
// outside the set is counted nowhere: a build of this module against a later
// SDK must not write past the end of a slice.
func indexOf(size, value int) (index int, ok bool) {
	if value < 1 || value > size {
		return 0, false
	}

	return value - 1, true
}

// reading returns the counts as the contract reports them. A pair that has
// counted nothing is left out, so a node that has handled one kind of message
// reports one row and not the whole table.
func (c *counters) reading() *linkapi.Counters {
	out := &linkapi.Counters{}

	for row, kind := range c.kinds {
		for column, outcome := range c.outcomes {
			if count := c.messages[row*len(c.outcomes)+column].Load(); count > 0 {
				out.Messages = append(out.Messages, linkapi.MessageCount{
					Kind:    kind.String(),
					Outcome: outcome.String(),
					Count:   count,
				})
			}
		}

		for column, reason := range c.reasons {
			if count := c.drops[row*len(c.reasons)+column].Load(); count > 0 {
				out.Drops = append(out.Drops, linkapi.DropCount{
					Kind:   kind.String(),
					Reason: reason.String(),
					Count:  count,
				})
			}
		}
	}

	if len(out.Messages) == 0 && len(out.Drops) == 0 {
		return nil
	}

	return out
}
