// Package fractaltest has an in-memory transport for the Prusa Fractal protocol
// and the conformance suite that every transport must pass.
//
// # The in-memory transport
//
// A Fabric connects several nodes in one process into a tree. Its links follow
// the delivery rules that sdk/fractal/node states for a transport:
//
//   - messages on one link arrive in the order they were sent
//   - a best-effort message sent while the link is down is dropped
//   - a durable message sent while the link is down waits, and arrives in
//     order when the link comes back
//   - a durable message that the receiver did not acknowledge is delivered
//     again
//   - a durable message with an id the link already saw inside the
//     deduplication window is delivered once
//
// A test can cut a link, bring it back, leave a request unacknowledged after
// the method ran, and move the clock. It needs no network and no sleep.
//
// # The conformance suite
//
// RunBindingSuite tests a transport against those delivery rules. It uses only
// the node.Binding interface, so a transport over NATS runs the same cases as
// the in-memory transport of this package. The engine in sdk/fractal/node
// relies on these rules, so a transport that passes the suite works with it.
//
// # The conformance table
//
// The tests of this package have a table with one row for each MUST sentence
// of fractal.md. A row names the test that proves the sentence, in this
// package or in another one. A test of the table fails when a named test does
// not exist.
package fractaltest
