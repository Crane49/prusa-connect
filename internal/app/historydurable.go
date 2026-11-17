package app

import (
	"context"
	"slices"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/event"
	"github.com/prusa3d/prusa-connect/sdk/historyapi"
)

// The frames this server writes to the store as it publishes them, without
// waiting for the feed's next flush.
//
// The feed is a buffered reader of the bus. It holds a batch until it has a
// thousand rows or two seconds have passed, because one transaction of a
// thousand rows runs three times faster than a thousand transactions of one.
// That suits telemetry, where losing the last two seconds costs two seconds of
// a temperature curve.
//
// It does not suit the record of an instruction. A command posted just before
// the process is killed would leave no row, so the restart sweep would have
// nothing to close. A graceful stop drains the last window, but a SIGKILL, a
// power cut and a panic drain nothing.
//
// So a command's and a transfer's own frames are written through to the store
// on the goroutine that publishes them. The feed still carries them too. A
// fact is idempotent on (ref, epoch, sequence), so the same frame arriving
// again through the feed keeps one row, and the feed covers a write-through
// that fails.
//
// It does not widen what a provider keeps. The origin has to be one the
// provider's own subscription may read, under the grant an operator wrote for
// it. A provider granted nothing still stores nothing. The audit class is left
// to the feed, because audit sight is a second grant per origin and only the
// subscription holds it.

// writeThroughBudget bounds one write-through. It runs on a caller's own
// goroutine, so a store that has stopped answering must cost that caller a
// bounded wait and not the whole request. The frame is on the bus either way
// and the feed still stores it.
const writeThroughBudget = 2 * time.Second

// durableWriter is what the device plane calls to put one frame in the store as
// it publishes it. A build with no history instance has none, and the zero
// value writes nothing.
type durableWriter func(ctx context.Context, fact historyapi.Fact)

// write is the nil-safe call site, so the device plane holds no branch.
func (w durableWriter) write(ctx context.Context, fact historyapi.Fact) {
	if w == nil {
		return
	}

	w(ctx, fact)
}

// durableWrites is how the composition root hands the device plane the write
// path. It resolves the serving providers per call, because the set of serving
// instances changes while the process runs.
func (p *plane) durableWrites() durableWriter {
	return p.past.writeThrough
}

// writeThrough stores one frame in every serving provider that would have
// caught it off the bus.
//
// The filter is the feed's own: the class the registration declared it
// consumes, the capability the adapter published, and the origins the
// provider's subscription holds. A frame that fails any of them is not written
// to that provider.
func (f *historyFeed) writeThrough(ctx context.Context, fact historyapi.Fact) {
	if fact.Type == "" || historyapi.VerbOf(fact.Class) != historyapi.VerbFact {
		return
	}

	// The audit class is left to the feed. Audit sight is a grant per origin
	// beside the read grant, and only the subscription holds the answer.
	if fact.Class == event.ClassAudit {
		return
	}

	f.mu.RLock()

	targets := make([]*historyStream, 0, len(f.serving))

	for _, feeding := range f.serving {
		if !feeding.wants(fact.Class) || !feeding.caps.Has(historyapi.CapFacts) {
			continue
		}

		if !feeding.sees(fact.Ref) {
			continue
		}

		targets = append(targets, feeding)
	}

	f.mu.RUnlock()

	if len(targets) == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), writeThroughBudget)
	defer cancel()

	for _, feeding := range targets {
		if err := feeding.provider.AppendFacts(ctx, []historyapi.Fact{fact}); err != nil {
			// Debug and not warn. The feed still carries the same frame, so the
			// row arrives late and is not lost. At warn, a store that is
			// restarting would print a line per command.
			f.log.DebugContext(ctx, "a frame was not written through to a history provider",
				"resource", feeding.provider.Ref().String(),
				"type", fact.Type, "error", err.Error())

			continue
		}

		feeding.counts.through.Add(1)
	}
}

// sees reports whether this provider's subscription holds the origin, which is
// the grant an operator wrote for it as the hub last resolved it.
func (s *historyStream) sees(origin event.Origin) bool {
	return slices.Contains(s.sub.Origins(), origin)
}

// factNow renders a frame this process has just published as the row that
// stores it.
//
// The sequence is the envelope's and never the position's offset. They are two
// different numbers over the same origin; see Hub.PublishStored. A history row
// is keyed on (ref, epoch, sequence), so a row written under the offset would
// be a second row for a frame the store already holds.
func factNow(origin event.Origin, env event.Envelope) historyapi.Fact {
	sequence, err := event.ParseSequence(env.Sequence)
	if err != nil {
		return historyapi.Fact{}
	}

	return historyapi.Fact{
		At:       env.Time,
		Ref:      origin,
		Type:     env.Type,
		Sequence: sequence,
		Epoch:    env.Epoch,
		Payload:  env.Data,
		Class:    env.Class,
	}
}
