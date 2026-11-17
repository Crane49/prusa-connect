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

// request builds the CallRequest of the tests: SetNozzleTemp(250.0) on target.
func request(target fractal.NodePath, expires fractal.Timestamp) node.CallRequest {
	return node.CallRequest{
		Target:    target,
		Interface: ifaceName,
		Instance:  ifaceInstance,
		Method:    setTemp,
		Params:    []json.RawMessage{json.RawMessage(`250.0`)},
		Expires:   expires,
	}
}

// TestACallSendsAndWaits checks that Call sends one durable RPC to the node
// directly below that leads to the target, waits, and returns the RES that
// arrives for the request id.
func TestACallSendsAndWaits(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, brnoPath)
		answers := make(chan fractal.RES, 1)
		go func() {
			res, err := h.node.Call(t.Context(), request(printerPath, now+30000))
			if err != nil {
				t.Errorf("call: %v", err)
				return
			}
			answers <- res
		}()
		synctest.Wait()

		requests := h.binding.OfKind(fractal.KindRPC)
		if len(requests) != 1 {
			t.Fatalf("the node sent %d RPC messages, want one", len(requests))
		}
		if requests[0].dispatch.Child != printerID || !requests[0].durable {
			t.Errorf("the RPC went to %q with durable=%v, want %q with a durable send",
				requests[0].dispatch.Child, requests[0].durable, printerID)
		}
		var sentRPC fractal.RPC
		if err := json.Unmarshal(requests[0].dispatch.Payload, &sentRPC); err != nil {
			t.Fatalf("decode the RPC: %v", err)
		}
		if sentRPC.Sender != brnoPath {
			t.Errorf("the sender of the RPC is %s, want %s", sentRPC.Sender, brnoPath)
		}
		if got := h.node.PendingCalls(); got != 1 {
			t.Errorf("PendingCalls is %d, want one", got)
		}

		answer := encode(t, fractal.RES{
			Version:  fractal.Version,
			ID:       sentRPC.ID,
			Receiver: brnoPath,
			Result:   json.RawMessage(`true`),
		})
		h.node.Deliver(t.Context(), node.Inbound{Kind: fractal.KindRES, Payload: answer, From: printerID})
		synctest.Wait()

		select {
		case res := <-answers:
			if string(res.Result) != "true" {
				t.Errorf("the result is %s, want true", res.Result)
			}
		default:
			t.Fatal("Call did not return after the RES arrived")
		}
		if got := h.node.PendingCalls(); got != 0 {
			t.Errorf("PendingCalls is %d after the answer, want none", got)
		}
	})
}

// TestACallIsRefusedBeforeItReachesTheTransport checks that Call and Desire
// return ErrNotBelow and send nothing for a target that is not below this
// node: the node above, the node itself, a node beside it, and a node under
// that one (fractal.md, Message Routing (Unicast)).
func TestACallIsRefusedBeforeItReachesTheTransport(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		target fractal.NodePath
		want   error
	}{
		{"the node above", hqPath, node.ErrNotBelow},
		{"itself", brnoPath, node.ErrNotBelow},
		{"a sibling", "hq/praha", node.ErrNotBelow},
		{"a node below a sibling", "hq/praha/printer-1", node.ErrNotBelow},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				h := newHarness(t, brnoPath)
				_, err := h.node.Call(t.Context(), request(c.target, now+30000))
				if !errors.Is(err, c.want) {
					t.Errorf("Call returned %v, want %v", err, c.want)
				}
				if desireErr := h.node.Desire(t.Context(), node.DesireRequest{
					Target:    c.target,
					Interface: ifaceName,
					Instance:  ifaceInstance,
					Props:     fractal.Props{targetProp: json.RawMessage(`1.0`)},
				}); !errors.Is(desireErr, c.want) {
					t.Errorf("Desire returned %v, want %v", desireErr, c.want)
				}
				if got := len(h.binding.Sent()); got != 0 {
					t.Errorf("the node sent %d messages, want none", got)
				}
			})
		})
	}
}

// TestACallToANodeUnderNoChildIsRefused checks that Call returns a
// *fractal.Error with CodeNodeNotFound when the target path is below this node and
// Admissions has not admitted the next node on the way.
func TestACallToANodeUnderNoChildIsRefused(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, brnoPath)
		_, err := h.node.Call(t.Context(), request("hq/brno/printer-9", now+30000))
		var carried *fractal.Error
		if !errors.As(err, &carried) || !carried.Code.Is(fractal.CodeNodeNotFound) {
			t.Errorf("Call returned %v, want an error with CodeNodeNotFound", err)
		}
	})
}

// TestACallWithNoNodePathIsRefused checks that Call and Desire return
// ErrNoNodePath while the node has no Node Path (fractal.md, Node Path).
func TestACallWithNoNodePathIsRefused(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		n, err := node.New(node.Config{Binding: &recordBinding{}, Admissions: children{printerID}})
		if err != nil {
			t.Fatalf("build the node: %v", err)
		}
		_, callErr := n.Call(t.Context(), request(printerPath, now+30000))
		if !errors.Is(callErr, node.ErrNoNodePath) {
			t.Errorf("Call returned %v, want %v", callErr, node.ErrNoNodePath)
		}
		err = n.Desire(t.Context(), node.DesireRequest{
			Target:    printerPath,
			Interface: ifaceName,
			Instance:  ifaceInstance,
			Props:     fractal.Props{targetProp: json.RawMessage(`1.0`)},
		})
		if !errors.Is(err, node.ErrNoNodePath) {
			t.Errorf("Desire returned %v, want %v", err, node.ErrNoNodePath)
		}
	})
}

// TestACallEndsWhenItsRequestExpires checks that Call returns ErrCallExpired
// when no answer arrives before the request has expired and CallGrace has
// passed, and that the pending map is then empty.
func TestACallEndsWhenItsRequestExpires(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, brnoPath)
		_, err := h.node.Call(t.Context(), request(printerPath, now))
		if !errors.Is(err, node.ErrCallExpired) {
			t.Errorf("Call returned %v, want %v", err, node.ErrCallExpired)
		}
		if got := h.node.PendingCalls(); got != 0 {
			t.Errorf("PendingCalls is %d after the call expired, want none", got)
		}
	})
}

// TestACallEndsWithItsContext checks that Call returns the context error when
// its context is cancelled, and that the pending map is then empty.
func TestACallEndsWithItsContext(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, brnoPath)
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)
		go func() {
			_, err := h.node.Call(ctx, request(printerPath, now+30000))
			done <- err
		}()
		synctest.Wait()
		cancel()
		synctest.Wait()

		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Errorf("Call returned %v, want context.Canceled", err)
		}
		if got := h.node.PendingCalls(); got != 0 {
			t.Errorf("PendingCalls is %d after the cancellation, want none", got)
		}
	})
}

// TestTheCallTableIsBounded checks that Call returns ErrTooManyCalls when
// MaxPendingCalls calls already wait for an answer.
func TestTheCallTableIsBounded(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		binding := &recordBinding{}
		n, err := node.New(node.Config{
			Binding:         binding,
			Admissions:      children{printerID},
			Path:            brnoPath,
			MaxPendingCalls: 1,
		})
		if err != nil {
			t.Fatalf("build the node: %v", err)
		}
		go func() {
			_, _ = n.Call(t.Context(), request(printerPath, now+30000))
		}()
		synctest.Wait()

		_, callErr := n.Call(t.Context(), request(printerPath, now+30000))
		if !errors.Is(callErr, node.ErrTooManyCalls) {
			t.Errorf("the second Call returned %v, want %v", callErr, node.ErrTooManyCalls)
		}
	})
}

// TestAnAnswerIsActedOnOnce checks a RES that arrives twice at the node that
// made the call. The node calls Ack for both deliveries and forwards neither
// (fractal.md, Method Invocation Result Message).
func TestAnAnswerIsActedOnOnce(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, brnoPath)
		answer := encode(t, fractal.RES{
			Version:  fractal.Version,
			ID:       "a",
			Receiver: brnoPath,
			Result:   json.RawMessage(`true`),
		})
		first := &acker{}
		second := &acker{}
		h.node.Deliver(t.Context(), node.Inbound{
			Kind: fractal.KindRES, Payload: answer, From: printerID, Ack: first.ack,
		})
		synctest.Wait()
		h.node.Deliver(t.Context(), node.Inbound{
			Kind: fractal.KindRES, Payload: answer, From: printerID, Ack: second.ack,
		})
		synctest.Wait()

		if first.Count() != 1 || second.Count() != 1 {
			t.Errorf("the node called Ack %d and %d times, want once for each delivery", first.Count(), second.Count())
		}
		if got := len(h.binding.Sent()); got != 0 {
			t.Errorf("the node sent %d messages, want none", got)
		}
	})
}

// TestAnAnswerForAnotherNodeIsPassedUp checks that a node forwards a RES
// addressed to another node to the node above with a durable send, and then
// calls Ack (fractal.md, Method Invocation Result Message).
func TestAnAnswerForAnotherNodeIsPassedUp(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, brnoPath)
		answer := encode(t, fractal.RES{
			Version:  fractal.Version,
			ID:       "a",
			Receiver: hqPath,
			Result:   json.RawMessage(`true`),
		})
		a := &acker{}
		h.node.Deliver(t.Context(), node.Inbound{
			Kind: fractal.KindRES, Payload: answer, From: printerID, Ack: a.ack,
		})
		synctest.Wait()

		passed := h.binding.OfKind(fractal.KindRES)
		if len(passed) != 1 {
			t.Fatalf("the node forwarded %d RES messages, want one", len(passed))
		}
		if !passed[0].durable || passed[0].dispatch.ID != "a" {
			t.Errorf("the RES went with durable=%v and id %q, want a durable send with id a",
				passed[0].durable, passed[0].dispatch.ID)
		}
		if a.Count() != 1 {
			t.Errorf("the node called Ack %d times, want once", a.Count())
		}
	})
}

// TestARootDropsAnAnswerItIsNotTheCallerOf checks that a root node drops a RES
// addressed to another node and calls Ack, because it has no node above to
// forward it to (fractal.md, Method Invocation Result Message).
func TestARootDropsAnAnswerItIsNotTheCallerOf(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, hqPath)
		answer := encode(t, fractal.RES{
			Version:  fractal.Version,
			ID:       "a",
			Receiver: "elsewhere",
			Result:   json.RawMessage(`true`),
		})
		a := &acker{}
		h.node.Deliver(t.Context(), node.Inbound{
			Kind: fractal.KindRES, Payload: answer, From: printerID, Ack: a.ack,
		})
		synctest.Wait()

		if got := len(h.binding.Sent()); got != 0 {
			t.Errorf("the node sent %d messages, want none", got)
		}
		if a.Count() != 1 {
			t.Errorf("the node called Ack %d times, want once", a.Count())
		}
	})
}

// TestADifferentNodePathDiscardsWhatItCannotRoute checks SetPath with a new
// Node Path. The node empties its pending map and its MemoryDedup, and the
// waiting Call never receives an answer (fractal.md, After the Join Exchange).
func TestADifferentNodePathDiscardsWhatItCannotRoute(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		dedup := node.NewMemoryDedup(nil, time.Minute, 16)
		binding := &recordBinding{}
		n, err := node.New(node.Config{
			Binding:    binding,
			Admissions: children{printerID},
			Executed:   dedup,
			Processed:  dedup,
			Path:       brnoPath,
		})
		if err != nil {
			t.Fatalf("build the node: %v", err)
		}
		if startErr := n.Start(t.Context()); startErr != nil {
			t.Fatalf("start the node: %v", startErr)
		}
		t.Cleanup(func() { _ = n.Close() })
		if recordErr := dedup.RecordExecuted(t.Context(), "a", []byte("{}")); recordErr != nil {
			t.Fatalf("record: %v", recordErr)
		}
		go func() {
			_, callErr := n.Call(t.Context(), request(printerPath, now+30000))
			if callErr == nil {
				t.Error("Call returned an answer after the Node Path changed, want an error")
			}
		}()
		synctest.Wait()

		if pathErr := n.SetPath(t.Context(), "hq/praha"); pathErr != nil {
			t.Fatalf("set the new path: %v", pathErr)
		}
		synctest.Wait()

		if got := n.PendingCalls(); got != 0 {
			t.Errorf("PendingCalls is %d after the path changed, want none", got)
		}
		executed, processed := dedup.Len()
		if executed != 0 || processed != 0 {
			t.Errorf("the store has %d executed and %d processed records, want none", executed, processed)
		}
		if n.Path() != "hq/praha" {
			t.Errorf("Path is %s, want hq/praha", n.Path())
		}
	})
}
