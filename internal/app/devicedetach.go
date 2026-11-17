package app

import (
	"context"
	"fmt"
	"runtime/debug"
	"time"
)

// relayDeadline is when a relay that nobody stops is abandoned.
//
// It is the caller's own deadline where there is one. Over HTTP that is the
// route's stream budget, core.api.stream_timeout. A caller with no deadline
// gets detachedPrintBudget. Only the deadline is kept: a caller that is cancelled
// does not end the relay.
func relayDeadline(ctx context.Context, now time.Time) time.Time {
	if deadline, bounded := ctx.Deadline(); bounded {
		return deadline
	}

	return now.Add(detachedPrintBudget)
}

// detachedAnswer is what the work outliveCaller started ended with.
type detachedAnswer[T any] struct {
	value T
	err   error
}

// outliveCaller runs work that moves a file to a printer on a goroutine of its
// own, and waits for it for as long as the caller waits.
//
// A caller can go away at any time, such as a page that is reloaded or closed.
// A relay on the caller's context would then end in the middle, and the printer
// would throw away a file nobody asked it to drop. So the work runs under a
// context the caller cannot cancel, bounded by relayDeadline, and a caller
// that leaves is answered with its own context's error while the file goes on
// moving. The stop route still ends a transfer: it cancels the context the
// transfer record holds, which is below this one.
//
// The caller's request ends when the caller leaves, so the HTTP server can
// drain on a shutdown. The relay itself ends when the plane closes the
// printer's session.
//
// A driver is code this project did not write, and a panic in it comes up
// through the relay to the top of this goroutine. Nothing above a goroutine
// catches one, so it is written down here and goes no further, and a caller
// still waiting is told the driver stopped. The record of the transfer is
// already closed by the relay.
func outliveCaller[T any](
	ctx context.Context, d *devices, id, path string, work func(context.Context) (T, error),
) (T, error) {
	relaying, stop := context.WithDeadline(context.WithoutCancel(ctx), relayDeadline(ctx, time.Now()))

	// Buffered, so the goroutine never blocks on a caller that has gone.
	answer := make(chan detachedAnswer[T], 1)

	go func() {
		defer stop()

		defer func() {
			recovered := recover()
			if recovered == nil {
				return
			}

			answer <- detachedAnswer[T]{err: driverStopped(path)}

			d.log.ErrorContext(relaying, "a driver stopped while relaying a file",
				"device_id", id, "path", path, "panic", fmt.Sprint(recovered),
				"stack", string(debug.Stack()))
		}()

		value, err := work(relaying)
		answer <- detachedAnswer[T]{value: value, err: err}
	}()

	select {
	case got := <-answer:
		return got.value, got.err
	case <-ctx.Done():
		var zero T

		return zero, ctx.Err()
	}
}
