package node_test

import (
	"encoding/json"
	"log/slog"
	"maps"
	"slices"
	"testing"
	"testing/synctest"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	"github.com/prusa3d/prusa-connect/sdk/fractal/node"
	"github.com/prusa3d/prusa-connect/sdk/fractal/shadow"
)

// TestAReportIsAppliedAndPassedOn checks that a node writes a REP from below to
// its shadow, and forwards the bytes that arrived to the node above with a
// best-effort send (fractal.md, Reported Properties Message).
func TestAReportIsAppliedAndPassedOn(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, brnoPath)
		payload := encode(t, report(now, fractal.Props{tempProp: json.RawMessage(`200.0`)}))
		h.node.Deliver(t.Context(), node.Inbound{Kind: fractal.KindREP, Payload: payload, From: printerID})
		synctest.Wait()

		forwarded := h.binding.OfKind(fractal.KindREP)
		if len(forwarded) != 1 {
			t.Fatalf("the node forwarded %d REP messages, want one", len(forwarded))
		}
		if string(forwarded[0].dispatch.Payload) != string(payload) {
			t.Errorf("the node changed the payload of the REP, want the bytes that arrived")
		}
		if forwarded[0].durable {
			t.Error("the node sent the REP with a durable send, want a best-effort send")
		}
		key := shadow.Key{Node: printerPath, Interface: ifaceName, Instance: ifaceInstance}
		snap, ok := h.shadow.Get(key)
		if !ok {
			t.Fatalf("the shadow has no entry for %s", key)
		}
		if snap.Status != shadow.StatusOK || snap.UpdatedAt != now {
			t.Errorf("the shadow entry has status %s and timestamp %d, want OK and %d",
				snap.Status, snap.UpdatedAt, now)
		}
	})
}

// TestAnOldReportIsNotPassedOn checks that a node does not forward a REP whose
// timestamp is lower than the one its shadow has for that interface
// (fractal.md, Reported Properties Message).
func TestAnOldReportIsNotPassedOn(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, brnoPath)
		fresh := encode(t, report(now, fractal.Props{tempProp: json.RawMessage(`200.0`)}))
		h.node.Deliver(t.Context(), node.Inbound{Kind: fractal.KindREP, Payload: fresh, From: printerID})
		synctest.Wait()
		h.binding.Reset()

		old := encode(t, report(now-1, fractal.Props{tempProp: json.RawMessage(`25.0`)}))
		h.node.Deliver(t.Context(), node.Inbound{Kind: fractal.KindREP, Payload: old, From: printerID})
		synctest.Wait()

		if got := len(h.binding.OfKind(fractal.KindREP)); got != 0 {
			t.Errorf("the node forwarded %d REP messages with a lower timestamp, want none", got)
		}
	})
}

// TestARootStopsAnUpwardBroadcast checks that a root node sends nothing when
// it receives a REP, because it has no node above it (fractal.md, Message
// Routing (Directional Broadcast)).
func TestARootStopsAnUpwardBroadcast(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, hqPath)
		payload := encode(t, fractal.REP{
			Version:   fractal.Version,
			Sender:    "hq/printer-1",
			Interface: ifaceName,
			Instance:  ifaceInstance,
			Timestamp: now,
			Props:     fractal.Props{tempProp: json.RawMessage(`25.0`)},
		})
		h.node.Deliver(t.Context(), node.Inbound{Kind: fractal.KindREP, Payload: payload, From: printerID})
		synctest.Wait()

		if got := len(h.binding.Sent()); got != 0 {
			t.Errorf("a root node sent %d messages, want none", got)
		}
	})
}

// TestAMessageIsDroppedForItsReason checks that a node rejects each kind of
// bad inbound message: it writes nothing to the shadow and sends nothing.
func TestAMessageIsDroppedForItsReason(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		level slog.Level
		in    func(t *testing.T) node.Inbound
	}{
		{
			"an unsupported protocol version",
			slog.LevelError,
			func(t *testing.T) node.Inbound {
				m := report(now, fractal.Props{tempProp: json.RawMessage(`1.0`)})
				m.Version = fractal.Version + 1
				return node.Inbound{Kind: fractal.KindREP, Payload: encode(t, m), From: printerID}
			},
		},
		{
			"a payload that is not the message type",
			slog.LevelWarn,
			func(*testing.T) node.Inbound {
				return node.Inbound{Kind: fractal.KindREP, Payload: []byte("not json"), From: printerID}
			},
		},
		{
			"a name outside its grammar",
			slog.LevelWarn,
			func(t *testing.T) node.Inbound {
				m := report(now, fractal.Props{tempProp: json.RawMessage(`1.0`)})
				m.Interface = "not a name"
				return node.Inbound{Kind: fractal.KindREP, Payload: encode(t, m), From: printerID}
			},
		},
		{
			"a sender that is not the node the message arrived from, or under it",
			slog.LevelError,
			func(t *testing.T) node.Inbound {
				m := report(now, fractal.Props{tempProp: json.RawMessage(`1.0`)})
				m.Sender = "hq/brno/other-printer"
				return node.Inbound{Kind: fractal.KindREP, Payload: encode(t, m), From: printerID}
			},
		},
		{
			"an upward message from the node above",
			slog.LevelWarn,
			func(t *testing.T) node.Inbound {
				m := report(now, fractal.Props{tempProp: json.RawMessage(`1.0`)})
				return node.Inbound{Kind: fractal.KindREP, Payload: encode(t, m)}
			},
		},
		{
			"a downward message from a node below",
			slog.LevelWarn,
			func(t *testing.T) node.Inbound {
				m := fractal.DES{
					Version:   fractal.Version,
					Receiver:  printerPath,
					Interface: ifaceName,
					Instance:  ifaceInstance,
					Props:     fractal.Props{targetProp: json.RawMessage(`1.0`)},
				}
				return node.Inbound{Kind: fractal.KindDES, Payload: encode(t, m), From: printerID}
			},
		},
		{
			"a downward message for no node below",
			slog.LevelError,
			func(t *testing.T) node.Inbound {
				m := fractal.DES{
					Version:   fractal.Version,
					Receiver:  "hq/brno/printer-9",
					Interface: ifaceName,
					Instance:  ifaceInstance,
					Props:     fractal.Props{targetProp: json.RawMessage(`1.0`)},
				}
				return node.Inbound{Kind: fractal.KindDES, Payload: encode(t, m)}
			},
		},
		{
			"a message type the transport does not name",
			slog.LevelWarn,
			func(*testing.T) node.Inbound {
				return node.Inbound{Kind: fractal.Kind(0), Payload: []byte("{}")}
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				h := newHarness(t, brnoPath)
				h.node.Deliver(t.Context(), c.in(t))
				synctest.Wait()
				if got := len(h.binding.Sent()); got != 0 {
					t.Errorf("the node sent %d messages, want none", got)
				}
				if got := h.shadow.Len(); got != 0 {
					t.Errorf("the shadow has %d interfaces, want none", got)
				}
				if got, want := h.log.Levels("message dropped"), []slog.Level{c.level}; !slices.Equal(got, want) {
					t.Errorf("the drop was logged at %v, want %v", got, want)
				}
			})
		})
	}
}

// TestADurableMessageIsAcknowledgedWhenDropped checks an RPC with an
// unsupported version. The node drops it, sends no answer and calls Ack, so
// the transport stops delivering it (fractal-nats.md, Message Encoding).
func TestADurableMessageIsAcknowledgedWhenDropped(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, brnoPath)
		m := invocation(printerPath)
		m.Version = fractal.Version + 1
		a := &acker{}
		h.node.Deliver(t.Context(), node.Inbound{
			Kind:    fractal.KindRPC,
			Payload: encode(t, m),
			Ack:     a.ack,
		})
		synctest.Wait()

		if got := a.Count(); got != 1 {
			t.Errorf("the node called Ack %d times, want once", got)
		}
		if got := len(h.binding.Sent()); got != 0 {
			t.Errorf("the node sent %d messages, want none: the node does not answer a request it drops", got)
		}
	})
}

// TestANodeWithNoPathProcessesNothing checks that a node with no Node Path
// forwards nothing when it receives a REP (fractal.md, Node Path).
func TestANodeWithNoPathProcessesNothing(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		binding := &recordBinding{}
		n, err := node.New(node.Config{Binding: binding, Admissions: children{printerID}})
		if err != nil {
			t.Fatalf("build the node: %v", err)
		}
		if startErr := n.Start(t.Context()); startErr != nil {
			t.Fatalf("start the node: %v", startErr)
		}
		t.Cleanup(func() { _ = n.Close() })

		payload := encode(t, report(now, fractal.Props{tempProp: json.RawMessage(`1.0`)}))
		n.Deliver(t.Context(), node.Inbound{Kind: fractal.KindREP, Payload: payload, From: printerID})
		synctest.Wait()

		if got := len(binding.Sent()); got != 0 {
			t.Errorf("the node sent %d messages, want none", got)
		}
		if n.Path() != "" {
			t.Errorf("Path is %q, want an empty path", n.Path())
		}
	})
}

// TestALossMarksTheSubtreeAndIsPassedOn checks that a node forwards a LOS from
// below to the node above as the bytes that arrived (fractal.md, Connection
// Loss Message).
func TestALossMarksTheSubtreeAndIsPassedOn(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, brnoPath)
		payload := encode(t, report(now, fractal.Props{tempProp: json.RawMessage(`200.0`)}))
		h.node.Deliver(t.Context(), node.Inbound{Kind: fractal.KindREP, Payload: payload, From: printerID})
		synctest.Wait()
		h.binding.Reset()

		loss := encode(t, fractal.LOS{Version: fractal.Version, Sender: printerPath, Child: "extruder-1"})
		h.node.Deliver(t.Context(), node.Inbound{Kind: fractal.KindLOS, Payload: loss, From: printerID})
		synctest.Wait()

		passed := h.binding.OfKind(fractal.KindLOS)
		if len(passed) != 1 {
			t.Fatalf("the node forwarded %d LOS messages, want one", len(passed))
		}
		if string(passed[0].dispatch.Payload) != string(loss) {
			t.Error("the node changed the payload of the LOS, want the bytes that arrived")
		}
	})
}

// TestAChildLinkGoingDownReportsTheLoss checks ChildLost. The node marks the
// interface of printer-1 as STALE in its shadow and sends a LOS that names
// printer-1 to the node above (fractal.md, Connection Loss Message).
func TestAChildLinkGoingDownReportsTheLoss(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, brnoPath)
		payload := encode(t, report(now, fractal.Props{tempProp: json.RawMessage(`200.0`)}))
		h.node.Deliver(t.Context(), node.Inbound{Kind: fractal.KindREP, Payload: payload, From: printerID})
		synctest.Wait()
		h.binding.Reset()

		h.node.ChildLost(t.Context(), printerID)
		synctest.Wait()

		messages := h.binding.OfKind(fractal.KindLOS)
		if len(messages) != 1 {
			t.Fatalf("the node sent %d LOS messages, want one", len(messages))
		}
		var los fractal.LOS
		if err := json.Unmarshal(messages[0].dispatch.Payload, &los); err != nil {
			t.Fatalf("decode the LOS: %v", err)
		}
		if los.Sender != brnoPath || los.Child != printerID {
			t.Errorf("the LOS has sender %s and child %s, want %s and %s", los.Sender, los.Child, brnoPath, printerID)
		}
		key := shadow.Key{Node: printerPath, Interface: ifaceName, Instance: ifaceInstance}
		status, _, ok := h.shadow.Status(key)
		if !ok || status != shadow.StatusStale {
			t.Errorf("the shadow entry has status %s, want STALE", status)
		}
	})
}

// TestAChildLinkComingUpAsksForAFullReport checks ChildConnected. The node
// sends a new RBR to the node whose link came up (fractal.md, Synchronization
// of the Shadow - Reaction to Connection Loss and Reconnection).
func TestAChildLinkComingUpAsksForAFullReport(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, brnoPath)
		h.node.ChildConnected(t.Context(), printerID)
		synctest.Wait()

		messages := h.binding.OfKind(fractal.KindRBR)
		if len(messages) != 1 {
			t.Fatalf("the node sent %d RBR messages, want one", len(messages))
		}
		if messages[0].dispatch.Child != printerID {
			t.Errorf("the RBR went to %q, want %q", messages[0].dispatch.Child, printerID)
		}
	})
}

// TestConnectivityBeforeAPathDoesNothing checks that ChildConnected and
// ChildLost send nothing while the node has no Node Path.
func TestConnectivityBeforeAPathDoesNothing(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		binding := &recordBinding{}
		n, err := node.New(node.Config{Binding: binding, Admissions: children{printerID}})
		if err != nil {
			t.Fatalf("build the node: %v", err)
		}
		n.ChildConnected(t.Context(), printerID)
		n.ChildLost(t.Context(), printerID)
		synctest.Wait()
		if got := len(binding.Sent()); got != 0 {
			t.Errorf("the node sent %d messages, want none", got)
		}
	})
}

// all returns a copy of every count, by reason.
func (m *countingMetrics) all() map[node.DropReason]int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return maps.Clone(m.drops)
}

// TestADropIsCountedUnderItsReason checks the reason the node counts a message
// it cannot read under: DropMalformed for a payload that is not JSON or has a
// field outside its grammar, and DropUnsupportedVersion for a version this
// package does not implement (fractal.md, Protocol Version).
func TestADropIsCountedUnderItsReason(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		reason node.DropReason
		in     func(t *testing.T) node.Inbound
	}{
		{"a report that is not json", node.DropMalformed, func(*testing.T) node.Inbound {
			return node.Inbound{Kind: fractal.KindREP, Payload: []byte("{"), From: printerID}
		}},
		{"a report of another version", node.DropUnsupportedVersion, func(t *testing.T) node.Inbound {
			return node.Inbound{Kind: fractal.KindREP, Payload: wrongVersion(t, report(now, nil)), From: printerID}
		}},
		{"a report naming no valid interface", node.DropMalformed, func(t *testing.T) node.Inbound {
			m := report(now, nil)
			m.Interface = "not a name"
			return node.Inbound{Kind: fractal.KindREP, Payload: encode(t, m), From: printerID}
		}},
		{"a loss message of another version", node.DropUnsupportedVersion, func(t *testing.T) node.Inbound {
			m := fractal.LOS{Version: fractal.Version, Sender: printerPath, Child: "extruder-1"}
			return node.Inbound{Kind: fractal.KindLOS, Payload: wrongVersion(t, m), From: printerID}
		}},
		{"a requested change of another version", node.DropUnsupportedVersion, func(t *testing.T) node.Inbound {
			m := desire(printerPath, fractal.Props{targetProp: json.RawMessage(`1.0`)})
			return node.Inbound{Kind: fractal.KindDES, Payload: wrongVersion(t, m)}
		}},
		{"a method invocation of another version", node.DropUnsupportedVersion, func(t *testing.T) node.Inbound {
			return node.Inbound{Kind: fractal.KindRPC, Payload: wrongVersion(t, invocation(printerPath))}
		}},
		{"an answer of another version", node.DropUnsupportedVersion, func(t *testing.T) node.Inbound {
			m := fractal.RES{Version: fractal.Version, ID: "a", Receiver: hqPath}
			return node.Inbound{Kind: fractal.KindRES, Payload: wrongVersion(t, m), From: printerID}
		}},
		{"a request for a full report that is not json", node.DropMalformed, func(*testing.T) node.Inbound {
			return node.Inbound{Kind: fractal.KindRBR, Payload: []byte("{")}
		}},
		{
			"a request for a full report of another version",
			node.DropUnsupportedVersion,
			func(t *testing.T) node.Inbound {
				return node.Inbound{
					Kind:    fractal.KindRBR,
					Payload: wrongVersion(t, fractal.RBR{Version: fractal.Version}),
				}
			},
		},
		{"an ancestor report that is not json", node.DropMalformed, func(*testing.T) node.Inbound {
			return node.Inbound{Kind: fractal.KindAREP, Payload: []byte("{")}
		}},
		{"an ancestor report of another version", node.DropUnsupportedVersion, func(t *testing.T) node.Inbound {
			return node.Inbound{Kind: fractal.KindAREP, Payload: wrongVersion(t, fractal.AREP(report(now, nil)))}
		}},
		{"an ancestor report naming no valid sender", node.DropMalformed, func(t *testing.T) node.Inbound {
			m := fractal.AREP(report(now, nil))
			m.Sender = "hq//a"
			return node.Inbound{Kind: fractal.KindAREP, Payload: encode(t, m)}
		}},
		{"an ancestor loss of another version", node.DropUnsupportedVersion, func(t *testing.T) node.Inbound {
			return node.Inbound{Kind: fractal.KindALOS, Payload: wrongVersion(t, fractal.ALOS{
				Version: fractal.Version, Sender: hqPath,
			})}
		}},
		{"an ancestor loss naming no valid sender", node.DropMalformed, func(t *testing.T) node.Inbound {
			return node.Inbound{Kind: fractal.KindALOS, Payload: encode(t, fractal.ALOS{
				Version: fractal.Version, Sender: "/hq",
			})}
		}},
		{"a request for the state above that is not json", node.DropMalformed, func(*testing.T) node.Inbound {
			return node.Inbound{Kind: fractal.KindARBR, Payload: []byte("{"), From: printerID}
		}},
		{
			"a request for the state above of another version",
			node.DropUnsupportedVersion,
			func(t *testing.T) node.Inbound {
				return node.Inbound{
					Kind:    fractal.KindARBR,
					Payload: wrongVersion(t, fractal.ARBR{Version: fractal.Version}),
					From:    printerID,
				}
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				n, binding, metrics := newCountingNode(t, nil)
				n.Deliver(t.Context(), c.in(t))
				synctest.Wait()

				want := map[node.DropReason]int{}
				want[c.reason] = 1
				if got := metrics.all(); !maps.Equal(got, want) {
					t.Errorf("the node counted %v, want %v", got, want)
				}
				if got := binding.Sent(); len(got) != 0 {
					t.Errorf("the node sent %d messages, want none", len(got))
				}
			})
		})
	}
}

// TestARevokedChildLeavesTheShadow checks that the shadow of a node below
// and of every node under it goes when that node is revoked. A revocation
// is the one permanent removal this node knows of (fractal.md, Shadow Status,
// the removed row: its shadow SHOULD be deleted).
func TestARevokedChildLeavesTheShadow(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, brnoPath)
		h.node.Deliver(t.Context(), node.Inbound{
			Kind: fractal.KindREP, Payload: encode(t, report(now, fractal.Props{tempProp: json.RawMessage(`1.0`)})),
			From: printerID,
		})
		synctest.Wait()
		before := h.shadow.Len()
		h.node.ChildRevoked(t.Context(), printerID)

		if got := []int{before, h.shadow.Len()}; !slices.Equal(got, []int{1, 0}) {
			t.Errorf("the shadow held %d interfaces before and %d after the revocation, want 1 and 0", got[0], got[1])
		}
	})
}
