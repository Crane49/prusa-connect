package node_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	"github.com/prusa3d/prusa-connect/sdk/fractal/node"
)

// failingDedup is an ExecutedStore and a ProcessedStore whose every method
// returns errBroken, like a store whose disk has failed.
type failingDedup struct{}

// Executed returns errBroken.
func (failingDedup) Executed(context.Context, fractal.RPCID) ([]byte, bool, error) {
	return nil, false, errBroken
}

// RecordExecuted returns errBroken.
func (failingDedup) RecordExecuted(context.Context, fractal.RPCID, []byte) error { return errBroken }

// Processed returns errBroken.
func (failingDedup) Processed(context.Context, fractal.RPCID) (bool, error) { return false, errBroken }

// RecordProcessed returns errBroken.
func (failingDedup) RecordProcessed(context.Context, fractal.RPCID) error { return errBroken }

// TestNewRefusesANodeWithNoTransport checks that New returns ErrNoBinding for a
// Config with no Binding.
func TestNewRefusesANodeWithNoTransport(t *testing.T) {
	t.Parallel()
	if _, err := node.New(node.Config{}); !errors.Is(err, node.ErrNoBinding) {
		t.Errorf("New returned %v, want %v", err, node.ErrNoBinding)
	}
}

// TestNewRefusesAPathOutsideItsGrammar checks that New validates Config.Path
// against the Node Path grammar (fractal.md, Node Path).
func TestNewRefusesAPathOutsideItsGrammar(t *testing.T) {
	t.Parallel()
	if _, err := node.New(node.Config{Binding: &recordBinding{}, Path: "hq//brno"}); err == nil {
		t.Error("New accepted a path with an empty segment, want an error")
	}
}

// TestANodeStartsOnceAndStopsTwice checks that a second Start returns
// ErrAlreadyStarted and a second Close returns nil.
func TestANodeStartsOnceAndStopsTwice(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		n, err := node.New(node.Config{Binding: &recordBinding{}, Path: brnoPath})
		if err != nil {
			t.Fatalf("build the node: %v", err)
		}
		if startErr := n.Start(t.Context()); startErr != nil {
			t.Fatalf("start the node: %v", startErr)
		}
		if startErr := n.Start(t.Context()); !errors.Is(startErr, node.ErrAlreadyStarted) {
			t.Errorf("the second Start returned %v, want %v", startErr, node.ErrAlreadyStarted)
		}
		if closeErr := n.Close(); closeErr != nil {
			t.Errorf("Close returned %v, want nil", closeErr)
		}
		if closeErr := n.Close(); closeErr != nil {
			t.Errorf("the second Close returned %v, want nil", closeErr)
		}
	})
}

// TestTheShadowIsTheOneItWasGiven checks that Node.Shadow returns the store
// from Config.Shadow.
func TestTheShadowIsTheOneItWasGiven(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, brnoPath)
		if h.node.Shadow() != h.shadow {
			t.Error("Shadow returned a different store than the one in Config.Shadow")
		}
	})
}

// TestTakingTheSamePathAgainChangesNothing checks that SetPath accepts the Node
// Path the node already has, and refuses a path that is not valid.
func TestTakingTheSamePathAgainChangesNothing(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, brnoPath)
		if err := h.node.SetPath(t.Context(), brnoPath); err != nil {
			t.Fatalf("set the same path again: %v", err)
		}
		if err := h.node.SetPath(t.Context(), "hq//brno"); err == nil {
			t.Error("SetPath accepted a path with an empty segment, want an error")
		}
	})
}

// TestAPathGrantedLaterStartsTheProcedure checks a node that starts with no
// Node Path. It sends nothing until SetPath gives it a path, and then sends an
// RBR to every node directly below it (fractal.md, Synchronization of the
// Shadow - Happy path).
func TestAPathGrantedLaterStartsTheProcedure(t *testing.T) {
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
		synctest.Wait()
		if got := len(binding.Sent()); got != 0 {
			t.Fatalf("the node sent %d messages before it had a path, want none", got)
		}

		if pathErr := n.SetPath(t.Context(), brnoPath); pathErr != nil {
			t.Fatalf("set the path: %v", pathErr)
		}
		synctest.Wait()
		if got := len(binding.OfKind(fractal.KindRBR)); got != 1 {
			t.Errorf("the node sent %d RBR messages, want one", got)
		}
	})
}

// TestRegisterRefusesWhatItCannotUse checks that Register returns an error for
// an instance with no owner, with a name or an instance id that is not valid,
// and with a name and instance id the node already owns.
func TestRegisterRefusesWhatItCannotUse(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, printerPath)
		cases := []struct {
			name string
			inst node.Instance
			want error
		}{
			{"no owner", node.Instance{Name: ifaceName, Instance: "2"}, node.ErrNoOwner},
			{
				"a name outside its grammar",
				node.Instance{Owner: h.owner, Name: "not a name", Instance: "2"},
				nil,
			},
			{
				"an instance id outside its grammar",
				node.Instance{Owner: h.owner, Name: ifaceName, Instance: "not an id"},
				nil,
			},
			{
				"a pair the node already owns",
				node.Instance{Owner: h.owner, Name: ifaceName, Instance: ifaceInstance},
				node.ErrDuplicateInstance,
			},
		}
		for _, c := range cases {
			_, err := h.node.Register(c.inst)
			if err == nil {
				t.Fatalf("%s: Register accepted the instance, want an error", c.name)
			}
			if c.want != nil && !errors.Is(err, c.want) {
				t.Errorf("%s: Register returned %v, want %v", c.name, err, c.want)
			}
		}
		if got := len(h.node.Interfaces()); got != 1 {
			t.Errorf("the node owns %d interfaces, want one", got)
		}
	})
}

// TestTheInterfaceHandleNamesWhatItOwns checks that the handle from Register
// returns the name, the instance id and the Definition it was registered with.
func TestTheInterfaceHandleNamesWhatItOwns(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, printerPath)
		if h.iface.Name() != ifaceName || h.iface.Instance() != ifaceInstance {
			t.Errorf("the handle returns %s/%s, want %s/%s",
				h.iface.Name(), h.iface.Instance(), ifaceName, ifaceInstance)
		}
		if h.iface.Definition() == nil {
			t.Error("Definition returned nil, and the instance was registered with one")
		}
	})
}

// TestReportRefusesWhatTheDefinitionRefuses checks that Report returns an error
// and sends nothing when props has a property the Definition does not declare.
func TestReportRefusesWhatTheDefinitionRefuses(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, printerPath)
		err := h.iface.Report(t.Context(), fractal.Props{"chamberTemp": json.RawMessage(`1.0`)}, now)
		if err == nil {
			t.Fatal("Report accepted a property the definition does not declare, want an error")
		}
		if got := len(h.binding.Sent()); got != 0 {
			t.Errorf("the node sent %d messages, want none", got)
		}
	})
}

// TestARootReportsToNobody checks that Report on a root node sends nothing and
// returns nil, because a root node has no node above it.
func TestARootReportsToNobody(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, hqPath)
		if err := h.iface.Report(t.Context(), fractal.Props{tempProp: json.RawMessage(`1.0`)}, now); err != nil {
			t.Errorf("Report returned %v, want nil", err)
		}
		if got := len(h.binding.Sent()); got != 0 {
			t.Errorf("the node sent %d messages, want none", got)
		}
	})
}

// TestReportWithNoNodePathIsRefused checks that Report returns ErrNoNodePath
// while the node has no Node Path (fractal.md, Node Path).
func TestReportWithNoNodePathIsRefused(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		n, err := node.New(node.Config{Binding: &recordBinding{}})
		if err != nil {
			t.Fatalf("build the node: %v", err)
		}
		handle := registerOn(t, n, &testOwner{})
		err = handle.Report(t.Context(), fractal.Props{tempProp: json.RawMessage(`1.0`)}, now)
		if !errors.Is(err, node.ErrNoNodePath) {
			t.Errorf("Report returned %v, want %v", err, node.ErrNoNodePath)
		}
	})
}

// TestARequestedChangeReachesTheTransport checks that Desire sends one DES with
// the receiver and the props of the request.
func TestARequestedChangeReachesTheTransport(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, brnoPath)
		err := h.node.Desire(t.Context(), node.DesireRequest{
			Target:    printerPath,
			Interface: ifaceName,
			Instance:  ifaceInstance,
			Props:     fractal.Props{targetProp: json.RawMessage(`220.0`)},
		})
		if err != nil {
			t.Fatalf("ask for the change: %v", err)
		}
		messages := h.binding.OfKind(fractal.KindDES)
		if len(messages) != 1 {
			t.Fatalf("the node sent %d DES messages, want one", len(messages))
		}
		var m fractal.DES
		if unmarshalErr := json.Unmarshal(messages[0].dispatch.Payload, &m); unmarshalErr != nil {
			t.Fatalf("decode the DES: %v", unmarshalErr)
		}
		if m.Receiver != printerPath || string(m.Props[targetProp]) != "220.0" {
			t.Errorf("the DES is %+v, want %s = 220.0 for %s", m, targetProp, printerPath)
		}
	})
}

// TestARequestedChangeOutsideItsGrammarIsRefused checks that Desire and Call
// return an error and send nothing for an interface name that is not valid.
func TestARequestedChangeOutsideItsGrammarIsRefused(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, brnoPath)
		err := h.node.Desire(t.Context(), node.DesireRequest{
			Target:    printerPath,
			Interface: "not a name",
			Instance:  ifaceInstance,
			Props:     fractal.Props{targetProp: json.RawMessage(`1.0`)},
		})
		if err == nil {
			t.Fatal("Desire accepted an interface name that is not valid, want an error")
		}
		_, err = h.node.Call(t.Context(), node.CallRequest{
			Target:    printerPath,
			Interface: "not a name",
			Instance:  ifaceInstance,
			Method:    setTemp,
			Expires:   now + 1000,
		})
		if err == nil {
			t.Fatal("Call accepted an interface name that is not valid, want an error")
		}
		if got := len(h.binding.Sent()); got != 0 {
			t.Errorf("the node sent %d messages, want none", got)
		}
	})
}

// TestATransportThatRefusesIsCounted checks a binding whose every send fails.
// Deliver of a REP does not panic, and Desire and Call return the binding's
// error.
func TestATransportThatRefusesIsCounted(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, brnoPath)
		h.binding.Fail(errBroken)
		payload := encode(t, report(now, fractal.Props{tempProp: json.RawMessage(`1.0`)}))
		h.node.Deliver(t.Context(), node.Inbound{Kind: fractal.KindREP, Payload: payload, From: printerID})
		synctest.Wait()

		err := h.node.Desire(t.Context(), node.DesireRequest{
			Target:    printerPath,
			Interface: ifaceName,
			Instance:  ifaceInstance,
			Props:     fractal.Props{targetProp: json.RawMessage(`1.0`)},
		})
		if !errors.Is(err, errBroken) {
			t.Errorf("Desire returned %v, want the binding's error", err)
		}
		_, err = h.node.Call(t.Context(), request(printerPath, now+1000))
		if !errors.Is(err, errBroken) {
			t.Errorf("Call returned %v, want the binding's error", err)
		}
	})
}

// TestAFullQueueLeavesTheMessageForLater checks that a node whose queue is
// full drops a message and does not call Ack, so the transport delivers the
// message again.
func TestAFullQueueLeavesTheMessageForLater(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		kind    fractal.Kind
		payload func(t *testing.T) []byte
	}{
		{
			"a method invocation",
			fractal.KindRPC,
			func(t *testing.T) []byte { return encode(t, invocation(printerPath)) },
		},
		{
			"a requested property change",
			fractal.KindDES,
			func(t *testing.T) []byte {
				return encode(t, desire(printerPath, fractal.Props{targetProp: json.RawMessage(`1.0`)}))
			},
		},
		{
			"a request for a full report",
			fractal.KindRBR,
			func(t *testing.T) []byte { return encode(t, fractal.RBR{Version: fractal.Version}) },
		},
		{
			"an answer",
			fractal.KindRES,
			func(t *testing.T) []byte {
				return encode(t, fractal.RES{Version: fractal.Version, ID: "a", Receiver: printerPath})
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				// The test never starts the node, so no goroutine reads the
				// queues. Each queue has room for one message, and the second
				// Deliver finds it full.
				n, err := node.New(node.Config{
					Binding:    &recordBinding{},
					Admissions: children{printerID},
					Path:       printerPath,
					WorkQueue:  1,
					RPCQueue:   1,
				})
				if err != nil {
					t.Fatalf("build the node: %v", err)
				}
				a := &acker{}
				in := node.Inbound{Kind: c.kind, Payload: c.payload(t), Ack: a.ack}
				if c.kind == fractal.KindRES {
					in.From = printerID
				}
				n.Deliver(t.Context(), in)
				n.Deliver(t.Context(), in)

				if got := a.Count(); got != 0 {
					t.Errorf("the node called Ack %d times, want none: "+
						"the transport must deliver the message again", got)
				}
			})
		})
	}
}

// TestADedupStoreThatFailsStopsTheWork checks a node whose ExecutedStore and
// ProcessedStore return errors. The node does not run the method and does not
// call Ack for the RPC or the RES, so the transport delivers both again.
func TestADedupStoreThatFailsStopsTheWork(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		binding := &recordBinding{}
		owner := &testOwner{}
		n, err := node.New(node.Config{
			Binding:   binding,
			Executed:  failingDedup{},
			Processed: failingDedup{},
			Path:      printerPath,
		})
		if err != nil {
			t.Fatalf("build the node: %v", err)
		}
		registerOn(t, n, owner)
		if startErr := n.Start(t.Context()); startErr != nil {
			t.Fatalf("start the node: %v", startErr)
		}
		t.Cleanup(func() { _ = n.Close() })

		a := &acker{}
		n.Deliver(t.Context(), node.Inbound{
			Kind:    fractal.KindRPC,
			Payload: encode(t, invocation(printerPath)),
			Ack:     a.ack,
		})
		n.Deliver(t.Context(), node.Inbound{
			Kind:    fractal.KindRES,
			Payload: encode(t, fractal.RES{Version: fractal.Version, ID: "a", Receiver: printerPath}),
			From:    printerID,
			Ack:     a.ack,
		})
		synctest.Wait()

		if got := a.Count(); got != 0 {
			t.Errorf("the node called Ack %d times, want none", got)
		}
		if got := owner.Runs(); got != 0 {
			t.Errorf("the method ran %d times, want none", got)
		}
	})
}

// TestAMalformedMessageOfEveryTypeIsDropped checks that the node sends nothing
// for a message that is not JSON, has an unsupported version, or has a field
// that is not valid, for each message type (fractal.md, Protocol Version).
func TestAMalformedMessageOfEveryTypeIsDropped(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   func(t *testing.T) node.Inbound
	}{
		{"a loss message that is not json", func(*testing.T) node.Inbound {
			return node.Inbound{Kind: fractal.KindLOS, Payload: []byte("{"), From: printerID}
		}},
		{"a loss message of another version", func(t *testing.T) node.Inbound {
			m := fractal.LOS{Version: fractal.Version, Sender: printerPath, Child: "extruder-1"}
			return node.Inbound{Kind: fractal.KindLOS, Payload: wrongVersion(t, m), From: printerID}
		}},
		{"a loss message naming no valid node", func(t *testing.T) node.Inbound {
			m := fractal.LOS{Version: fractal.Version, Sender: printerPath, Child: "-bad"}
			return node.Inbound{Kind: fractal.KindLOS, Payload: encode(t, m), From: printerID}
		}},
		{"a requested change that is not json", func(*testing.T) node.Inbound {
			return node.Inbound{Kind: fractal.KindDES, Payload: []byte("{")}
		}},
		{"a requested change of another version", func(t *testing.T) node.Inbound {
			m := desire(printerPath, fractal.Props{targetProp: json.RawMessage(`1.0`)})
			return node.Inbound{Kind: fractal.KindDES, Payload: wrongVersion(t, m)}
		}},
		{"a requested change naming no valid node", func(t *testing.T) node.Inbound {
			m := desire(printerPath, fractal.Props{targetProp: json.RawMessage(`1.0`)})
			m.Receiver = "hq//brno"
			return node.Inbound{Kind: fractal.KindDES, Payload: encode(t, m)}
		}},
		{"a method invocation that is not json", func(*testing.T) node.Inbound {
			return node.Inbound{Kind: fractal.KindRPC, Payload: []byte("{")}
		}},
		{"a method invocation naming no valid method", func(t *testing.T) node.Inbound {
			m := invocation(printerPath)
			m.Method = "not a name"
			return node.Inbound{Kind: fractal.KindRPC, Payload: encode(t, m)}
		}},
		{"a request for a full report that is not json", func(*testing.T) node.Inbound {
			return node.Inbound{Kind: fractal.KindRBR, Payload: []byte("{")}
		}},
		{"a request for a full report of another version", func(t *testing.T) node.Inbound {
			return node.Inbound{Kind: fractal.KindRBR, Payload: wrongVersion(t, fractal.RBR{Version: fractal.Version})}
		}},
		{"an answer that is not json", func(*testing.T) node.Inbound {
			return node.Inbound{Kind: fractal.KindRES, Payload: []byte("{"), From: printerID}
		}},
		{"an answer of another version", func(t *testing.T) node.Inbound {
			m := fractal.RES{Version: fractal.Version, ID: "a", Receiver: hqPath}
			return node.Inbound{Kind: fractal.KindRES, Payload: wrongVersion(t, m), From: printerID}
		}},
		{"an answer naming no valid node", func(t *testing.T) node.Inbound {
			m := fractal.RES{Version: fractal.Version, ID: "a", Receiver: "hq//brno"}
			return node.Inbound{Kind: fractal.KindRES, Payload: encode(t, m), From: printerID}
		}},
		{"a message on a link with no valid node id", func(t *testing.T) node.Inbound {
			m := report(now, fractal.Props{tempProp: json.RawMessage(`1.0`)})
			return node.Inbound{Kind: fractal.KindREP, Payload: encode(t, m), From: "-bad"}
		}},
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
			})
		})
	}
}

// TestTheMemoryDedupForgetsWhatThePastWindowHolds checks that a MemoryDedup
// does not return a record older than its window, removes the oldest id when
// it has more than maxEntries, and is empty after Reset.
func TestTheMemoryDedupForgetsWhatThePastWindowHolds(t *testing.T) {
	t.Parallel()
	clock := &fixedClock{at: now}
	store := node.NewMemoryDedup(clock, time.Minute, 2)
	ctx := t.Context()
	if err := store.RecordExecuted(ctx, "a", []byte(`{"id":"a"}`)); err != nil {
		t.Fatalf("record: %v", err)
	}
	res, ok, err := store.Executed(ctx, "a")
	if err != nil || !ok || string(res) != `{"id":"a"}` {
		t.Errorf("Executed returned %s, %v, %v, want the stored answer", res, ok, err)
	}

	clock.Set(now + 61_000)
	if _, found, _ := store.Executed(ctx, "a"); found {
		t.Error("Executed returned an answer that is older than the window")
	}

	clock.Set(now)
	for _, id := range []fractal.RPCID{"a", "b", "c"} {
		if recordProcessedErr := store.RecordProcessed(ctx, id); recordProcessedErr != nil {
			t.Fatalf("record %s: %v", id, recordProcessedErr)
		}
	}
	if _, processed := store.Len(); processed != 2 {
		t.Errorf("the store has %d processed ids, want its limit of two", processed)
	}
	if seen, _ := store.Processed(ctx, "a"); seen {
		t.Error("the store kept the oldest id after it went over its limit")
	}
	if seen, _ := store.Processed(ctx, "c"); !seen {
		t.Error("the store removed the newest id, want it kept")
	}
	store.Reset()
	if executed, processed := store.Len(); executed != 0 || processed != 0 {
		t.Errorf("the store has %d executed and %d processed records after Reset, want none", executed, processed)
	}
}

// TestTheMemoryDedupTakesItsDefaults checks that NewMemoryDedup with a nil
// clock, a zero window and a zero limit builds a store that works.
func TestTheMemoryDedupTakesItsDefaults(t *testing.T) {
	t.Parallel()
	store := node.NewMemoryDedup(nil, 0, 0)
	if err := store.RecordProcessed(t.Context(), "a"); err != nil {
		t.Fatalf("record: %v", err)
	}
	seen, err := store.Processed(t.Context(), "a")
	if err != nil || !seen {
		t.Errorf("Processed returned %v, %v, want true and no error", seen, err)
	}
}

// TestTheCountersNameWhatTheyCount checks that every Outcome and every
// DropReason has a name, and that a value outside the list is "unknown".
func TestTheCountersNameWhatTheyCount(t *testing.T) {
	t.Parallel()
	for _, o := range node.Outcomes() {
		if o.String() == "unknown" {
			t.Errorf("the outcome %d has no name", o)
		}
	}
	if got := node.Outcome(0).String(); got != "unknown" {
		t.Errorf("an outcome outside the table is %q, want unknown", got)
	}
	for _, r := range node.DropReasons() {
		if r.String() == "unknown" {
			t.Errorf("the reason %d has no name", r)
		}
	}
	if got := node.DropReason(0).String(); got != "unknown" {
		t.Errorf("a reason outside the table is %q, want unknown", got)
	}
}

// TestANodeWithNoChildrenHasNone checks that NoChildren admits no Node ID and
// lists none.
func TestANodeWithNoChildrenHasNone(t *testing.T) {
	t.Parallel()
	var leaf node.Admissions = node.NoChildren{}
	if leaf.Admitted(printerID) {
		t.Error("NoChildren admitted a Node ID")
	}
	for range leaf.Children() {
		t.Error("NoChildren listed a Node ID")
	}
}

// TestADispatchKnowsWhichWayItGoes checks Dispatch.Down and Inbound.FromChild:
// a Dispatch with a Child goes down, and an Inbound with a From came from
// below.
func TestADispatchKnowsWhichWayItGoes(t *testing.T) {
	t.Parallel()
	if !(node.Dispatch{Child: printerID}).Down() {
		t.Error("Down is false for a Dispatch with a Child")
	}
	if (node.Dispatch{}).Down() {
		t.Error("Down is true for a Dispatch with no Child")
	}
	if !(node.Inbound{From: printerID}).FromChild() {
		t.Error("FromChild is false for an Inbound with a From")
	}
	if (node.Inbound{}).FromChild() {
		t.Error("FromChild is true for an Inbound with no From")
	}
}

// TestTheSystemClockReadsTheMachine checks that SystemClock.After and
// SystemClock.Now agree: after After(time.Second), Now is one second later.
func TestTheSystemClockReadsTheMachine(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		clock := node.SystemClock{}
		start := clock.Now()
		<-clock.After(time.Second)
		if got := clock.Now().Sub(start); got != time.Second {
			t.Errorf("Now moved by %v, want one second", got)
		}
	})
}
