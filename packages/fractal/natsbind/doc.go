// Package natsbind sends and receives Prusa Fractal messages over NATS.
//
// Each node runs an embedded NATS server with JetStream and connects to the
// server of the node above with a leafnode connection. This package moves a
// message over one hop. The engine in sdk/fractal/node forwards it further.
//
// A node has its own NATS account and one account per node below it, so a
// node below cannot read the subjects of another. State messages go over core
// NATS and are dropped while the connection is down. Method invocations and
// results go through JetStream streams and wait there, in order.
package natsbind
