package app

import (
	"context"
	"log/slog"
	"time"

	"github.com/prusa3d/prusa-connect/internal/transport/stream"
	"github.com/prusa3d/prusa-connect/sdk/event"
	"github.com/prusa3d/prusa-connect/sdk/historyapi"
)

// What a restart does about the commands the last process was holding.
//
// A command in flight when the server stops leaves a row with requested and
// authorized and no settled. The driver session it waited on ended with the
// process, so no other code closes that row.
//
// Nothing is re-dispatched. Only the device knows whether it executed the
// instruction, and the reply went to a socket that is gone. Sending the
// instruction again would be a guess about the state of a machine that may be
// working. So the sweep writes what it knows: the invocation ended, this
// process did not see how, and the reason is that the server restarted.
//
// The frame is published and also stored. Publishing tells a subscriber.
// Storing it directly closes the row even when this process's history
// subscription is not yet authorized to see that origin. Both carry the same
// position, and a fact is idempotent on (ref, epoch, sequence), so the store
// keeps one row however many times it arrives.

// sweepBudget bounds the whole sweep. It runs once per process, off the path
// anything is waiting on, and a store that will not answer must not hold the
// history feed's first flush behind it.
const sweepBudget = 30 * time.Second

// sweepPage is how many open invocations one query asks for. It bounds the
// memory one answer takes. A deployment with more open commands has the rest
// closed by the next start.
const sweepPage = 1000

// closeOpenCommands terminates every invocation the store holds open.
//
// It is called once, by the history feed, as soon as a provider that folds
// commands is serving. No earlier moment can ask the question. A reader that
// connects before the sweep has run sees rows as open that are not.
func closeOpenCommands(
	ctx context.Context, hub *stream.Hub, provider historyapi.Provider, log *slog.Logger,
) {
	if !provider.Caps().Has(historyapi.CapCommands) {
		// A provider that keeps the facts and no fold cannot say which
		// invocations are open. Reading every stored command frame to find
		// out is too slow for start-up. Its record of an interrupted command
		// stays open, and a deployment accepts that when it configures a
		// store that does not fold.
		return
	}

	ctx, cancel := context.WithTimeout(ctx, sweepBudget)
	defer cancel()

	open, err := provider.QueryCommands(ctx, historyapi.CommandQuery{
		OpenOnly: true, Limit: sweepPage,
	})
	if err != nil {
		log.WarnContext(ctx, "the open commands of the last run could not be read, "+
			"so their records stay open", "error", err.Error())

		return
	}

	if len(open.Commands) == 0 {
		return
	}

	closing := make([]historyapi.Fact, 0, len(open.Commands))

	for _, one := range open.Commands {
		fact, ok := restartedFact(hub, one, log)
		if !ok {
			continue
		}

		closing = append(closing, fact)
	}

	if err = provider.AppendFacts(ctx, closing); err != nil {
		log.WarnContext(ctx, "the terminal frames for the last run's open commands "+
			"were published and not stored", "commands", len(closing), "error", err.Error())

		return
	}

	log.InfoContext(ctx, "the commands the last run left open were closed as unknown",
		"commands", len(closing), "status", event.CommandServerRestarted)

	if open.Cursor != "" {
		// One page is the bound, and a deployment past it gets the rest on the
		// next start. A loop here would make a start-up that writes without a
		// bound.
		log.WarnContext(ctx, "more commands were left open than one sweep closes; "+
			"the rest are closed by the next start", "closed", len(closing))
	}
}

// restartedFact publishes one invocation's terminal frame and renders it as the
// row that closes the fold.
//
// The payload is the ordinary settled frame, so nothing downstream needs a case
// for it: the status is a word in sdk/event's closed set, the outcome is marked
// unknown, and a fold that has never heard of a restart still writes the row.
func restartedFact(
	hub *stream.Hub, one historyapi.Command, log *slog.Logger,
) (historyapi.Fact, bool) {
	data := settledFrame(one.ID, one.Verb, event.CommandServerRestarted, true,
		false, "", nil, nil, "", 0)
	if len(data) == 0 {
		return historyapi.Fact{}, false
	}

	env, _, err := hub.PublishStored(stream.Publication{
		Origin:      one.Ref,
		Type:        event.TypeDeviceCommandSettled,
		Class:       event.ClassState,
		Data:        data,
		CausationID: one.ID,
	})
	if err != nil {
		log.Warn("the terminal frame of a command the last run left open reached no hub",
			"command_id", one.ID, "error", err.Error())

		return historyapi.Fact{}, false
	}

	// The fact takes the envelope's own sequence, not the position's offset.
	// They count different things over one origin (see Hub.PublishStored).
	// The store keys a fact on (ref, epoch, sequence), so a row written under
	// the offset would be a second copy of the frame the feed stores.
	fact := factNow(one.Ref, env)
	if fact.Type == "" {
		return historyapi.Fact{}, false
	}

	return fact, true
}
