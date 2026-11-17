package main

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/prusa3d/fractal-link/natsbind"

	"github.com/prusa3d/prusa-connect/sdk/fault"
	"github.com/prusa3d/prusa-connect/sdk/fractal"
	"github.com/prusa3d/prusa-connect/sdk/linkapi"
)

// link implements linkapi.Link, the methods the host calls on this module:
// Status, Approve and Revoke.
//
// Once the transport is served, Status reads it at each call. So the last
// heartbeat of a server below is the latest one, not the one at the last
// change of status. Before that it returns the last status stored. It sends
// nothing over the network, so it answers inside the time limit the host sets
// on each call.
type link struct {
	mu      sync.RWMutex
	reading linkapi.Status
	binding decider
	count   *counters
}

// decider is the part of the transport that Status, Approve, Reject and Revoke
// call. Status reads the transport from memory, Approve admits one server
// below this one, Reject removes one that waits for a decision, and Revoke
// removes the admission of one that joined.
type decider interface {
	Status() natsbind.Status
	Approve(ctx context.Context, id fractal.NodeID) error
	Reject(ctx context.Context, id fractal.NodeID) error
	Revoke(ctx context.Context, id fractal.NodeID) error
}

// newLink returns a link with the empty status: no node id, no server above and
// no server below. Status returns it until the transport reports the first
// real one.
func newLink() *link {
	return &link{reading: linkapi.Status{}}
}

// Status returns the status of the transport, or the last one stored before
// the transport is served.
func (l *link) Status(context.Context) (linkapi.Status, error) {
	l.mu.RLock()
	binding, count, stored := l.binding, l.count, l.reading
	l.mu.RUnlock()

	if binding == nil {
		return stored, nil
	}

	return currentReading(binding.Status(), count), nil
}

// currentReading converts one status of the transport, with the counters.
func currentReading(status natsbind.Status, count *counters) linkapi.Status {
	current := reading(status, admitsOf(status))
	current.Counters = count.reading()

	return current
}

// Approve admits one server below this one. Every refusal carries a registered
// code, so the links page of the host shows the code and this module's English
// sentence.
func (l *link) Approve(ctx context.Context, params linkapi.NodeParams) error {
	binding := l.decider()
	if binding == nil {
		return notServing()
	}

	if err := binding.Approve(ctx, fractal.NodeID(params.ID)); err != nil {
		return refusal(params.ID, err)
	}

	return nil
}

// Reject removes one server that waits for a decision. Its name is then free,
// so another machine can ask for it. It returns the same errors as Approve.
func (l *link) Reject(ctx context.Context, params linkapi.NodeParams) error {
	binding := l.decider()
	if binding == nil {
		return notServing()
	}

	if err := binding.Reject(ctx, fractal.NodeID(params.ID)); err != nil {
		return refusal(params.ID, err)
	}

	return nil
}

// Revoke removes the admission of one server below this one. It returns the
// same errors as Approve.
func (l *link) Revoke(ctx context.Context, params linkapi.NodeParams) error {
	binding := l.decider()
	if binding == nil {
		return notServing()
	}

	if err := binding.Revoke(ctx, fractal.NodeID(params.ID)); err != nil {
		return refusal(params.ID, err)
	}

	return nil
}

// decider returns the transport, or nil while build has not finished.
func (l *link) decider() decider {
	l.mu.RLock()
	defer l.mu.RUnlock()

	return l.binding
}

// serve stores the transport that Status, Approve and Revoke call, and the
// counters Status reads. build calls it once the transport exists.
func (l *link) serve(binding decider, count *counters) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.binding = binding
	l.count = count
}

// update replaces the stored status. The OnChange callback of the transport
// calls it.
func (l *link) update(reading linkapi.Status) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.reading = reading
}

// reportedBy sends a new status to the host with link.changed. The module
// calls it after a join, a refused join, a lost or opened connection, and a
// change of admission. The host publishes an event from it. A report that
// fails is not sent again: the host calls Status whenever a person opens the
// links page, so the page still shows the current status.
type reportedBy interface {
	Changed(ctx context.Context, params linkapi.ChangedParams) error
}

// reading converts the status of the transport into a linkapi.Status.
func reading(status natsbind.Status, admits linkapi.Admits) linkapi.Status {
	out := linkapi.Status{
		NodeID:    linkapi.NodeID(status.NodeID),
		NodePath:  linkapi.NodePath(status.NodePath),
		PublicKey: string(status.Identity),
		Accepting: status.Accepting,
		Admits:    admits,
		Uplink:    uplinkOf(status.Uplink),
	}

	for _, below := range status.Nodes {
		out.Nodes = append(out.Nodes, linkapi.Node{
			ID:        linkapi.NodeID(below.ID),
			Path:      linkapi.NodePath(below.Path),
			Admission: admission(below.Admission),
			PublicKey: string(below.Identity),
			Connected: below.Connected,
			LastSeen:  below.LastHeartbeat,
		})
	}

	for _, listener := range status.Listeners {
		out.Listeners = append(out.Listeners, linkapi.Listener{
			Protocol: listener.Protocol,
			Address:  listener.Address,
			Port:     listener.Port,
		})
	}

	return out
}

// uplinkOf converts the connection to the server above. It returns nil for a
// server whose settings name no server above it.
//
// A refusal carries two identifiers. Code is the protocol's own number, which
// the server above chose. FaultCode is this module's registered code, which the
// links page keys its own sentence by.
func uplinkOf(up natsbind.Uplink) *linkapi.Uplink {
	if up.State == natsbind.UplinkNotConfigured || up.State == "" {
		return nil
	}

	out := &linkapi.Uplink{
		Address:     up.Address,
		State:       linkapi.UplinkState(up.State),
		Reason:      up.Reason,
		Code:        int(up.Code),
		ConnectedAt: up.ConnectedAt,
	}

	if out.State == linkapi.UplinkDenied {
		denied := JoinDenied
		switch {
		case up.Refused:
			denied = GrantRefused
		case up.Unreadable:
			denied = RefusalUnreadable
		}

		out.FaultCode = denied.Code().String()

		if out.Reason == "" {
			out.Reason = denied.New("").Title()
		}
	}

	return out
}

// admission converts the admission of a server below this one: pending,
// admitted or revoked. A value it does not know becomes pending.
func admission(from natsbind.Admission) linkapi.Admission {
	switch from {
	case natsbind.AdmissionPending:
		return linkapi.AdmissionPending
	case natsbind.AdmissionAdmitted:
		return linkapi.AdmissionAdmitted
	case natsbind.AdmissionRevoked:
		return linkapi.AdmissionRevoked
	}

	return linkapi.AdmissionPending
}

// admitsOf returns how this server admits a server that asks to join. With
// ask_first it is linkapi.AdmitsExplicit: a person approves each new server.
// Without it, it is linkapi.AdmitsInherit: the verification mode alone
// decides. It is empty on a server that accepts none.
func admitsOf(status natsbind.Status) linkapi.Admits {
	if !status.Accepting {
		return ""
	}

	if status.AsksFirst {
		return linkapi.AdmitsExplicit
	}

	return linkapi.AdmitsInherit
}

// notServing is the refusal for a decision that arrived before the transport
// was built. It keeps linkapi.ErrNotReady as the cause, so the host still
// matches that error variable.
func notServing() error {
	return linkapi.Faulted(TransportUnavailable.
		New("this link has not finished starting").Wrap(linkapi.ErrNotReady))
}

// refusal converts an error of the transport into the refusal the host
// receives. A server this link has no record of gets NodeUnknown, and keeps
// linkapi.ErrNoNode as the cause so the host still answers the request as a
// node that is not there. Every other error gets DecisionRefused with the
// reason, which reaches this module's log and the host's problem document.
func refusal(id linkapi.NodeID, err error) error {
	if errors.Is(err, natsbind.ErrUnknownNode) {
		return linkapi.Faulted(NodeUnknown.New("this link holds no server under that id").
			With(fault.StringParam(paramID, id.String())).Wrap(linkapi.ErrNoNode))
	}

	return linkapi.Faulted(DecisionRefused.New("this link could not record the decision").
		With(fault.StringParam(paramID, id.String()), fault.StringParam(paramReason, err.Error())).
		Wrap(err))
}

// reportEvery sends one status to the host and waits at most budget for the
// answer. It ignores the error, because a host that does not serve
// link.changed refuses every report.
func reportEvery(ctx context.Context, to reportedBy, status linkapi.Status, budget time.Duration) {
	if to == nil {
		return
	}

	bounded, cancel := context.WithTimeout(ctx, budget)
	defer cancel()

	_ = to.Changed(bounded, linkapi.ChangedParams{Status: status})
}
