package node_test

import (
	"context"
	"encoding/json"
	"testing"
	"testing/synctest"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	"github.com/prusa3d/prusa-connect/sdk/fractal/node"
)

// desire builds a DES for the test interface, addressed to receiver.
func desire(receiver fractal.NodePath, props fractal.Props) fractal.DES {
	return fractal.DES{
		Version:   fractal.Version,
		Receiver:  receiver,
		Interface: ifaceName,
		Instance:  ifaceInstance,
		Props:     props,
	}
}

// TestAnAcceptedChangeIsReported checks that when the owner accepts a DES, the
// node sends one REP with the properties Owner.Desire returned and the current
// time (fractal.md, Desired Properties Message).
func TestAnAcceptedChangeIsReported(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, printerPath)
		payload := encode(t, desire(printerPath, fractal.Props{targetProp: json.RawMessage(`220.0`)}))
		h.node.Deliver(t.Context(), node.Inbound{Kind: fractal.KindDES, Payload: payload})
		synctest.Wait()

		reports := h.binding.OfKind(fractal.KindREP)
		if len(reports) != 1 {
			t.Fatalf("the node sent %d REP messages, want one", len(reports))
		}
		var m fractal.REP
		if err := json.Unmarshal(reports[0].dispatch.Payload, &m); err != nil {
			t.Fatalf("decode the REP: %v", err)
		}
		if m.Sender != printerPath || m.Timestamp != now {
			t.Errorf("the REP has sender %s and timestamp %d, want %s and %d", m.Sender, m.Timestamp, printerPath, now)
		}
		if string(m.Props[targetProp]) != "220.0" {
			t.Errorf("the REP has %s = %s, want 220.0", targetProp, m.Props[targetProp])
		}
	})
}

// TestARejectedChangeIsAnsweredWithNothing checks that the node sends no
// message for a DES it rejects. The cases are an instance the node does not
// own, a property the definition does not declare, a read-only property, a
// value of the wrong type, and an owner that returns an error (fractal.md,
// Desired Properties Message).
func TestARejectedChangeIsAnsweredWithNothing(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		setUp func(h *harness, m *fractal.DES)
	}{
		{
			"an interface the node does not own",
			func(_ *harness, m *fractal.DES) { m.Instance = "9" },
		},
		{
			"a property the interface does not define",
			func(_ *harness, m *fractal.DES) { m.Props = fractal.Props{"chamberTemp": json.RawMessage(`1.0`)} },
		},
		{
			"a read-only property",
			func(_ *harness, m *fractal.DES) { m.Props = fractal.Props{tempProp: json.RawMessage(`1.0`)} },
		},
		{
			"a value of the wrong type",
			func(_ *harness, m *fractal.DES) { m.Props = fractal.Props{targetProp: json.RawMessage(`"warm"`)} },
		},
		{
			"an owner that refused",
			func(h *harness, _ *fractal.DES) { h.owner.desireErr = errBroken },
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				h := newHarness(t, printerPath)
				m := desire(printerPath, fractal.Props{targetProp: json.RawMessage(`220.0`)})
				c.setUp(h, &m)
				h.node.Deliver(t.Context(), node.Inbound{Kind: fractal.KindDES, Payload: encode(t, m)})
				synctest.Wait()

				if got := len(h.binding.Sent()); got != 0 {
					t.Errorf("the node sent %d messages, want none", got)
				}
			})
		})
	}
}

// TestAChangeIsRoutedDown checks that a node forwards a DES for a node below
// it to printer-1 with a best-effort send, and does not change the payload
// (fractal.md, Message Routing (Unicast)).
func TestAChangeIsRoutedDown(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, brnoPath)
		payload := encode(t, desire(printerPath, fractal.Props{targetProp: json.RawMessage(`220.0`)}))
		h.node.Deliver(t.Context(), node.Inbound{Kind: fractal.KindDES, Payload: payload})
		synctest.Wait()

		forwarded := h.binding.OfKind(fractal.KindDES)
		if len(forwarded) != 1 {
			t.Fatalf("the node forwarded %d DES messages, want one", len(forwarded))
		}
		if forwarded[0].dispatch.Child != printerID || forwarded[0].durable {
			t.Errorf("the DES went to %q with durable=%v, want %q with a best-effort send",
				forwarded[0].dispatch.Child, forwarded[0].durable, printerID)
		}
		if string(forwarded[0].dispatch.Payload) != string(payload) {
			t.Error("the node changed the payload of the DES, want the bytes that arrived")
		}
	})
}

// TestAChangeForNoChildIsDropped checks that the node sends nothing for a DES
// whose receiver is under a node this one has not admitted (fractal.md, Message
// Routing (Unicast)).
func TestAChangeForNoChildIsDropped(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, brnoPath)
		payload := encode(t, desire("hq/brno/printer-9", fractal.Props{targetProp: json.RawMessage(`1.0`)}))
		h.node.Deliver(t.Context(), node.Inbound{Kind: fractal.KindDES, Payload: payload})
		synctest.Wait()

		if got := len(h.binding.Sent()); got != 0 {
			t.Errorf("the node sent %d messages, want none", got)
		}
	})
}

// TestAFullReportRequestAnswersAndIsPassedOn checks that a node answers an RBR
// with a REP for the interface it owns, and then forwards the RBR unchanged to
// the node directly below it. The REP goes first (fractal.md, Full State Report
// Request Message).
func TestAFullReportRequestAnswersAndIsPassedOn(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, brnoPath)
		payload := encode(t, fractal.RBR{Version: fractal.Version})
		h.node.Deliver(t.Context(), node.Inbound{Kind: fractal.KindRBR, Payload: payload})
		synctest.Wait()

		messages := h.binding.Sent()
		if len(messages) != 2 {
			t.Fatalf("the node sent %d messages, want one REP and one RBR", len(messages))
		}
		if messages[0].dispatch.Kind != fractal.KindREP {
			t.Errorf("the first message is %s, want the REP first", messages[0].dispatch.Kind)
		}
		if messages[1].dispatch.Kind != fractal.KindRBR || messages[1].dispatch.Child != printerID {
			t.Errorf("the second message is %s to %q, want the RBR to %q",
				messages[1].dispatch.Kind, messages[1].dispatch.Child, printerID)
		}
		if string(messages[1].dispatch.Payload) != string(payload) {
			t.Error("the node changed the payload of the RBR, want the bytes that arrived")
		}
	})
}

// TestAnOwnerThatCannotReadItsStateIsSkipped checks an owner whose Snapshot
// returns an error. The node sends no REP for that interface and still forwards
// the RBR to the node directly below it.
func TestAnOwnerThatCannotReadItsStateIsSkipped(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, brnoPath)
		h.owner.snapshotErr = errBroken
		h.node.Deliver(t.Context(), node.Inbound{
			Kind:    fractal.KindRBR,
			Payload: encode(t, fractal.RBR{Version: fractal.Version}),
		})
		synctest.Wait()

		if got := len(h.binding.OfKind(fractal.KindREP)); got != 0 {
			t.Errorf("the node sent %d REP messages, want none", got)
		}
		if got := len(h.binding.OfKind(fractal.KindRBR)); got != 1 {
			t.Errorf("the node forwarded %d RBR messages, want one", got)
		}
	})
}

// TestStartupAsksEveryChildForAFullReport checks that Start on a node with a
// Node Path sends an RBR to every node directly below it (fractal.md,
// Synchronization of the Shadow - Happy path).
func TestStartupAsksEveryChildForAFullReport(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		binding := &recordBinding{}
		n, err := node.New(node.Config{
			Binding:    binding,
			Admissions: children{printerID},
			Path:       brnoPath,
		})
		if err != nil {
			t.Fatalf("build the node: %v", err)
		}
		if startErr := n.Start(t.Context()); startErr != nil {
			t.Fatalf("start the node: %v", startErr)
		}
		t.Cleanup(func() { _ = n.Close() })
		synctest.Wait()

		if got := len(binding.OfKind(fractal.KindRBR)); got != 1 {
			t.Errorf("the node sent %d RBR messages, want one", got)
		}
	})
}

// TestABusyNodeStillAnswersAFullReportRequest checks that an RBR that arrives
// while every worker is busy and the work queue is full is still answered
// (fractal.md, Full State Report Request Message: the node MUST report and
// pass the request on).
func TestABusyNodeStillAnswersAFullReportRequest(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		binding := &recordBinding{}
		release := make(chan struct{})
		n, err := node.New(node.Config{
			Binding:    binding,
			Admissions: children{printerID},
			Clock:      &fixedClock{at: now},
			Path:       brnoPath,
			Workers:    1,
			WorkQueue:  1,
		})
		if err != nil {
			t.Fatalf("build the node: %v", err)
		}
		registerOn(t, n, slowDesireOwner{testOwner: &testOwner{}, release: release})
		if startErr := n.Start(t.Context()); startErr != nil {
			t.Fatalf("start the node: %v", startErr)
		}
		t.Cleanup(func() { _ = n.Close() })
		synctest.Wait()
		binding.Reset()

		change := encode(t, desire(brnoPath, fractal.Props{targetProp: json.RawMessage(`220.0`)}))
		for range 2 {
			n.Deliver(t.Context(), node.Inbound{Kind: fractal.KindDES, Payload: change})
		}
		synctest.Wait()
		ask := encode(t, fractal.RBR{Version: fractal.Version})
		n.Deliver(t.Context(), node.Inbound{Kind: fractal.KindRBR, Payload: ask})
		close(release)
		synctest.Wait()

		if got := len(binding.OfKind(fractal.KindRBR)); got != 1 {
			t.Errorf("the node forwarded %d RBR messages, want one", got)
		}
	})
}

// slowDesireOwner is an Owner whose Desire returns only once release is closed.
type slowDesireOwner struct {
	*testOwner

	release chan struct{}
}

// Desire waits for release to close and then accepts the change.
func (o slowDesireOwner) Desire(ctx context.Context, props fractal.Props) (fractal.Props, error) {
	<-o.release
	return o.testOwner.Desire(ctx, props)
}
