package node_test

import (
	"encoding/json"
	"errors"
	"log/slog"
	"slices"
	"testing"
	"testing/synctest"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	"github.com/prusa3d/prusa-connect/sdk/fractal/node"
)

// errBroken is the error the test owners, bindings and stores return for a
// failure that has no Fractal error code.
var errBroken = errors.New("the device did not answer")

// TestTheRecipientAnswersEveryInvocation checks the error code of the RES for
// each way an RPC fails at its receiver, and whether the method ran. The node
// sends one RES to the caller and calls Ack in every case (fractal.md, Method
// Invocation Request Message).
func TestTheRecipientAnswersEveryInvocation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		setUp func(h *harness, m *fractal.RPC)
		code  fractal.Code
		runs  int
	}{
		{
			name:  "a request that arrived after it expired",
			setUp: func(_ *harness, m *fractal.RPC) { m.Expires = now - 1 },
			code:  fractal.CodeExpired,
		},
		{
			name:  "an interface instance the node does not have",
			setUp: func(_ *harness, m *fractal.RPC) { m.Instance = "9" },
			code:  fractal.CodeObjectNotFound,
		},
		{
			name:  "a method the interface does not have",
			setUp: func(_ *harness, m *fractal.RPC) { m.Method = "Fly" },
			code:  fractal.CodeObjectNotFound,
		},
		{
			name: "a method the interface does not have, with parameters of the wrong type",
			setUp: func(_ *harness, m *fractal.RPC) {
				m.Method = "Fly"
				m.Params = []json.RawMessage{json.RawMessage(`"warm"`)}
			},
			code: fractal.CodeObjectNotFound,
		},
		{
			name:  "a parameter of the wrong type",
			setUp: func(_ *harness, m *fractal.RPC) { m.Params = []json.RawMessage{json.RawMessage(`"warm"`)} },
			code:  fractal.CodeInvalidParams,
		},
		{
			name:  "a method that failed",
			setUp: func(h *harness, _ *fractal.RPC) { h.owner.invokeErr = errBroken },
			code:  fractal.CodeInternal,
			runs:  1,
		},
		{
			name:  "a method that panicked",
			setUp: func(h *harness, _ *fractal.RPC) { h.owner.panics = true },
			code:  fractal.CodeInternal,
			runs:  1,
		},
		{
			name: "a result the definition refuses",
			setUp: func(h *harness, _ *fractal.RPC) {
				h.owner.result = json.RawMessage(`"yes"`)
			},
			code: fractal.CodeInternal,
			runs: 1,
		},
		{
			name: "a method the owner refused",
			setUp: func(h *harness, _ *fractal.RPC) {
				h.owner.invokeErr = &fractal.Error{Code: fractal.CodeDenied, Message: "the door is open"}
			},
			code: fractal.CodeDenied,
			runs: 1,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				h := newHarness(t, printerPath)
				m := invocation(printerPath)
				c.setUp(h, &m)
				a := &acker{}
				h.node.Deliver(t.Context(), node.Inbound{
					Kind:    fractal.KindRPC,
					Payload: encode(t, m),
					Ack:     a.ack,
				})
				synctest.Wait()

				answers := h.binding.OfKind(fractal.KindRES)
				if len(answers) != 1 {
					t.Fatalf("the node sent %d RES messages, want one", len(answers))
				}
				res := decodeRES(t, answers[0].dispatch.Payload)
				if res.Error == nil || !res.Error.Code.Is(c.code) {
					t.Errorf("the error of the RES is %+v, want %s", res.Error, c.code)
				}
				if res.Receiver != hqPath {
					t.Errorf("the receiver of the RES is %s, want the caller %s", res.Receiver, hqPath)
				}
				if got := h.owner.Runs(); got != c.runs {
					t.Errorf("the method ran %d times, want %d", got, c.runs)
				}
				if got := a.Count(); got != 1 {
					t.Errorf("the node called Ack %d times, want once", got)
				}
			})
		})
	}
}

// TestASuccessfulInvocationCarriesItsResult checks an RPC that succeeds. The
// node sends one RES with a durable send, with the result of the method and no
// error.
func TestASuccessfulInvocationCarriesItsResult(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, printerPath)
		h.owner.result = json.RawMessage(`true`)
		h.node.Deliver(t.Context(), node.Inbound{
			Kind:    fractal.KindRPC,
			Payload: encode(t, invocation(printerPath)),
		})
		synctest.Wait()

		answers := h.binding.OfKind(fractal.KindRES)
		if len(answers) != 1 {
			t.Fatalf("the node sent %d RES messages, want one", len(answers))
		}
		if !answers[0].durable {
			t.Error("the node sent the RES with a best-effort send, want a durable send")
		}
		res := decodeRES(t, answers[0].dispatch.Payload)
		if res.Failed() || string(res.Result) != "true" {
			t.Errorf("the RES is %+v, want the result true and no error", res)
		}
	})
}

// TestAnAnswerThatDidNotGoOutIsNotAcknowledged checks the order
// fractal-nats.md states for the node that ran the method: record the answer,
// send it, then acknowledge the request. When the send fails the node does not
// acknowledge, so the transport delivers the request again. The second
// delivery sends the recorded answer and does not run the method a second time
// (fractal-nats.md, Callee ordering).
func TestAnAnswerThatDidNotGoOutIsNotAcknowledged(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, printerPath)
		h.owner.result = json.RawMessage(`true`)
		h.binding.Fail(errBroken)
		a := &acker{}
		h.node.Deliver(t.Context(), node.Inbound{
			Kind:    fractal.KindRPC,
			Payload: encode(t, invocation(printerPath)),
			Ack:     a.ack,
		})
		synctest.Wait()

		if got := a.Count(); got != 0 {
			t.Errorf("the node acknowledged %d times after the send failed, want none", got)
		}
		if got := h.owner.Runs(); got != 1 {
			t.Fatalf("the method ran %d times, want once", got)
		}

		h.binding.Fail(nil)
		h.node.Deliver(t.Context(), node.Inbound{
			Kind:    fractal.KindRPC,
			Payload: encode(t, invocation(printerPath)),
			Ack:     a.ack,
		})
		synctest.Wait()

		if got := h.owner.Runs(); got != 1 {
			t.Errorf("the method ran %d times, want once: the record answers the second delivery", got)
		}
		answers := h.binding.OfKind(fractal.KindRES)
		if len(answers) != 1 {
			t.Fatalf("the node sent %d RES messages after the transport recovered, want one", len(answers))
		}
		res := decodeRES(t, answers[0].dispatch.Payload)
		if res.Failed() || string(res.Result) != "true" {
			t.Errorf("the RES is %+v, want the result true and no error", res)
		}
		if got := a.Count(); got != 1 {
			t.Errorf("the node acknowledged %d times, want once after the answer went out", got)
		}
	})
}

// TestAGuardRefusesBeforeTheMethodRuns checks an owner that implements Guard
// and whose Allow returns an error. The node answers with CodeDenied and does
// not call Invoke (fractal.md, Method Invocation Request Message, step 3).
func TestAGuardRefusesBeforeTheMethodRuns(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		store, err := shadowStore()
		if err != nil {
			t.Fatalf("build the shadow: %v", err)
		}
		binding := &recordBinding{}
		clock := &fixedClock{at: now}
		owner := &guardedOwner{testOwner: &testOwner{allowErr: errBroken}}
		n, err := node.New(node.Config{
			Binding: binding,
			Shadow:  store,
			Clock:   clock,
			Path:    printerPath,
		})
		if err != nil {
			t.Fatalf("build the node: %v", err)
		}
		registerOn(t, n, owner)
		if startErr := n.Start(t.Context()); startErr != nil {
			t.Fatalf("start the node: %v", startErr)
		}
		t.Cleanup(func() { _ = n.Close() })

		n.Deliver(t.Context(), node.Inbound{
			Kind:    fractal.KindRPC,
			Payload: encode(t, invocation(printerPath)),
		})
		synctest.Wait()

		answers := binding.OfKind(fractal.KindRES)
		if len(answers) != 1 {
			t.Fatalf("the node sent %d RES messages, want one", len(answers))
		}
		res := decodeRES(t, answers[0].dispatch.Payload)
		if res.Error == nil || !res.Error.Code.Is(fractal.CodeDenied) {
			t.Errorf("the error of the RES is %+v, want CodeDenied", res.Error)
		}
		if got := owner.Runs(); got != 0 {
			t.Errorf("the method ran %d times, want none", got)
		}
	})
}

// TestARepeatedInvocationResendsTheStoredAnswer checks an RPC that arrives
// twice with the same id. The node sends the same RES bytes both times and
// runs the method once (fractal.md, Method Invocation Request Message, check
// dedup).
func TestARepeatedInvocationResendsTheStoredAnswer(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, printerPath)
		h.owner.result = json.RawMessage(`true`)
		in := node.Inbound{Kind: fractal.KindRPC, Payload: encode(t, invocation(printerPath))}
		h.node.Deliver(t.Context(), in)
		synctest.Wait()
		h.node.Deliver(t.Context(), in)
		synctest.Wait()

		answers := h.binding.OfKind(fractal.KindRES)
		if len(answers) != 2 {
			t.Fatalf("the node sent %d RES messages, want two", len(answers))
		}
		if string(answers[0].dispatch.Payload) != string(answers[1].dispatch.Payload) {
			t.Error("the second RES has other bytes than the first, want the stored answer")
		}
		if got := h.owner.Runs(); got != 1 {
			t.Errorf("the method ran %d times, want once", got)
		}
	})
}

// TestAnInvocationIsRoutedDown checks that a node forwards an RPC for a node
// below it. The RPC goes unchanged, with its id, to the node whose Node ID is
// the next segment of the receiver path, and the node then calls Ack
// (fractal-nats.md, Persist before ack).
func TestAnInvocationIsRoutedDown(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, brnoPath)
		payload := encode(t, invocation(printerPath))
		a := &acker{}
		h.node.Deliver(t.Context(), node.Inbound{Kind: fractal.KindRPC, Payload: payload, Ack: a.ack})
		synctest.Wait()

		forwarded := h.binding.OfKind(fractal.KindRPC)
		if len(forwarded) != 1 {
			t.Fatalf("the node forwarded %d RPC messages, want one", len(forwarded))
		}
		if forwarded[0].dispatch.Child != printerID {
			t.Errorf("the RPC went to %q, want %q", forwarded[0].dispatch.Child, printerID)
		}
		if forwarded[0].dispatch.ID != "a" {
			t.Errorf("Dispatch.ID is %q, want the id of the RPC, a", forwarded[0].dispatch.ID)
		}
		if string(forwarded[0].dispatch.Payload) != string(payload) {
			t.Error("the node changed the payload of the RPC, want the bytes that arrived")
		}
		if got := a.Count(); got != 1 {
			t.Errorf("the node called Ack %d times, want once", got)
		}
	})
}

// TestARequestThatCannotBeRoutedIsAnswered checks an RPC whose receiver is
// under a node this one has not admitted. The node answers with CodeNodeNotFound
// (fractal.md, Method Invocation Request Message).
func TestARequestThatCannotBeRoutedIsAnswered(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, brnoPath)
		h.node.Deliver(t.Context(), node.Inbound{
			Kind:    fractal.KindRPC,
			Payload: encode(t, invocation("hq/brno/printer-9")),
		})
		synctest.Wait()

		answers := h.binding.OfKind(fractal.KindRES)
		if len(answers) != 1 {
			t.Fatalf("the node sent %d RES messages, want one", len(answers))
		}
		res := decodeRES(t, answers[0].dispatch.Payload)
		if res.Error == nil || !res.Error.Code.Is(fractal.CodeNodeNotFound) {
			t.Errorf("the error of the RES is %+v, want CodeNodeNotFound", res.Error)
		}
		want := []slog.Level{slog.LevelError}
		if got := h.log.Levels("a method invocation names no node below this one"); !slices.Equal(got, want) {
			t.Errorf("the node logged the drop at %v, want %v", got, want)
		}
	})
}

// TestARequestIsNotAcknowledgedWhenTheTransportRefuses checks that a node does
// not call Ack for an RPC when SendDurable returns an error. The transport
// then delivers the RPC again, so it is not lost (fractal-nats.md, Why
// persist-before-ack is the required order).
func TestARequestIsNotAcknowledgedWhenTheTransportRefuses(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, brnoPath)
		h.binding.Fail(errBroken)
		a := &acker{}
		h.node.Deliver(t.Context(), node.Inbound{
			Kind:    fractal.KindRPC,
			Payload: encode(t, invocation(printerPath)),
			Ack:     a.ack,
		})
		synctest.Wait()

		if got := a.Count(); got != 0 {
			t.Errorf("the node called Ack %d times, want none", got)
		}
	})
}

// TestAnUnsentErrorAnswerIsNotAcknowledged checks the two answers a
// node sends for a call it does not run. When the RES is not sent, the
// node does not acknowledge the RPC, so the transport delivers it again and
// the caller still gets an answer (fractal-nats.md, Crash-Safety).
func TestAnUnsentErrorAnswerIsNotAcknowledged(t *testing.T) {
	t.Parallel()
	expired := invocation(printerPath)
	expired.Expires = now - 1
	for _, c := range []struct {
		name string
		rpc  fractal.RPC
	}{
		{"an expired call", expired},
		{"a call for no node below", invocation("hq/brno/printer-9")},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				h := newHarness(t, brnoPath)
				h.binding.Fail(errBroken)
				a := &acker{}
				h.node.Deliver(t.Context(), node.Inbound{
					Kind:    fractal.KindRPC,
					Payload: encode(t, c.rpc),
					Ack:     a.ack,
				})
				synctest.Wait()

				if got := a.Count(); got != 0 {
					t.Errorf("the node called Ack %d times, want none", got)
				}
			})
		})
	}
}
