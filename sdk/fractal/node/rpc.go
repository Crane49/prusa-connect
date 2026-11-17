package node

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	"github.com/prusa3d/prusa-connect/sdk/fractal/schema"
)

// The error texts a node puts in the answer to a call it did not run or that
// failed. They are fixed. Details go to the log of this node only.
const (
	msgExpired       = "the request arrived after it expired"
	msgNotFound      = "this node has no such node under it"
	msgInternal      = "this node could not run the method"
	msgUnknownOwner  = "this node does not hold that interface instance"
	msgUnknownMethod = "that interface instance has no such method"
)

// deliverRPC decodes a method invocation and puts it on the RPC queue. One
// goroutine reads that queue, so a node runs one method at a time, in order.
func (n *Node) deliverRPC(ctx context.Context, state *pathState, in Inbound) {
	m, ok := decode[fractal.RPC](ctx, n, in)
	if !ok {
		return
	}
	// An RPC arrives from the node above, so its caller must be a node above
	// this one.
	if !m.Sender.IsAncestorOf(state.path) {
		n.drop(ctx, in, DropSenderInvalid)
		return
	}
	taken := n.submit(n.rpc, func(workCtx context.Context) { n.processRPC(workCtx, m, in) })
	if !taken {
		n.dropBusy(ctx, in)
	}
}

// processRPC handles one RPC. The expiry check must come before the receiver
// check. It then runs the method or forwards the request.
func (n *Node) processRPC(ctx context.Context, m fractal.RPC, in Inbound) {
	state := n.state.Load()
	if state == nil {
		n.drop(ctx, in, DropNoNodePath)
		return
	}
	if m.Expired(fractal.TimestampOf(n.clock.Now())) {
		n.log.InfoContext(ctx, "a method invocation expired", "id", m.ID, "receiver", m.Receiver)
		if !n.answer(ctx, m, failure(fractal.CodeExpired, msgExpired)) {
			return
		}
		n.metrics.Drop(fractal.KindRPC, DropRejected)
		n.metrics.Message(fractal.KindRPC, OutcomeDropped)
		n.ack(ctx, in)
		return
	}
	if m.Receiver == state.path {
		n.executeRPC(ctx, m, in)
		return
	}
	child, ok := n.nextHop(state, m.Receiver)
	if !ok {
		n.notFound(ctx, m, in, "a method invocation names no node below this one")
		return
	}
	// Ack comes only after SendDurable has stored the forwarded copy, so a
	// crash between the two loses nothing.
	dispatch := Dispatch{Kind: fractal.KindRPC, Payload: in.Payload, Child: child, ID: m.ID}
	if err := n.send(ctx, dispatch); err != nil {
		if errors.Is(err, ErrNoSuchLink) {
			// The request cannot arrive. Answer the caller at once, so it does
			// not wait until the request expires.
			n.notFound(ctx, m, in, "the transport has no link to the next node of a method invocation")
			return
		}
		n.log.ErrorContext(ctx, "forward a method invocation", "id", m.ID, "child", child, "error", err)
		n.metrics.Drop(fractal.KindRPC, DropSendFailed)
		return
	}
	n.metrics.Message(fractal.KindRPC, OutcomeForwarded)
	n.ack(ctx, in)
}

// notFound answers an RPC this node cannot forward with fractal.CodeNodeNotFound,
// and calls Ack once the answer was sent. why is the log message.
func (n *Node) notFound(ctx context.Context, m fractal.RPC, in Inbound, why string) {
	n.log.ErrorContext(ctx, why, "id", m.ID, "receiver", m.Receiver)
	if !n.answer(ctx, m, failure(fractal.CodeNodeNotFound, msgNotFound)) {
		return
	}
	n.metrics.Drop(fractal.KindRPC, DropNotRoutable)
	n.metrics.Message(fractal.KindRPC, OutcomeDropped)
	n.ack(ctx, in)
}

// executeRPC handles an RPC whose receiver is this node. It sends the stored
// answer for a repeated id. Otherwise it runs the method.
func (n *Node) executeRPC(ctx context.Context, m fractal.RPC, in Inbound) {
	stored, ok, err := n.executed.Executed(ctx, m.ID)
	if err != nil {
		n.log.ErrorContext(ctx, "read the record of an executed method", "id", m.ID, "error", err)
		return
	}
	if ok {
		// The method already ran for this id. Send the stored bytes again.
		n.log.InfoContext(ctx, "a method invocation arrived again", "id", m.ID)
		if !n.answerWent(ctx, m.ID, stored) {
			return
		}
		n.metrics.Message(fractal.KindRPC, OutcomeProcessed)
		n.ack(ctx, in)
		return
	}
	res := n.runMethod(ctx, m)
	payload, err := json.Marshal(res)
	if err != nil {
		n.log.ErrorContext(ctx, "encode the answer to a method invocation", "id", m.ID, "error", err)
		return
	}
	// The order is required: record the answer, send it, then acknowledge the
	// request.
	if recordExecutedErr := n.executed.RecordExecuted(ctx, m.ID, payload); recordExecutedErr != nil {
		n.log.ErrorContext(ctx, "record an executed method", "id", m.ID, "error", recordExecutedErr)
		return
	}
	if !n.answerWent(ctx, m.ID, payload) {
		return
	}
	n.metrics.Message(fractal.KindRPC, OutcomeProcessed)
	n.ack(ctx, in)
}

// answerWent sends the answer to a method invocation and reports whether the
// transport took it. On false the caller must not acknowledge the request.
func (n *Node) answerWent(ctx context.Context, id fractal.RPCID, payload json.RawMessage) bool {
	err := n.send(ctx, Dispatch{Kind: fractal.KindRES, Payload: payload, ID: id})
	if err == nil {
		return true
	}
	n.log.ErrorContext(ctx, "send the answer to a method invocation", "id", id, "error", err)
	n.metrics.Drop(fractal.KindRES, sendDropReason(err))
	return false
}

// runMethod runs one method and returns the answer. The first step that fails
// decides the error code: lookup, parameters, Guard, Invoke, result.
func (n *Node) runMethod(ctx context.Context, m fractal.RPC) fractal.RES {
	handle, ok := n.lookup(m.Interface, m.Instance)
	if !ok {
		n.log.WarnContext(ctx, "a method invocation names an interface this node does not own",
			"id", m.ID, "interface", m.Interface, "instance", m.Instance)
		return n.result(m, nil, failure(fractal.CodeObjectNotFound, msgUnknownOwner))
	}
	if handle.def != nil {
		if err := handle.def.ValidateParams(m.Method, m.Params); err != nil {
			if errors.Is(err, schema.ErrUnknownMethod) {
				n.log.InfoContext(ctx, "a method invocation names a method the interface does not have",
					"id", m.ID, "interface", m.Interface, "method", m.Method)
				return n.result(m, nil, failure(fractal.CodeObjectNotFound, msgUnknownMethod))
			}
			n.log.InfoContext(ctx, "the parameters of a method invocation failed validation",
				"id", m.ID, "method", m.Method, "error", err)
			return n.result(m, nil, failure(fractal.CodeInvalidParams, err.Error()))
		}
	}
	call := Call{Method: m.Method, Params: m.Params, Caller: m.Sender, ID: m.ID}
	if guard, guards := handle.owner.(Guard); guards {
		if err := guard.Allow(ctx, call); err != nil {
			n.log.InfoContext(ctx, "the owner refused a method invocation", "id", m.ID, "error", err)
			return n.result(m, nil, refusal(err))
		}
	}
	// runCtx ends at the expiry time the caller set. A method that is still
	// running then must stop.
	runCtx, cancel := context.WithDeadline(ctx, m.Expires.Time())
	defer cancel()
	value, err := invoke(runCtx, handle.owner, call)
	if err != nil {
		n.log.ErrorContext(ctx, "a method failed", "id", m.ID, "method", m.Method, "error", err)
		return n.result(m, nil, refusalOrInternal(err))
	}
	if handle.def != nil {
		if validateResultErr := handle.def.ValidateResult(m.Method, value); validateResultErr != nil {
			n.log.ErrorContext(ctx, "a method returned a result its definition refuses",
				"id", m.ID, "method", m.Method, "error", validateResultErr)
			return n.result(m, nil, failure(fractal.CodeInternal, msgInternal))
		}
	}
	return n.result(m, value, nil)
}

// result builds the RES for one RPC. The receiver of the RES is the sender of
// the RPC.
func (n *Node) result(m fractal.RPC, value json.RawMessage, failed *fractal.Error) fractal.RES {
	res := fractal.RES{Version: fractal.Version, ID: m.ID, Receiver: m.Sender, Error: failed}
	if failed == nil {
		res.Result = value
	}
	return res
}

// answer sends a RES with an error for an RPC this node did not run. On false
// the caller must not acknowledge the RPC.
func (n *Node) answer(ctx context.Context, m fractal.RPC, failed *fractal.Error) bool {
	payload, err := json.Marshal(n.result(m, nil, failed))
	if err != nil {
		n.log.ErrorContext(ctx, "encode the answer to a method invocation", "id", m.ID, "error", err)
		return false
	}
	return n.answerWent(ctx, m.ID, payload)
}

// invoke calls Owner.Invoke and turns a panic in it into an error, so a bug in
// one owner does not crash the node.
func invoke(ctx context.Context, owner Owner, call Call) (value json.RawMessage, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("the method panicked: %v", recovered)
		}
	}()
	return owner.Invoke(ctx, call)
}

// failure builds the error field of a RES.
func failure(code fractal.Code, message string) *fractal.Error {
	return &fractal.Error{Code: code, Message: message}
}

// refusal turns the error from Guard.Allow into the error field of a RES: a
// wrapped *fractal.Error with a valid code, or CodeDenied with the error text.
func refusal(err error) *fractal.Error {
	var carried *fractal.Error
	if errors.As(err, &carried) && carried.Code.Valid() {
		return carried
	}
	return failure(fractal.CodeDenied, err.Error())
}

// refusalOrInternal turns the error from Owner.Invoke into the error field of
// a RES: a wrapped *fractal.Error with a valid code, or CodeInternal.
func refusalOrInternal(err error) *fractal.Error {
	var carried *fractal.Error
	if errors.As(err, &carried) && carried.Code.Valid() {
		return carried
	}
	return failure(fractal.CodeInternal, msgInternal)
}
