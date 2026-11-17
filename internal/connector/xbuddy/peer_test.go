package xbuddy

import (
	"net/netip"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// The host matches a connected printer against the printers it hears on the
// local network by this address, so it is the socket's far end while the
// printer is there and nothing once it has gone.
func TestThePeerIsTheSocketsFarEndWhileThePrinterIsThere(t *testing.T) {
	t.Parallel()

	away := newSession(settings{
		identity: connectorapi.Identity{Vendor: Vendor, Model: "MK4", Serial: "SN-0"},
		caps:     baseline(),
		storage:  defaultStorage,
		live:     quick(),
	})
	t.Cleanup(func() { _ = away.Close() })

	if peer := away.Peer(); peer.IsValid() {
		t.Fatalf("a session with no connection yet reported the peer %v", peer)
	}

	live := quick()
	stand := attached(t, live, time.Minute)

	if want, got := netip.MustParseAddr("127.0.0.1"), stand.session.Peer(); got != want {
		t.Fatalf("a connected printer reported the peer %v, expected %v", got, want)
	}

	_ = stand.client.Close()

	select {
	case <-stand.session.PresenceChanged():
	case <-time.After(20 * live.ReadDeadline):
		t.Fatal("the printer hung up and nothing woke a reader of presence")
	}

	if peer := stand.session.Peer(); peer.IsValid() {
		t.Fatalf("a printer that hung up still reported the peer %v", peer)
	}
}
