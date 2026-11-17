package fractaltest

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	"github.com/prusa3d/prusa-connect/sdk/fractal/node"
)

// suiteTimeout is how long a case waits for a message or a signal. suitePoll
// is how often it checks.
const (
	suiteTimeout = 5 * time.Second
	suitePoll    = time.Millisecond
	// settleWindow is how long a case keeps checking a message count that must
	// not change.
	settleWindow = 200 * time.Millisecond
	// twoDeliveries is the message count a case waits for when it expects the
	// transport to deliver one message a second time.
	twoDeliveries = 2
	// twoConnections is the ParentConnected count a case waits for after the
	// link comes up a second time.
	twoConnections = 2
)

// farFuture is an Expires timestamp far after any test run, so no method
// invocation the suite sends expires.
const farFuture = fractal.Timestamp(1) << 62

// TestLink is one link of the transport under test. A link connects two
// nodes: the node above and the node below it. The conformance suite sends
// messages from both ends, cuts the link and restores it. A transport package
// implements TestLink with its own connection code.
type TestLink interface {
	// ParentSide returns the binding that the node above sends through.
	ParentSide() node.Binding
	// ChildSide returns the binding that the node below sends through.
	ChildSide() node.Binding
	// Connect brings the link up for the first time.
	Connect(ctx context.Context)
	// Cut takes the link down.
	Cut(ctx context.Context)
	// Restore brings the link up again after Cut.
	Restore(ctx context.Context)
	// Close stops the link and frees its goroutines and connections.
	Close()
}

// LinkFactory builds one link for one case of the conformance suite.
//
// cfg names the engine at each end. The link delivers to cfg.Above every
// message the node below sends, and to cfg.Below every message the node above
// sends. The factory also starts whatever the transport needs to run, such as
// goroutines or a server. It returns the link not connected. Each case calls
// Connect itself.
type LinkFactory func(t *testing.T, cfg LinkConfig) TestLink

// RunBindingSuite runs every case a transport must pass. Each case gets a new
// link from factory. The cases use only the node.Binding interface of
// sdk/fractal/node, so the in-memory transport of this package and a transport
// over a real network run the same cases.
//
// A conformance table refers to a case by its name, for a rule that only a
// transport can prove. Rename a case and the table rows that name it together.
func RunBindingSuite(t *testing.T, factory LinkFactory) {
	t.Helper()
	cases := []struct {
		name string
		run  func(t *testing.T, h *harness)
	}{
		{"best effort goes down and up", bestEffortBothWays},
		{"best effort is dropped while the link is down", bestEffortDropped},
		{"durable waits while the link is down", durableWaits},
		{"durable arrives in order", durableInOrder},
		{"durable is delivered once when acknowledged", durableDeliveredOnce},
		{"durable is delivered again when not acknowledged", durableRedelivered},
		{"durable of a repeated id is delivered once", durableDeduplicated},
		{"a message from below names the link it arrived on", inboundNamesTheChild},
		{"a message from above names no link", inboundFromAboveNamesNoChild},
		{"a connect signal reaches the node above", connectSignal},
		{"a loss signal reaches the node above", lossSignal},
		{"a child that never connected produces no loss", noLossBeforeFirstConnect},
		{"the parent link signals reach the node below", parentSignals},
		{"a send to a node the transport does not hold says so", noSuchLink},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t, factory, c.name)
			defer h.link.Close()
			c.run(t, h)
		})
	}
}

// harness is the link of one case, with a Recorder as the engine at each end.
// child is the Node ID of the node below, and path is the Node Path of the
// node above.
type harness struct {
	link  TestLink
	above *Recorder
	below *Recorder
	// ctx is the context of the one test the harness serves.
	ctx   context.Context
	child fractal.NodeID
	path  fractal.NodePath
}

// newHarness builds the link one case runs on.
func newHarness(t *testing.T, factory LinkFactory, name string) *harness {
	t.Helper()
	above, below := NewRecorder(true), NewRecorder(true)
	h := &harness{
		above: above,
		below: below,
		ctx:   t.Context(),
		child: "printer-1",
		path:  "hq",
	}
	h.link = factory(t, LinkConfig{
		Above:  above,
		Below:  below,
		Parent: h.path,
		Child:  h.child,
	})
	if h.link == nil {
		t.Fatalf("%s: the LinkFactory returned a nil link", name)
	}
	return h
}

// down sends one message from the node above to the node below.
func (h *harness) down(t *testing.T, k fractal.Kind, payload []byte, id fractal.RPCID) {
	t.Helper()
	d := node.Dispatch{Kind: k, Payload: payload, Child: h.child, ID: id}
	h.sendThrough(t, h.link.ParentSide(), d)
}

// up sends one message from the node below to the node above.
func (h *harness) up(t *testing.T, k fractal.Kind, payload []byte, id fractal.RPCID) {
	t.Helper()
	h.sendThrough(t, h.link.ChildSide(), node.Dispatch{Kind: k, Payload: payload, ID: id})
}

// sendThrough sends d with the delivery its message type requires: SendDurable
// for a durable type and SendBestEffort for every other type.
func (h *harness) sendThrough(t *testing.T, b node.Binding, d node.Dispatch) {
	t.Helper()
	var err error
	if d.Kind.Delivery() == fractal.Durable {
		err = b.SendDurable(h.ctx, d)
	} else {
		err = b.SendBestEffort(h.ctx, d)
	}
	if err != nil {
		t.Fatalf("the binding refused to send a %s message: %v", d.Kind, err)
	}
}

// bestEffortBothWays checks that a best-effort message reaches the other end
// in each direction while the link is up.
func bestEffortBothWays(t *testing.T, h *harness) {
	h.link.Connect(h.ctx)
	h.down(t, fractal.KindDES, desPayload("hq/printer-1"), "")
	waitFor(t, h.below, 1)
	h.up(t, fractal.KindREP, repPayload("hq/printer-1", 1), "")
	waitFor(t, h.above, 1)
	if got := h.below.Kinds()[0]; got != fractal.KindDES {
		t.Errorf("the node below received a %s message, want DES", got)
	}
	if got := h.above.Kinds()[0]; got != fractal.KindREP {
		t.Errorf("the node above received a %s message, want REP", got)
	}
}

// bestEffortDropped checks that the transport drops a best-effort message sent
// while the link is down, and does not deliver it after the link returns.
func bestEffortDropped(t *testing.T, h *harness) {
	h.link.Connect(h.ctx)
	h.link.Cut(h.ctx)
	h.down(t, fractal.KindDES, desPayload("hq/printer-1"), "")
	h.link.Restore(h.ctx)
	stayAt(t, h.below, 0)
}

// durableWaits checks that the transport keeps a durable message sent while
// the link is down, and delivers it when the link returns.
func durableWaits(t *testing.T, h *harness) {
	h.link.Connect(h.ctx)
	h.link.Cut(h.ctx)
	h.down(t, fractal.KindRPC, rpcPayload("a"), "a")
	stayAt(t, h.below, 0)
	h.link.Restore(h.ctx)
	waitFor(t, h.below, 1)
}

// durableInOrder checks that durable messages for one node arrive in the order
// they were sent.
func durableInOrder(t *testing.T, h *harness) {
	h.link.Connect(h.ctx)
	h.link.Cut(h.ctx)
	ids := []fractal.RPCID{"a", "b", "c"}
	for _, id := range ids {
		h.down(t, fractal.KindRPC, rpcPayload(id), id)
	}
	h.link.Restore(h.ctx)
	waitFor(t, h.below, len(ids))
	for i, got := range h.below.Messages() {
		want := rpcPayload(ids[i])
		if string(got.Payload) != string(want) {
			t.Errorf("durable message %d arrived with payload %s, want %s: "+
				"durable messages must arrive in the order they were sent", i, got.Payload, want)
		}
	}
}

// durableDeliveredOnce checks that the transport does not deliver an
// acknowledged durable message again after the link is cut and restored.
func durableDeliveredOnce(t *testing.T, h *harness) {
	h.link.Connect(h.ctx)
	h.down(t, fractal.KindRPC, rpcPayload("a"), "a")
	waitFor(t, h.below, 1)
	h.link.Cut(h.ctx)
	h.link.Restore(h.ctx)
	stayAt(t, h.below, 1)
}

// durableRedelivered checks that the transport delivers a durable message
// again after the link returns, when the receiver did not acknowledge the
// first delivery. The case models a node that received a method invocation and
// stopped before it acknowledged the message.
func durableRedelivered(t *testing.T, h *harness) {
	h.below.SetAutoAck(false)
	h.link.Connect(h.ctx)
	h.down(t, fractal.KindRPC, rpcPayload("a"), "a")
	waitFor(t, h.below, 1)
	h.link.Cut(h.ctx)
	h.link.Restore(h.ctx)
	waitFor(t, h.below, twoDeliveries)
	messages := h.below.Messages()
	if string(messages[0].Payload) != string(messages[1].Payload) {
		t.Errorf("the second delivery has payload %s, want the payload of the first delivery, %s",
			messages[1].Payload, messages[0].Payload)
	}
}

// durableDeduplicated checks that the transport delivers one durable message
// when the sender sends the same id twice inside the deduplication window.
func durableDeduplicated(t *testing.T, h *harness) {
	h.link.Connect(h.ctx)
	payload := rpcPayload("a")
	h.down(t, fractal.KindRPC, payload, "a")
	waitFor(t, h.below, 1)
	h.down(t, fractal.KindRPC, payload, "a")
	stayAt(t, h.below, 1)
}

// inboundNamesTheChild checks that a message from the node below has, in
// Inbound.From, the Node ID of the link it arrived on. The engine compares
// From with the sender of the message (fractal.md, Sender Validation).
func inboundNamesTheChild(t *testing.T, h *harness) {
	h.link.Connect(h.ctx)
	h.up(t, fractal.KindREP, repPayload("hq/printer-1", 1), "")
	waitFor(t, h.above, 1)
	if got := h.above.Messages()[0].From; got != h.child {
		t.Errorf("Inbound.From is %q on a message from the node below, want %q, "+
			"the Node ID of the link it arrived on", got, h.child)
	}
}

// inboundFromAboveNamesNoChild checks that a message from the node above has
// an empty Inbound.From.
func inboundFromAboveNamesNoChild(t *testing.T, h *harness) {
	h.link.Connect(h.ctx)
	h.down(t, fractal.KindDES, desPayload("hq/printer-1"), "")
	waitFor(t, h.below, 1)
	if got := h.below.Messages()[0].From; got != "" {
		t.Errorf("Inbound.From is %q on a message from the node above, want it empty", got)
	}
}

// connectSignal checks that the transport calls ChildConnected on the engine
// of the node above when the link comes up.
func connectSignal(t *testing.T, h *harness) {
	h.link.Connect(h.ctx)
	waitUntil(t, func() bool { return len(h.above.Connected()) == 1 }, connectCall)
	if got := h.above.Connected()[0]; got != h.child {
		t.Errorf("the transport called ChildConnected with %q, want %q", got, h.child)
	}
}

// lossSignal checks that the transport calls ChildLost on the engine of the
// node above when the link goes down.
func lossSignal(t *testing.T, h *harness) {
	h.link.Connect(h.ctx)
	waitUntil(t, func() bool { return len(h.above.Connected()) == 1 }, connectCall)
	h.link.Cut(h.ctx)
	waitUntil(t, func() bool { return len(h.above.Lost()) == 1 }, "one ChildLost call on the node above")
	if got := h.above.Lost()[0]; got != h.child {
		t.Errorf("the transport called ChildLost with %q, want %q", got, h.child)
	}
}

// noLossBeforeFirstConnect checks that the transport calls neither ChildLost
// nor ParentLost when a link that never connected is cut (fractal-nats.md,
// Where liveness monitoring starts).
func noLossBeforeFirstConnect(t *testing.T, h *harness) {
	h.link.Cut(h.ctx)
	if got := len(h.above.Lost()); got != 0 {
		t.Errorf("the transport called ChildLost %d times for a link that never connected, want 0", got)
	}
	if got := h.below.ParentLosses(); got != 0 {
		t.Errorf("the transport called ParentLost %d times for a link that never connected, want 0", got)
	}
}

// parentSignals checks that the transport calls ParentConnected on the engine
// of the node below each time the link comes up, and ParentLost when it goes
// down. The engine below sends its ARBR and marks its ancestor shadow stale on
// them (fractal-nats.md, Connectivity Detection).
func parentSignals(t *testing.T, h *harness) {
	connections := func(want int) func() bool {
		return func() bool { return h.below.ParentConnections() == want }
	}
	h.link.Connect(h.ctx)
	waitUntil(t, connections(1), "one ParentConnected call on the node below")
	h.link.Cut(h.ctx)
	waitUntil(t, func() bool { return h.below.ParentLosses() == 1 }, "one ParentLost call on the node below")
	h.link.Restore(h.ctx)
	waitUntil(t, connections(twoConnections), "a second ParentConnected call on the node below")
}

// noSuchLink checks that SendBestEffort and SendDurable return
// node.ErrNoSuchLink when the transport has no link to the node that
// Dispatch.Child names. The engine uses that error to tell a message it cannot
// route from a transport failure. It answers such a method invocation with
// fractal.CodeNodeNotFound.
func noSuchLink(t *testing.T, h *harness) {
	h.link.Connect(h.ctx)
	down := h.link.ParentSide()
	stranger := node.Dispatch{Kind: fractal.KindDES, Payload: []byte(`{}`), Child: "printer-9"}
	if err := down.SendBestEffort(h.ctx, stranger); !errors.Is(err, node.ErrNoSuchLink) {
		t.Errorf("SendBestEffort to a node the transport has no link to returned %v, "+
			"want an error that wraps node.ErrNoSuchLink", err)
	}
	call := node.Dispatch{Kind: fractal.KindRPC, Payload: []byte(`{}`), Child: "printer-9", ID: "a"}
	if err := down.SendDurable(h.ctx, call); !errors.Is(err, node.ErrNoSuchLink) {
		t.Errorf("SendDurable to a node the transport has no link to returned %v, "+
			"want an error that wraps node.ErrNoSuchLink", err)
	}
}

// connectCall names the signal a case waits for after Connect.
const connectCall = "one ChildConnected call on the node above"

// waitFor waits until the recorder has want messages, and fails when it has
// more.
func waitFor(t *testing.T, r *Recorder, want int) {
	t.Helper()
	waitUntil(t, func() bool { return r.Count() >= want }, "the expected messages to arrive")
	if got := r.Count(); got != want {
		t.Fatalf("the transport delivered %d messages, want %d", got, want)
	}
}

// stayAt checks the message count of the recorder again and again for
// settleWindow. One check cannot show that no further message arrives.
func stayAt(t *testing.T, r *Recorder, want int) {
	t.Helper()
	deadline := time.Now().Add(settleWindow)
	for time.Now().Before(deadline) {
		if got := r.Count(); got != want {
			t.Fatalf("the transport delivered %d messages, want the count to stay at %d", got, want)
		}
		time.Sleep(suitePoll)
	}
}

// waitUntil waits until done returns true. It fails after suiteTimeout and
// names, with what, the event that did not happen.
func waitUntil(t *testing.T, done func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(suiteTimeout)
	for time.Now().Before(deadline) {
		if done() {
			return
		}
		time.Sleep(suitePoll)
	}
	t.Fatalf("waited %s for %s, and it did not happen", suiteTimeout, what)
}

// desPayload builds a requested property change for one receiver.
func desPayload(receiver fractal.NodePath) []byte {
	return mustEncode(fractal.DES{
		Version:   fractal.Version,
		Receiver:  receiver,
		Interface: "FDMPrinter1",
		Instance:  "1",
		Props:     fractal.Props{"nozzleTempTarget": json.RawMessage(`220`)},
	})
}

// repPayload builds a state report for one sender.
func repPayload(sender fractal.NodePath, at fractal.Timestamp) []byte {
	return mustEncode(fractal.REP{
		Version:   fractal.Version,
		Sender:    sender,
		Interface: "FDMPrinter1",
		Instance:  "1",
		Timestamp: at,
		Props:     fractal.Props{"nozzleTemp": json.RawMessage(`25`)},
	})
}

// rpcPayload builds a method invocation with one id.
func rpcPayload(id fractal.RPCID) []byte {
	return mustEncode(fractal.RPC{
		Version:   fractal.Version,
		ID:        id,
		Sender:    "hq",
		Receiver:  "hq/printer-1",
		Interface: "FDMPrinter1",
		Instance:  "1",
		Method:    "SetNozzleTemp",
		Expires:   farFuture,
	})
}

// mustEncode encodes a message the suite built itself, so an error here is a
// fault in this file.
func mustEncode(m any) []byte {
	payload, err := json.Marshal(m)
	if err != nil {
		panic(err)
	}
	return payload
}
