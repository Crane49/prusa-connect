package node

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
)

// ErrCallExpired is the error Call returns when no answer arrived before the
// request expired and Config.CallGrace passed after that. The caller does not
// know whether the method ran. Read the shadow to find out, or call again.
var ErrCallExpired = errors.New("no answer before the request expired")

// CallRequest describes one method invocation for Node.Call.
type CallRequest struct {
	// Target is the Node Path of the node that runs the method. It must name a
	// node below this one.
	Target fractal.NodePath
	// Interface is the Interface Name.
	Interface fractal.InterfaceName
	// Instance is the Interface Instance ID.
	Instance fractal.InstanceID
	// Method is the method name.
	Method fractal.MethodName
	// Params are the parameters in the order the method declares them. Leave
	// it empty for a method with no parameters.
	Params []json.RawMessage
	// Expires is the time after which the method must not start. A node that
	// receives the request after this time does not run or forward it, and
	// answers with fractal.CodeExpired.
	Expires fractal.Timestamp
}

// DesireRequest describes one property change request for Node.Desire.
type DesireRequest struct {
	// Target is the Node Path of the node that owns the properties. It must
	// name a node below this one.
	Target fractal.NodePath
	// Interface is the Interface Name.
	Interface fractal.InterfaceName
	// Instance is the Interface Instance ID.
	Instance fractal.InstanceID
	// Props are the properties to change, with their new values. A property
	// that is not in Props keeps its value.
	Props fractal.Props
}

// Call invokes a method on a node below this one and waits for the answer.
//
// When the method failed, or a node on the way refused the request, the Error
// field of the RES has the code and the Go error is nil. Call returns a Go
// error when it has no RES: ctx ended, the request expired (ErrCallExpired),
// the send failed, the target is not below this node (ErrNotBelow), or its
// next node is not admitted (a *fractal.Error with CodeNodeNotFound).
func (n *Node) Call(ctx context.Context, req CallRequest) (fractal.RES, error) {
	state := n.state.Load()
	if state == nil {
		return fractal.RES{}, ErrNoNodePath
	}
	if !state.path.IsAncestorOf(req.Target) {
		return fractal.RES{}, fmt.Errorf("%s: %w", req.Target, ErrNotBelow)
	}
	child, ok := n.nextHop(state, req.Target)
	if !ok {
		return fractal.RES{}, &fractal.Error{Code: fractal.CodeNodeNotFound, Message: msgNotFound}
	}
	id, err := fractal.NewRPCID()
	if err != nil {
		return fractal.RES{}, fmt.Errorf("build a request id: %w", err)
	}
	m := fractal.RPC{
		Version:   fractal.Version,
		ID:        id,
		Sender:    state.path,
		Receiver:  req.Target,
		Interface: req.Interface,
		Instance:  req.Instance,
		Method:    req.Method,
		Params:    req.Params,
		Expires:   req.Expires,
	}
	if validateErr := m.Validate(); validateErr != nil {
		return fractal.RES{}, validateErr
	}
	payload, err := json.Marshal(m)
	if err != nil {
		return fractal.RES{}, fmt.Errorf("encode the request: %w", err)
	}
	answers, err := n.await(id)
	if err != nil {
		return fractal.RES{}, err
	}
	defer n.forget(id)
	dispatch := Dispatch{Kind: fractal.KindRPC, Payload: payload, Child: child, ID: id}
	if sendErr := n.send(ctx, dispatch); sendErr != nil {
		return fractal.RES{}, fmt.Errorf("send the request: %w", sendErr)
	}
	return n.wait(ctx, answers, req.Expires)
}

// Desire asks a node below this one to change property values. It returns
// after the transport has accepted the DES message. It refuses a target the
// way Call does. A DES has no answer, and the transport or the receiver may
// drop it. Read the shadow of the target to see whether the values changed.
func (n *Node) Desire(ctx context.Context, req DesireRequest) error {
	state := n.state.Load()
	if state == nil {
		return ErrNoNodePath
	}
	if !state.path.IsAncestorOf(req.Target) {
		return fmt.Errorf("%s: %w", req.Target, ErrNotBelow)
	}
	child, ok := n.nextHop(state, req.Target)
	if !ok {
		return &fractal.Error{Code: fractal.CodeNodeNotFound, Message: msgNotFound}
	}
	m := fractal.DES{
		Version:   fractal.Version,
		Receiver:  req.Target,
		Interface: req.Interface,
		Instance:  req.Instance,
		Props:     req.Props,
	}
	if err := m.Validate(); err != nil {
		return err
	}
	payload, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("encode the request: %w", err)
	}
	if sendErr := n.send(ctx, Dispatch{Kind: fractal.KindDES, Payload: payload, Child: child}); sendErr != nil {
		return fmt.Errorf("send the request: %w", sendErr)
	}
	return nil
}

// PendingCalls returns the number of calls that wait for an answer.
func (n *Node) PendingCalls() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.pending)
}

// wait blocks until the answer arrives, ctx ends, or the request has expired
// and callGrace has passed. The grace gives a CodeExpired answer time to arrive.
func (n *Node) wait(ctx context.Context, answers <-chan fractal.RES, expires fractal.Timestamp) (fractal.RES, error) {
	deadline := expires.Time().Add(n.callGrace)
	left := max(deadline.Sub(n.clock.Now()), 0)
	select {
	case res := <-answers:
		return res, nil
	case <-ctx.Done():
		return fractal.RES{}, ctx.Err()
	case <-n.clock.After(left):
		return fractal.RES{}, ErrCallExpired
	}
}

// await adds one call to the pending map and returns the channel that receives
// its answer. It returns ErrTooManyCalls when the map has maxPending entries.
func (n *Node) await(id fractal.RPCID) (<-chan fractal.RES, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.pending) >= n.maxPending {
		return nil, ErrTooManyCalls
	}
	answers := make(chan fractal.RES, 1)
	n.pending[id] = answers
	return answers, nil
}

// forget removes one call from the pending map.
func (n *Node) forget(id fractal.RPCID) {
	n.mu.Lock()
	defer n.mu.Unlock()
	delete(n.pending, id)
}

// resolve gives an answer to the call that waits for its id. It returns false
// when no call waits. It never blocks.
func (n *Node) resolve(m fractal.RES) bool {
	n.mu.Lock()
	answers, ok := n.pending[m.ID]
	n.mu.Unlock()
	if !ok {
		return false
	}
	select {
	case answers <- m:
		return true
	default:
		return false
	}
}

// discardPending empties the pending map after the Node Path changed, because
// the answers are addressed to the old path. It also resets the dedup stores.
func (n *Node) discardPending(ctx context.Context, old, current fractal.NodePath) {
	n.mu.Lock()
	waiting := len(n.pending)
	n.pending = make(map[fractal.RPCID]chan fractal.RES)
	n.mu.Unlock()
	if resettable, ok := n.executed.(Resetter); ok {
		resettable.Reset()
	}
	if resettable, ok := n.processed.(Resetter); ok {
		resettable.Reset()
	}
	n.log.WarnContext(ctx, "the node took a different node path",
		"was", old, "is", current, "discarded", waiting)
}
