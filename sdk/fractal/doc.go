// Package fractal defines the Go types of the Fractal protocol: names, node
// paths, messages, error codes, RPC ids and shadow selection patterns.
//
// Fractal arranges devices as trees of nodes. A node exchanges messages only
// with the one node above it and the nodes below it. Upward it reports state
// with REP and a lost node with LOS. Downward it sets properties with DES,
// calls a method with RPC (answered with RES) and asks for a full report with
// RBR. AREP, ALOS and ARBR are the same in the other direction. The copy a
// node keeps of the state of other nodes is the shadow.
//
// This package does no input or output, starts no goroutine and reads no
// clock. Where it disagrees with the Fractal specification, the specification
// is right.
package fractal
