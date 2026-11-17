package linkapi

import "fmt"

// MaxCounterRows limits how many rows one Counters block reports, so a module
// cannot send the host a list that grows with the traffic.
const MaxCounterRows = 256

// MaxCounterNameBytes limits the length of a kind, an outcome or a reason. Each
// one is a name from the link protocol's own closed set, not a sentence.
const MaxCounterNameBytes = 64

// MessageCount is how many messages of one kind ended one way.
type MessageCount struct {
	// Kind is the link protocol's name for the type of message, such as REP or
	// RPC. This contract defines no set of kinds.
	Kind string `json:"kind"`
	// Outcome is the link protocol's name for what the link did with the
	// message: it handled it, it passed it on, or it threw it away.
	Outcome string `json:"outcome"`
	// Count is how many messages of that kind ended that way since the link
	// started. It never goes down while the link runs.
	Count uint64 `json:"count"`
}

// DropCount is how many messages of one kind the link threw away for one
// reason.
type DropCount struct {
	// Kind is the link protocol's name for the type of message.
	Kind string `json:"kind"`
	// Reason is the link protocol's name for why the link threw the message
	// away, such as a version it does not read or a queue that was full.
	Reason string `json:"reason"`
	// Count is how many messages of that kind the link threw away for that
	// reason since it started.
	Count uint64 `json:"count"`
}

// Counters is what one link has handled since it started. An operator reads
// them on the links page and over GET /api/v1/links. A pair that has counted
// nothing is left out.
type Counters struct {
	// Messages is how many messages of each kind ended each way.
	Messages []MessageCount `json:"messages,omitempty"`
	// Drops is how many messages of each kind the link threw away for each
	// reason.
	Drops []DropCount `json:"drops,omitempty"`
}

// Validate checks a Counters block against the limits of this contract. The
// error matches ErrInvalidReading.
func (c *Counters) Validate() error {
	if c == nil {
		return nil
	}

	if len(c.Messages) > MaxCounterRows {
		return overBound("counters.messages", len(c.Messages), MaxCounterRows)
	}

	if len(c.Drops) > MaxCounterRows {
		return overBound("counters.drops", len(c.Drops), MaxCounterRows)
	}

	for _, row := range c.Messages {
		if err := counterName("counters.messages", row.Kind, row.Outcome); err != nil {
			return err
		}
	}

	for _, row := range c.Drops {
		if err := counterName("counters.drops", row.Kind, row.Reason); err != nil {
			return err
		}
	}

	return nil
}

// counterName checks the two names of one row. An empty name is refused as well
// as a long one.
func counterName(where, kind, label string) error {
	switch {
	case kind == "" || label == "":
		return fmt.Errorf("%w: %s has a row that names no kind or no outcome", ErrInvalidReading, where)
	case len(kind) > MaxCounterNameBytes:
		return overBound(where+"[].kind", len(kind), MaxCounterNameBytes)
	case len(label) > MaxCounterNameBytes:
		return overBound(where+"[].outcome", len(label), MaxCounterNameBytes)
	}

	return nil
}
