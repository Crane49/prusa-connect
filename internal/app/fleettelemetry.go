package app

import (
	"context"
	"fmt"

	"github.com/prusa3d/prusa-connect/internal/authz"
)

// fleetTelemetry decides who may read an answer covering the whole fleet. It is
// the permission /metrics takes, and the transport names the port it satisfies.
//
// It is a type of its own because the transport asks about a principal string
// and the graph answers about a parsed subject. The composition root does that
// parse, as it does for every other surface here.
type fleetTelemetry struct {
	graph *authz.Store
}

// CanReadFleetTelemetry reports whether this principal reaches telemetry on
// every device this deployment knows about.
//
// A principal that will not parse is refused, with an error. It cannot arise
// from a request that reached a handler, because the middleware writes the
// value. A build that changes that must refuse the scrape.
func (f fleetTelemetry) CanReadFleetTelemetry(ctx context.Context, principal string) (bool, error) {
	who, err := authz.ParseSubject(principal)
	if err != nil {
		return false, fmt.Errorf("read the caller's principal: %w", err)
	}

	return f.graph.CanReadFleetTelemetry(ctx, who)
}
