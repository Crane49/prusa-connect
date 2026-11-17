package node_test

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	"github.com/prusa3d/prusa-connect/sdk/fractal/node"
	"github.com/prusa3d/prusa-connect/sdk/fractal/shadow"
)

// errNoLink is the error a Binding returns when it has no link to printer-1. It
// wraps node.ErrNoSuchLink.
var errNoLink = fmt.Errorf("printer-1 below hq/brno: %w", node.ErrNoSuchLink)

// countingMetrics is a Metrics that counts the drops by DropReason.
type countingMetrics struct {
	mu    sync.Mutex
	drops map[node.DropReason]int
}

// Message counts nothing, because these tests read only the drop reasons.
func (m *countingMetrics) Message(fractal.Kind, node.Outcome) {}

// Drop counts one dropped message under its reason.
func (m *countingMetrics) Drop(_ fractal.Kind, r node.DropReason) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.drops == nil {
		m.drops = map[node.DropReason]int{}
	}
	m.drops[r]++
}

// count returns how many drops had the reason r.
func (m *countingMetrics) count(r node.DropReason) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.drops[r]
}

// newCountingNode builds and starts a node at hq/brno with a countingMetrics.
// Its binding returns the error refuse for every send to a node below.
func newCountingNode(t *testing.T, refuse error) (*node.Node, *recordBinding, *countingMetrics) {
	t.Helper()
	store, err := shadow.New(shadow.Config{Selection: shadow.Selection{Patterns: []fractal.Pattern{"*"}}})
	if err != nil {
		t.Fatalf("build the shadow: %v", err)
	}
	binding := &recordBinding{}
	metrics := &countingMetrics{}
	n, err := node.New(node.Config{
		Binding:    binding,
		Admissions: children{printerID},
		Shadow:     store,
		Clock:      &fixedClock{at: now},
		Metrics:    metrics,
		Path:       brnoPath,
	})
	if err != nil {
		t.Fatalf("build the node: %v", err)
	}
	if _, regErr := n.Register(node.Instance{
		Definition: definition(t),
		Owner:      &testOwner{snapshot: fractal.Props{tempProp: json.RawMessage(`25.0`)}},
		Name:       ifaceName,
		Instance:   ifaceInstance,
	}); regErr != nil {
		t.Fatalf("register the interface: %v", regErr)
	}
	if startErr := n.Start(t.Context()); startErr != nil {
		t.Fatalf("start the node: %v", startErr)
	}
	t.Cleanup(func() { _ = n.Close() })
	// Start has already sent its RBR. The sends fail only from here on, so the
	// test counts only the message it delivers.
	binding.Reset()
	binding.FailDown(refuse)
	return n, binding, metrics
}

// TestALinkThatIsNotThereIsCountedApart checks the DropReason of a DES the
// binding failed to send. An error that wraps ErrNoSuchLink counts as
// DropNotRoutable, and any other error counts as DropSendFailed.
func TestALinkThatIsNotThereIsCountedApart(t *testing.T) {
	t.Parallel()
	cases := []struct {
		refuse error
		name   string
		want   node.DropReason
	}{
		{refuse: errNoLink, name: "no such link", want: node.DropNotRoutable},
		{refuse: errBroken, name: "a transport that failed", want: node.DropSendFailed},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				n, _, metrics := newCountingNode(t, c.refuse)
				n.Deliver(t.Context(), node.Inbound{
					Kind: fractal.KindDES,
					Payload: encode(t, desire(printerPath, fractal.Props{
						targetProp: json.RawMessage(`250.0`),
					})),
				})
				synctest.Wait()
				if got := metrics.count(c.want); got != 1 {
					t.Errorf("%s was counted %d times, want once", c.want, got)
				}
			})
		})
	}
}

// TestACallIntoALinkThatIsNotThereIsAnswered checks an RPC the binding refuses
// with ErrNoSuchLink. The node answers the caller with CodeNodeNotFound, so the
// caller does not wait until the request expires. It counts the RPC as
// DropNotRoutable and calls Ack (fractal.md, Message Routing (Unicast)).
func TestACallIntoALinkThatIsNotThereIsAnswered(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		n, binding, metrics := newCountingNode(t, errNoLink)
		a := &acker{}
		n.Deliver(t.Context(), node.Inbound{
			Kind:    fractal.KindRPC,
			Payload: encode(t, invocation(printerPath)),
			Ack:     a.ack,
		})
		synctest.Wait()

		answers := binding.OfKind(fractal.KindRES)
		if len(answers) != 1 {
			t.Fatalf("the node sent %d RES messages, want one", len(answers))
		}
		res := decodeRES(t, answers[0].dispatch.Payload)
		if res.Error == nil || !res.Error.Code.Is(fractal.CodeNodeNotFound) {
			t.Errorf("the error of the RES is %+v, want CodeNodeNotFound", res.Error)
		}
		if got := metrics.count(node.DropNotRoutable); got != 1 {
			t.Errorf("the node counted DropNotRoutable %d times, want once", got)
		}
		if got := a.Count(); got != 1 {
			t.Errorf("the node called Ack %d times, want once", got)
		}
	})
}
