// Package shadow implements the shadow of one Fractal node: the copy a node
// keeps of the property values of interfaces on other nodes.
//
// A Store is that copy. A Selection says which interfaces it keeps. For each
// one it has a Key, a Status (EMPTY, OK or STALE) and the last reported
// property values.
//
// For the nodes below, pass each REP to ApplyREP and each LOS to ApplyLOS,
// and call MarkStale when the link to a node directly below is lost. The
// caller decides from the Result whether to forward a message.
//
// For the nodes above, keep a second Store. Call Rebase each time the node
// takes a Node Path and MarkStaleAbove when a link above is lost.
package shadow
