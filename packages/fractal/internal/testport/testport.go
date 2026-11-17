// Package testport hands out TCP ports to the tests of this module.
package testport

import (
	"fmt"
	"math/rand/v2"
	"net"
	"sync"
	"testing"
)

// handedOut holds every port Free has returned in this process, so two tests
// running in parallel are never given the same one.
//
//nolint:gochecknoglobals // one set for the whole test binary is the point.
var handedOut = struct {
	mu    sync.Mutex
	ports map[int]bool
}{ports: map[int]bool{}}

// The ports Free hands out lie below the range the system takes a port from
// for a listener on port 0 or an outgoing connection: 32768 and up on Linux,
// 49152 and up elsewhere. A port from that range is free again once Free
// closes its probe, and a relay or another test binary could take it before
// the test's server binds it. That server then never becomes ready.
const (
	first = 20000
	count = 12000
)

// Free returns a TCP port that no process listens on and that no other test
// in this process was given.
func Free(t testing.TB) int {
	t.Helper()
	handedOut.mu.Lock()
	defer handedOut.mu.Unlock()
	start := rand.IntN(count) //nolint:gosec // spreads the ports of parallel test binaries, not a secret.
	for i := range count {
		port := first + (start+i)%count
		if handedOut.ports[port] {
			continue
		}
		probe, err := new(net.ListenConfig).Listen(t.Context(), "tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			continue
		}
		if closeErr := probe.Close(); closeErr != nil {
			t.Fatalf("close the probe on port %d: %v", port, closeErr)
		}
		handedOut.ports[port] = true
		return port
	}
	t.Fatalf("no free port from %d to %d", first, first+count-1)
	return 0
}
