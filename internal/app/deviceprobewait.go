package app

import (
	"context"
	"fmt"
	"runtime/debug"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// askProbe calls the driver's Probe and waits for it until ctx ends. It
// returns nil when Probe panics, and the printer then keeps the set its
// admission resolved. It also returns nil when ctx ends first, and the arrival
// then refuses the session.
//
// Probe takes no context, and the contract sets no limit on how long it takes,
// so there is no time limit here either: a slow answer still narrows the set.
// ctx is the arrival's. For a printer the server dials it ends when the dial
// loop stops, so a Probe that never returns does not keep the server from
// stopping. A Probe that never returns leaves the goroutine that calls it
// running, and the session can be closed while Probe still runs.
func (a arrivals) askProbe(
	ctx context.Context, identity connectorapi.Identity, reporter connectorapi.ReportsProbe,
) connectorapi.Set {
	answer := make(chan connectorapi.Set, 1)

	go func() {
		// A driver is code this project did not write. Nothing above this
		// goroutine catches a panic, so without this recover the process would
		// end.
		defer func() {
			if recovered := recover(); recovered != nil {
				a.plane.log.ErrorContext(ctx, "a driver panicked while reporting what its printer has",
					"model", identity.Model, "serial", serialOf(identity),
					"panic", fmt.Sprint(recovered), "stack", string(debug.Stack()))

				answer <- nil
			}
		}()

		answer <- reporter.Probe()
	}()

	select {
	case probe := <-answer:
		return probe
	case <-ctx.Done():
		return nil
	}
}
