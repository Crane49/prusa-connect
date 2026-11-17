package natsbind

import (
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
)

// TestJoinURLKeepsASchemeTheOperatorWrote checks the address this node dials
// when it asks the node above to join. An operator writes that address either
// as a host and a port or with the scheme in front, and both have to reach the
// same server. Prefixing an address that already names a scheme produces
// "nats://nats://hq:4222", which the NATS client reads as the host "nats".
func TestJoinURLKeepsASchemeTheOperatorWrote(t *testing.T) {
	t.Parallel()

	for _, row := range []struct {
		name   string
		addr   string
		secure bool
		want   string
	}{
		{name: "a host and a port", addr: "hq:4222", want: "nats://hq:4222"},
		{name: "a host and a port with tls", addr: "hq:4222", secure: true, want: "tls://hq:4222"},
		{name: "the scheme already written", addr: "nats://hq:4222", want: "nats://hq:4222"},
		{name: "a tls scheme already written", addr: "tls://hq:4222", secure: true, want: "tls://hq:4222"},
		{name: "a tls scheme without tls material", addr: "tls://hq:4222", want: "tls://hq:4222"},
		{name: "an ip address and a port", addr: "10.0.0.2:4222", want: "nats://10.0.0.2:4222"},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()

			if got := joinURL(row.addr, row.secure); got != row.want {
				t.Fatalf("joinURL(%q, %v) = %q, want %q", row.addr, row.secure, got, row.want)
			}
		})
	}
}

// TestTheNewestReasonIsTheOneAnOperatorReads: a node denied once, and then
// unable to reach the node above at all, shows the second reason. The first
// one would send an operator looking for the wrong thing.
func TestTheNewestReasonIsTheOneAnOperatorReads(t *testing.T) {
	t.Parallel()

	b := &Binding{
		cfg:    Config{NodeID: "workshop", ParentAddr: "brno:4222", Logger: slog.New(slog.DiscardHandler)},
		log:    slog.New(slog.DiscardHandler),
		uplink: Uplink{State: UplinkJoining, Address: "brno:4222"},
	}

	b.denyState(fractal.CodeJoinUnauthorized, "waiting for an operator to approve this node")

	if b.Status().Uplink.State != UplinkDenied {
		t.Fatalf("the state is %q after a denial, want %q", b.Status().Uplink.State, UplinkDenied)
	}

	b.joinFailed(t.Context(), errors.New("connect to brno:4222: no servers available for connection"))

	held := b.Status().Uplink

	if held.State != UplinkJoining {
		t.Errorf("the state is %q and the node is trying again, so it is %q", held.State, UplinkJoining)
	}

	if !strings.Contains(held.Reason, "no servers available") {
		t.Errorf("the reason is %q and the newest one is the connection that failed", held.Reason)
	}

	if held.Code != 0 {
		t.Errorf("the code is %d and no denial stands, so it is 0", held.Code)
	}
}
