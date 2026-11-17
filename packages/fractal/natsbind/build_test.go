package natsbind_test

import (
	"errors"
	"testing"

	"github.com/prusa3d/fractal-link/natsbind"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	"github.com/prusa3d/prusa-connect/sdk/fractal/fractaltest"
	fractalnode "github.com/prusa3d/prusa-connect/sdk/fractal/node"
)

// TestABindingIsRefusedBeforeAnythingIsOpened checks that New returns an error
// for a nil engine and for a Config with no deduplication window, before it
// opens a file or a connection.
func TestABindingIsRefusedBeforeAnythingIsOpened(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	good, _ := baseConfig(t, nodeOptions{id: "printer-1"})
	if _, err := natsbind.New(ctx, good, nil); err == nil {
		t.Error("a binding with no engine was built")
	}
	bad := good
	bad.DedupWindow = 0
	if _, err := natsbind.New(ctx, bad, fractaltest.NewRecorder(true)); !errors.Is(err, natsbind.ErrNoDedupWindow) {
		t.Errorf("the error is %v, want ErrNoDedupWindow", err)
	}
}

// TestANodeThatHasNotStartedReadsAsEmpty checks the Status of a node before
// Start. The links page shows that Status while the node starts.
func TestANodeThatHasNotStartedReadsAsEmpty(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	cfg, _ := baseConfig(t, nodeOptions{id: "hq", accepts: true})
	built := buildNode(ctx, t, cfg)

	status := built.binding.Status()
	switch {
	case status.NodeID != "hq":
		t.Errorf("Status.NodeID is %q, want hq", status.NodeID)
	case status.NodePath != "hq":
		t.Errorf("Status.NodePath is %q, want hq", status.NodePath)
	case !status.Accepting:
		t.Error("Status.Accepting is false on a node that accepts joins")
	case status.Mode != natsbind.ModeTOFU:
		t.Errorf("Status.Mode is %q, want tofu", status.Mode)
	case len(status.Listeners) != 0:
		t.Errorf("Status lists %d listeners on a node that has not started", len(status.Listeners))
	case len(status.Nodes) != 0:
		t.Errorf("Status lists %d nodes below a node that no node has joined", len(status.Nodes))
	}
	if built.binding.Admitted("printer-1") {
		t.Error("Admitted is true for a node that never joined")
	}
	for id := range built.binding.Children() {
		t.Errorf("Children lists %q on a node that no node has joined", id)
	}
	if built.binding.Relocated() == nil {
		t.Error("Relocated returned a nil channel")
	}
}

// TestAMessageToANodeNobodyAdmittedIsRefused checks that SendBestEffort and
// SendDurable return ErrNoLink for a Node ID that never joined.
func TestAMessageToANodeNobodyAdmittedIsRefused(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	p := newPair(ctx, t)
	if err := p.above.binding.SendBestEffort(
		ctx,
		dispatchTo("printer-9", fractal.KindDES),
	); !errors.Is(
		err,
		natsbind.ErrNoLink,
	) {
		t.Errorf("SendBestEffort returned %v, want ErrNoLink", err)
	}
	if err := p.above.binding.SendDurable(
		ctx,
		dispatchTo("printer-9", fractal.KindRPC),
	); !errors.Is(
		err,
		natsbind.ErrNoLink,
	) {
		t.Errorf("SendDurable returned %v, want ErrNoLink", err)
	}
	if err := p.above.binding.SendBestEffort(ctx, dispatchTo("printer-1", fractal.KindREP)); err == nil {
		t.Error("SendBestEffort sent a state report downward")
	}
}

// dispatchTo builds a Dispatch of the given kind for the node below with this
// Node ID.
func dispatchTo(id fractal.NodeID, kind fractal.Kind) fractalnode.Dispatch {
	return fractalnode.Dispatch{Kind: kind, Child: id, Payload: []byte(`{"version":1}`)}
}

// TestAnAncestorMessageGoesOneWayOnly checks the direction of AREP, ALOS and
// ARBR. A node sends AREP and ALOS only to a node below and ARBR only to the
// node above. SendBestEffort refuses the other direction with ErrNoSubject
// (fractal-nats.md, Subject Namespace).
func TestAnAncestorMessageGoesOneWayOnly(t *testing.T) {
	t.Parallel()
	p := newPair(t.Context(), t)
	cases := []struct {
		name string
		from *node
		d    fractalnode.Dispatch
		want error
	}{
		{"AREP goes down", p.above, dispatchTo("printer-1", fractal.KindAREP), nil},
		{"ALOS goes down", p.above, dispatchTo("printer-1", fractal.KindALOS), nil},
		{"ARBR goes up", p.below, dispatchTo("", fractal.KindARBR), nil},
		{"AREP does not go up", p.below, dispatchTo("", fractal.KindAREP), natsbind.ErrNoSubject},
		{"ALOS does not go up", p.below, dispatchTo("", fractal.KindALOS), natsbind.ErrNoSubject},
		{"ARBR does not go down", p.above, dispatchTo("printer-1", fractal.KindARBR), natsbind.ErrNoSubject},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if err := c.from.binding.SendBestEffort(t.Context(), c.d); !errors.Is(err, c.want) {
				t.Errorf("SendBestEffort returned %v, want %v", err, c.want)
			}
		})
	}
}

// TestANodeThatAcceptsNobodyOpensOnlyItsOwnListener checks the ports of a node
// with no accept block. Nothing outside this process connects to such a node,
// so it opens the client listener on the loopback address and a free port, and
// no leafnode listener at all. Two of them on one machine would otherwise both
// take the default ports and the second one could not start.
func TestANodeThatAcceptsNobodyOpensOnlyItsOwnListener(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	build := func() *node {
		cfg := natsbind.Config{
			NodeID:       "printer-1",
			DataDir:      t.TempDir(),
			DedupWindow:  testDedupWindow,
			AckWait:      testAckWait,
			PollInterval: testPoll,
		}
		return startNode(ctx, t, cfg)
	}
	first, second := build(), build()

	for _, built := range []*node{first, second} {
		open := built.binding.Status().Listeners
		if len(open) != 1 {
			t.Fatalf("the node listens on %v, want the client listener alone", open)
		}
		if open[0].Protocol != "nats" {
			t.Errorf("the one listener is %q, want the client listener", open[0].Protocol)
		}
		if open[0].Address != "127.0.0.1" {
			t.Errorf("the client listener is on %q, want the loopback address", open[0].Address)
		}
		if open[0].Port == 4222 {
			t.Errorf("the client listener took the default port %d", open[0].Port)
		}
	}
	if first.binding.Status().Listeners[0].Port == second.binding.Status().Listeners[0].Port {
		t.Error("both nodes took the same port")
	}
}
