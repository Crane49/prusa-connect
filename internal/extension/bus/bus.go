// Package bus lets a supervised extension publish an event on this server's own
// event stream.
//
// Both kinds of extension publish through it: a script instance and a module
// instance running as a child process. An extension package may not import a
// transport, so the composition root binds one emitter per instance and the
// extension never sees what is behind it.
package bus

// Emission is one event an extension asks the host to publish on the local bus.
//
// There is no origin field. An emitter is bound to one instance, so the host
// already knows who sent the emission.
type Emission struct {
	// Type is the event type the operator's declaration produced for this
	// destination.
	Type string
	// Key is the routing key the extension computed. It becomes the
	// publication's conflation key.
	Key string
	// Body is the payload the extension encoded. It must be JSON, because the
	// bus carries it as a CloudEvents data member.
	Body string
}

// Emitter publishes one emission on this server's own event stream.
//
// An implementation must not block. It returns an error instead. A caller that
// waited would stall the instance it publishes for.
type Emitter interface {
	Emit(Emission) error
}
