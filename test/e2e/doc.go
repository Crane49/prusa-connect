// Package e2e drives the built binary with an operator's configuration file
// over real sockets.
//
// It is the lane test/fleet cannot be. The matrix runs in one process over
// net.Pipe so a six-week partition costs microseconds, and that is only
// possible because testing/synctest excludes network I/O from durable blocking.
// Everything the matrix therefore does not exercise is
// here: the composition root's startup order, the configuration file, the
// listener, the HTTP surface as a client reaches it, and the event stream as a
// browser reads it.
//
// # Blocked scenarios
//
// Some scenarios need wiring that does not exist yet: nothing in
// internal/app mounts the inbound connector, and no route publishes a printer.
// They are written rather than stubbed, and each names a probe that says
// whether its wiring has arrived. blocked.json lists them, and
// TestNothingIsStillBlockedThatHasBeenWired fails the moment a probe starts
// passing while its scenarios are still listed. A suite that quietly skips is
// how a lane goes green for a year without running.
package e2e
