package natsbind_test

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/prusa3d/fractal-link/natsbind"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	fractalnode "github.com/prusa3d/prusa-connect/sdk/fractal/node"
)

// tree has the nodes of the examples in fractal.md: the root node hq, brno
// which joins hq, and printer-1 which joins brno. toBrno and toPrint are the
// relays of the two leafnode connections.
type tree struct {
	hq      *node
	brno    *node
	printer *node
	toBrno  *proxy
	toPrint *proxy
}

// treeSetup is the configuration of hq, brno and printer-1, and the two open
// relays between them.
type treeSetup struct {
	hq, brno, printer natsbind.Config
	toBrno, toPrint   *proxy
}

// setUpTree configures hq, brno which joins hq through toBrno, and printer-1
// which joins brno through toPrint. Each function in tune changes the
// configuration of all three.
func setUpTree(t testing.TB, tune ...func(*natsbind.Config)) treeSetup {
	t.Helper()
	hq, hqLeaf := baseConfig(t, nodeOptions{id: "hq", accepts: true})
	toBrno := newProxy(t, fmt.Sprintf("127.0.0.1:%d", hqLeaf))
	hq.AdvertisedLeafRemote = "nats://" + toBrno.Addr()
	toBrno.Open()

	brno, brnoLeaf := baseConfig(t, nodeOptions{id: "brno", accepts: true, parent: hq.ClientListen})
	toPrint := newProxy(t, fmt.Sprintf("127.0.0.1:%d", brnoLeaf))
	brno.AdvertisedLeafRemote = "nats://" + toPrint.Addr()
	toPrint.Open()

	printer, _ := baseConfig(t, nodeOptions{id: "printer-1", parent: brno.ClientListen})
	for _, cfg := range []*natsbind.Config{&hq, &brno, &printer} {
		for _, change := range tune {
			change(cfg)
		}
	}
	return treeSetup{hq: hq, brno: brno, printer: printer, toBrno: toBrno, toPrint: toPrint}
}

// newTree starts hq, brno and printer-1 and waits until hq sees brno and brno
// sees printer-1 as connected.
func newTree(ctx context.Context, t testing.TB, tune ...func(*natsbind.Config)) *tree {
	t.Helper()
	s := setUpTree(t, tune...)
	hq := startNode(ctx, t, s.hq)
	brno := startNode(ctx, t, s.brno)
	printer := startNode(ctx, t, s.printer)

	built := &tree{hq: hq, brno: brno, printer: printer, toBrno: s.toBrno, toPrint: s.toPrint}
	waitUntil(t, "hq to see brno and brno to see printer-1 as connected", func() bool {
		return len(hq.engine.Connected()) >= 1 && len(brno.engine.Connected()) >= 1
	})
	return built
}

// TestTheTreeCarriesTheExamplesOfTheSpecification sends the messages of the
// examples in fractal.md between real NATS servers, one hop at a time. The
// engine of each node is a recorder that forwards nothing, so the test sends
// every hop itself and compares the bytes that arrive with the bytes it sent.
func TestTheTreeCarriesTheExamplesOfTheSpecification(t *testing.T) {
	ctx := t.Context()
	tr := newTree(ctx, t)

	t.Run("example 1, startup", func(t *testing.T) {
		// hq sends brno a request for a full state report, brno sends it on to
		// printer-1, and printer-1 answers with a state report.
		send(t, tr.hq, fractalnode.Dispatch{
			Kind: fractal.KindRBR, Payload: rbrPayload(t), Child: "brno",
		})
		awaitKind(t, tr.brno, fractal.KindRBR, 1)
		send(t, tr.brno, fractalnode.Dispatch{
			Kind: fractal.KindRBR, Payload: rbrPayload(t), Child: "printer-1",
		})
		awaitKind(t, tr.printer, fractal.KindRBR, 1)

		report := repPayload(t, "hq/brno/printer-1", 1751400000000)
		send(t, tr.printer, fractalnode.Dispatch{Kind: fractal.KindREP, Payload: report})
		awaitKind(t, tr.brno, fractal.KindREP, 1)
		send(t, tr.brno, fractalnode.Dispatch{Kind: fractal.KindREP, Payload: report})
		awaitKind(t, tr.hq, fractal.KindREP, 1)
		if got := lastOf(tr.hq, fractal.KindREP); string(got) != string(report) {
			t.Errorf("hq received the state report %s, want the bytes printer-1 sent", got)
		}
	})

	t.Run("example 2, a state change", func(t *testing.T) {
		report := repPayload(t, "hq/brno/printer-1", 1751400010000)
		send(t, tr.printer, fractalnode.Dispatch{Kind: fractal.KindREP, Payload: report})
		awaitKind(t, tr.brno, fractal.KindREP, 2)
	})

	t.Run("example 3, a requested property change", func(t *testing.T) {
		change := desPayload(t, "hq/brno/printer-1")
		send(t, tr.hq, fractalnode.Dispatch{Kind: fractal.KindDES, Payload: change, Child: "brno"})
		awaitKind(t, tr.brno, fractal.KindDES, 1)
		send(t, tr.brno, fractalnode.Dispatch{Kind: fractal.KindDES, Payload: change, Child: "printer-1"})
		awaitKind(t, tr.printer, fractal.KindDES, 1)
		if got := lastOf(tr.printer, fractal.KindDES); string(got) != string(change) {
			t.Errorf("printer-1 received the property change %s, want the bytes hq sent", got)
		}
	})

	t.Run("example 4, a method invocation and its result", func(t *testing.T) {
		call := rpcPayload(t, "dGVzdA")
		send(t, tr.hq, fractalnode.Dispatch{
			Kind: fractal.KindRPC, Payload: call, Child: "brno", ID: "dGVzdA",
		})
		awaitKind(t, tr.brno, fractal.KindRPC, 1)
		send(t, tr.brno, fractalnode.Dispatch{
			Kind: fractal.KindRPC, Payload: call, Child: "printer-1", ID: "dGVzdA",
		})
		awaitKind(t, tr.printer, fractal.KindRPC, 1)

		answer := resPayload(t, "dGVzdA")
		send(t, tr.printer, fractalnode.Dispatch{Kind: fractal.KindRES, Payload: answer, ID: "dGVzdA"})
		awaitKind(t, tr.brno, fractal.KindRES, 1)
		send(t, tr.brno, fractalnode.Dispatch{Kind: fractal.KindRES, Payload: answer, ID: "dGVzdA"})
		awaitKind(t, tr.hq, fractal.KindRES, 1)
		if got := lastOf(tr.hq, fractal.KindRES); string(got) != string(answer) {
			t.Errorf("hq received the result %s, want the bytes printer-1 sent", got)
		}
	})

	t.Run("example 5, a connection is lost", func(t *testing.T) {
		tr.toPrint.Cut()
		waitUntil(t, "brno to see printer-1 as lost", func() bool {
			return len(tr.brno.engine.Lost()) == 1
		})
		if got := tr.brno.engine.Lost()[0]; got != "printer-1" {
			t.Errorf("brno called ChildLost for %q, want printer-1", got)
		}
		loss := losPayload(t, "hq/brno", "printer-1")
		send(t, tr.brno, fractalnode.Dispatch{Kind: fractal.KindLOS, Payload: loss})
		awaitKind(t, tr.hq, fractal.KindLOS, 1)
	})

	t.Run("example 6, the connection comes back", func(t *testing.T) {
		before := len(tr.brno.engine.Connected())
		tr.toPrint.Open()
		waitUntil(t, "brno to see printer-1 as connected again", func() bool {
			return len(tr.brno.engine.Connected()) > before
		})
		report := repPayload(t, "hq/brno/printer-1", 1751400120000)
		send(t, tr.printer, fractalnode.Dispatch{Kind: fractal.KindREP, Payload: report})
		awaitKind(t, tr.brno, fractal.KindREP, 3)
	})
}

// TestACutLinkKeepsMethodInvocationsInOrder disconnects printer-1 and sends
// method invocations for it from brno. They wait in the stream of brno, and
// after printer-1 connects again they arrive in the order brno sent them.
func TestACutLinkKeepsMethodInvocationsInOrder(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	tr := newTree(ctx, t)
	tr.toPrint.Cut()
	waitUntil(t, "brno to see printer-1 as lost", func() bool { return len(tr.brno.engine.Lost()) == 1 })

	ids := []fractal.RPCID{"one", "two", "three"}
	for _, id := range ids {
		send(t, tr.brno, fractalnode.Dispatch{
			Kind: fractal.KindRPC, Payload: rpcPayload(t, id), Child: "printer-1", ID: id,
		})
	}
	if got := countKind(tr.printer, fractal.KindRPC); got != 0 {
		t.Fatalf("%d method invocations arrived while printer-1 was disconnected, want 0", got)
	}
	tr.toPrint.Open()
	awaitKind(t, tr.printer, fractal.KindRPC, len(ids))
	for i, m := range onlyKind(tr.printer, fractal.KindRPC) {
		if want := string(rpcPayload(t, ids[i])); string(m) != want {
			t.Errorf("invocation %d is %s, want %s", i, m, want)
		}
	}
}

// TestTheTreeCarriesTheAncestorMessages sends AREP and ALOS down and ARBR up
// between real NATS servers, one hop at a time. Every hop delivers the bytes
// it was given, and an ARBR names the link it arrived on (fractal-nats.md,
// AREP, ALOS and ARBR).
func TestTheTreeCarriesTheAncestorMessages(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	tr := newTree(ctx, t)

	report := arepPayload(t, 1751400000000)
	send(t, tr.hq, fractalnode.Dispatch{Kind: fractal.KindAREP, Payload: report, Child: "brno"})
	awaitKind(t, tr.brno, fractal.KindAREP, 1)
	send(t, tr.brno, fractalnode.Dispatch{Kind: fractal.KindAREP, Payload: report, Child: "printer-1"})
	awaitKind(t, tr.printer, fractal.KindAREP, 1)

	loss := alosPayload(t, "hq/brno")
	send(t, tr.brno, fractalnode.Dispatch{Kind: fractal.KindALOS, Payload: loss, Child: "printer-1"})
	awaitKind(t, tr.printer, fractal.KindALOS, 1)

	request := arbrPayload(t)
	send(t, tr.printer, fractalnode.Dispatch{Kind: fractal.KindARBR, Payload: request})
	awaitKind(t, tr.brno, fractal.KindARBR, 1)
	send(t, tr.brno, fractalnode.Dispatch{Kind: fractal.KindARBR, Payload: request})
	awaitKind(t, tr.hq, fractal.KindARBR, 1)

	got := map[fractal.NodeID][]arrival{
		"hq":        arrivals(tr.hq),
		"brno":      arrivals(tr.brno),
		"printer-1": arrivals(tr.printer),
	}
	want := map[fractal.NodeID][]arrival{
		"hq": {{kind: fractal.KindARBR, from: "brno", payload: string(request)}},
		"brno": {
			{kind: fractal.KindAREP, from: "", payload: string(report)},
			{kind: fractal.KindARBR, from: "printer-1", payload: string(request)},
		},
		"printer-1": {
			{kind: fractal.KindAREP, from: "", payload: string(report)},
			{kind: fractal.KindALOS, from: "", payload: string(loss)},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the nodes received %+v, want %+v", got, want)
	}
}

// TestBothEndsSeeEveryShortDrop cuts the link of printer-1 for 10 ms, ten
// times, at the default poll interval and heartbeats, and counts the calls
// about the drop at both ends. The NATS server dials again only after its
// leafnode reconnect interval of one second, and the next cut follows as soon
// as a message crosses, so each drop lasts about one poll. A loss of a leaf
// connection is a disconnect however briefly it lasts (fractal-nats.md,
// Connectivity Detection).
func TestBothEndsSeeEveryShortDrop(t *testing.T) {
	if testing.Short() {
		t.Skip("this test cuts a link ten times at the default poll interval of one second")
	}
	t.Parallel()
	const cuts = 10
	ctx := t.Context()
	tr := newTree(ctx, t, func(cfg *natsbind.Config) {
		cfg.PollInterval = natsbind.DefaultPollInterval
		cfg.HeartbeatInterval = natsbind.DefaultHeartbeatInterval
		cfg.MaxHeartbeatInterval = natsbind.DefaultMaxHeartbeatInterval
	})
	waitUntil(t, "printer-1 to see its parent link open", func() bool {
		return tr.printer.engine.ParentConnections() >= 1
	})
	type seen struct{ childLost, parentLost int }
	count := func() seen {
		return seen{childLost: len(tr.brno.engine.Lost()), parentLost: tr.printer.engine.ParentLosses()}
	}
	before := count()
	for range cuts {
		tr.toPrint.Cut()
		time.Sleep(10 * time.Millisecond)
		tr.toPrint.Open()
		awaitCrossing(t, tr)
	}
	after := count()
	got := seen{childLost: after.childLost - before.childLost, parentLost: after.parentLost - before.parentLost}
	if want := (seen{childLost: cuts, parentLost: cuts}); got != want {
		t.Errorf("the drops seen are %+v, want %+v", got, want)
	}
}

// TestTheNodeBelowSeesItsParentDropAtOnce cuts the link of printer-1 three
// times, at the default poll interval of one second, and leaves it closed
// until printer-1 calls ParentLost. Only the disconnect event of its NATS
// server tells it within parentDropSeen, and a poll does so in one of five
// cuts (fractal-nats.md, Connectivity Detection).
func TestTheNodeBelowSeesItsParentDropAtOnce(t *testing.T) {
	if testing.Short() {
		t.Skip("this test cuts a link three times at the default poll interval of one second")
	}
	t.Parallel()
	const parentDropSeen = 200 * time.Millisecond
	tr := newTree(t.Context(), t, func(cfg *natsbind.Config) {
		cfg.PollInterval = natsbind.DefaultPollInterval
	})
	for cut := 1; cut <= 3; cut++ {
		waitUntil(t, "printer-1 to see its parent link open", func() bool {
			return tr.printer.engine.ParentConnections() >= cut
		})
		tr.toPrint.Cut()
		at := time.Now()
		waitUntil(t, "printer-1 to see its parent link down", func() bool {
			return tr.printer.engine.ParentLosses() == cut
		})
		if took := time.Since(at); took > parentDropSeen {
			t.Errorf("printer-1 saw cut %d after %v, want within %v", cut, took, parentDropSeen)
		}
		tr.toPrint.Open()
	}
}

// awaitCrossing sends a property change from brno to printer-1 at every check
// until one more arrives. Core NATS drops a message sent while the leafnode
// connection is down, so an arrival proves the connection is open again.
func awaitCrossing(t testing.TB, tr *tree) {
	t.Helper()
	before := countKind(tr.printer, fractal.KindDES)
	waitUntil(t, "a property change to reach printer-1 again", func() bool {
		if countKind(tr.printer, fractal.KindDES) > before {
			return true
		}
		send(t, tr.brno, fractalnode.Dispatch{
			Kind: fractal.KindDES, Payload: desPayload(t, "hq/brno/printer-1"), Child: "printer-1",
		})
		return false
	})
}

// send calls SendDurable or SendBestEffort of a node, as the message type
// requires, and fails the test on an error.
func send(t testing.TB, n *node, d fractalnode.Dispatch) {
	t.Helper()
	var err error
	if d.Kind.Delivery() == fractal.Durable {
		err = n.binding.SendDurable(t.Context(), d)
	} else {
		err = n.binding.SendBestEffort(t.Context(), d)
	}
	if err != nil {
		t.Fatalf("send %s: %v", d.Kind, err)
	}
}

// awaitKind waits until the engine of a node has received want messages of one
// type, and fails the test when it has received more.
func awaitKind(t testing.TB, n *node, kind fractal.Kind, want int) {
	t.Helper()
	waitUntil(t, fmt.Sprintf("%d %s messages", want, kind), func() bool {
		return countKind(n, kind) >= want
	})
	if got := countKind(n, kind); got != want {
		t.Fatalf("the node received %d %s messages, want %d", got, kind, want)
	}
}

// onlyKind returns the payloads of the messages of one type that the engine
// of a node received, in order.
func onlyKind(n *node, kind fractal.Kind) [][]byte {
	var out [][]byte
	for _, m := range n.engine.Messages() {
		if m.Kind == kind {
			out = append(out, m.Payload)
		}
	}
	return out
}

// lastOf returns the payload of the latest message of one type, or nil.
func lastOf(n *node, kind fractal.Kind) []byte {
	held := onlyKind(n, kind)
	if len(held) == 0 {
		return nil
	}
	return held[len(held)-1]
}

// rbrPayload builds a request for a full state report.
func rbrPayload(t testing.TB) []byte {
	t.Helper()
	return encode(t, fractal.RBR{Version: fractal.Version})
}

// losPayload builds a connection loss message.
func losPayload(t testing.TB, sender fractal.NodePath, child fractal.NodeID) []byte {
	t.Helper()
	return encode(t, fractal.LOS{Version: fractal.Version, Sender: sender, Child: child})
}

// arepPayload builds a state report of hq for the nodes below it.
func arepPayload(t testing.TB, at fractal.Timestamp) []byte {
	t.Helper()
	return encode(t, fractal.AREP{
		Version:   fractal.Version,
		Sender:    "hq",
		Interface: "Notice1",
		Instance:  "1",
		Timestamp: at,
		Props:     fractal.Props{"message": json.RawMessage(`"hello"`)},
	})
}

// alosPayload builds the message that the node at sender lost the node above
// it.
func alosPayload(t testing.TB, sender fractal.NodePath) []byte {
	t.Helper()
	return encode(t, fractal.ALOS{Version: fractal.Version, Sender: sender})
}

// arbrPayload builds a request for a full state report of the nodes above.
func arbrPayload(t testing.TB) []byte {
	t.Helper()
	return encode(t, fractal.ARBR{Version: fractal.Version})
}

// arrival is one message the recorder of a node took, with the payload as
// text, so a test compares whole lists of them.
type arrival struct {
	kind    fractal.Kind
	from    fractal.NodeID
	payload string
}

// arrivals returns every message the engine of a node received, in order.
func arrivals(n *node) []arrival {
	var out []arrival
	for _, m := range n.engine.Messages() {
		out = append(out, arrival{kind: m.Kind, from: m.From, payload: string(m.Payload)})
	}
	return out
}
