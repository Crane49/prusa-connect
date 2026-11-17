package connectorapi

import (
	"context"
	"errors"
	"net/http"
)

// The errors a direction reports. Callers inspect them with errors.Is.
var (
	// ErrNoDirection reports a driver that implements neither Outbound nor
	// Inbound.
	ErrNoDirection = errors.New("connectorapi: driver is neither outbound nor inbound")

	// ErrDeviceRefused reports a device the host will not take: one it does not
	// know, one an operator has not approved, or one past a limit it enforces.
	// The driver must hang up on it.
	ErrDeviceRefused = errors.New("connectorapi: the host refused the device")

	// ErrSessionNotTaken reports a session the host did not accept after it
	// admitted the device. The driver must close the session it built.
	ErrSessionNotTaken = errors.New("connectorapi: the host did not take the session")
)

// Outbound is a driver for devices the host dials: a serial port, or a printer
// listening on a socket that configuration names.
type Outbound interface {
	Driver

	// Connect performs the identity exchange and the live probe and returns a
	// session. profile is the set the host resolved from its data files. The
	// driver may narrow it and must not widen it.
	Connect(ctx context.Context, endpoint Endpoint, profile Set) (Session, error)
}

// Inbound is a driver for devices that dial the host and identify themselves
// in the handshake. The host validates commands and supervises the session the
// same way as for Outbound.
type Inbound interface {
	Driver

	// Handler returns the routes a device dials. It is called once. The host
	// mounts the routes under a prefix and strips that prefix from each
	// request, so the driver routes from its own root. The driver reaches the
	// host only through arrivals.
	Handler(arrivals Arrivals) (http.Handler, error)
}

// Arrivals is how an inbound driver hands a device to the host. Admit gives
// the driver the profile that Connect receives as an argument, and Arrived
// takes the session that Connect returns. Every method is safe for concurrent
// use.
type Arrivals interface {
	// Admit asks whether the host will take a device that has identified
	// itself, and returns the profile the host resolved for it. The driver may
	// narrow the profile and must not widen it. A refusal is ErrDeviceRefused,
	// and the driver must not serve that device.
	Admit(ctx context.Context, identity Identity) (profile Set, err error)

	// Arrived hands the host the session for a device Admit accepted. The host
	// owns it from that call and releases it when the session's state channel
	// closes. On any error the driver must close the session. ctx bounds the
	// handover, not the session, so a request context is fine.
	Arrived(ctx context.Context, session Session) error
}

// Directions reports which of Inbound and Outbound a driver implements. Either
// return may be nil. A driver that implements neither is ErrNoDirection.
func Directions(d Driver) (inbound Inbound, outbound Outbound, err error) {
	inbound, _ = d.(Inbound)
	outbound, _ = d.(Outbound)

	if inbound == nil && outbound == nil {
		return nil, nil, ErrNoDirection
	}

	return inbound, outbound, nil
}
