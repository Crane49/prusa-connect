package node

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	"github.com/prusa3d/prusa-connect/sdk/fractal/shadow"
)

// Deliver receives one message from the transport. A transport calls it for
// every message it reads, on the goroutine that read the message. It forwards
// a message before it returns and never calls an Owner, so a slow owner does
// not block the transport. A rejected message is dropped and acknowledged.
// When a queue is full the message is dropped without Ack.
func (n *Node) Deliver(ctx context.Context, in Inbound) {
	state := n.state.Load()
	if state == nil {
		n.drop(ctx, in, DropNoNodePath)
		return
	}
	if !directionOK(in.Kind, in.FromChild()) {
		n.drop(ctx, in, DropWrongDirection)
		return
	}
	switch in.Kind {
	case fractal.KindREP:
		n.deliverREP(ctx, state, in)
	case fractal.KindLOS:
		n.deliverLOS(ctx, state, in)
	case fractal.KindRBR:
		n.deliverRBR(ctx, in)
	case fractal.KindDES:
		n.deliverDES(ctx, state, in)
	case fractal.KindRPC:
		n.deliverRPC(ctx, state, in)
	case fractal.KindRES:
		n.deliverRES(ctx, state, in)
	case fractal.KindAREP:
		n.deliverAREP(ctx, state, in)
	case fractal.KindALOS:
		n.deliverALOS(ctx, state, in)
	case fractal.KindARBR:
		n.deliverARBR(ctx, in)
	default:
		n.drop(ctx, in, DropMalformed)
	}
}

// directionOK reports whether a message type arrived from the right side: an
// upward type from a node below, a downward type from the node above.
func directionOK(k fractal.Kind, fromChild bool) bool {
	switch k.Direction() {
	case fractal.Up:
		return fromChild
	case fractal.Down:
		return !fromChild
	default:
		return false
	}
}

// decode reads the payload of in as a message of type M and validates it,
// also for a message the node only forwards. On failure it drops the message.
func decode[M interface{ Validate() error }](ctx context.Context, n *Node, in Inbound) (M, bool) {
	var m M
	if err := json.Unmarshal(in.Payload, &m); err != nil {
		n.drop(ctx, in, DropMalformed)
		return m, false
	}
	if err := m.Validate(); err != nil {
		reason := DropMalformed
		if errors.Is(err, fractal.ErrUnsupportedVersion) {
			reason = DropUnsupportedVersion
		}
		n.drop(ctx, in, reason)
		return m, false
	}
	return m, true
}

// drop logs and counts a message the node rejects, and calls Ack. A drop for
// the version, the routing or the sender check is logged as an error.
func (n *Node) drop(ctx context.Context, in Inbound, reason DropReason) {
	level := slog.LevelWarn
	if reason == DropUnsupportedVersion || reason == DropNotRoutable || reason == DropSenderInvalid {
		level = slog.LevelError
	}
	n.log.Log(ctx, level, "message dropped", "kind", in.Kind, "reason", reason, "from", in.From)
	n.metrics.Drop(in.Kind, reason)
	n.metrics.Message(in.Kind, OutcomeDropped)
	n.ack(ctx, in)
}

// dropBusy logs and counts a message that found a full queue. It does not call
// Ack, so the transport delivers a durable message again.
func (n *Node) dropBusy(ctx context.Context, in Inbound) {
	n.log.WarnContext(ctx, "message dropped", "kind", in.Kind, "reason", DropBusy, "from", in.From)
	n.metrics.Drop(in.Kind, DropBusy)
	n.metrics.Message(in.Kind, OutcomeDropped)
}

// ack calls Inbound.Ack when the transport set it, and logs an error from it.
func (n *Node) ack(ctx context.Context, in Inbound) {
	if in.Ack == nil {
		return
	}
	if err := in.Ack(ctx); err != nil {
		n.log.ErrorContext(ctx, "acknowledge a message", "kind", in.Kind, "error", err)
	}
}

// senderOK reports whether sender is the node directly below whose link the
// message arrived on, or a node under it.
func (n *Node) senderOK(state *pathState, from fractal.NodeID, sender fractal.NodePath) bool {
	link, err := state.path.Child(from)
	if err != nil {
		return false
	}
	return link.Covers(sender)
}

// deliverREP writes a report to the shadow and forwards it to the node above.
// It does not forward a report the shadow discarded as too old.
func (n *Node) deliverREP(ctx context.Context, state *pathState, in Inbound) {
	m, ok := decode[fractal.REP](ctx, n, in)
	if !ok {
		return
	}
	if !n.senderOK(state, in.From, m.Sender) {
		n.drop(ctx, in, DropSenderInvalid)
		return
	}
	result := n.shadow.ApplyREP(m)
	if result.Outcome == shadow.OutcomeDiscarded {
		n.metrics.Drop(fractal.KindREP, DropStaleReport)
		n.metrics.Message(fractal.KindREP, OutcomeDropped)
		return
	}
	n.forwardUp(ctx, state, in)
}

// deliverLOS marks the node a LOS names, and every node under it, as stale in
// the shadow, and forwards the message to the node above.
func (n *Node) deliverLOS(ctx context.Context, state *pathState, in Inbound) {
	m, ok := decode[fractal.LOS](ctx, n, in)
	if !ok {
		return
	}
	if !n.senderOK(state, in.From, m.Sender) {
		n.drop(ctx, in, DropSenderInvalid)
		return
	}
	changed, err := n.shadow.ApplyLOS(m)
	if err != nil {
		n.drop(ctx, in, DropMalformed)
		return
	}
	n.log.InfoContext(ctx, "a node below lost its connection",
		"sender", m.Sender, "child", m.Child, "stale", len(changed))
	n.forwardUp(ctx, state, in)
}

// forwardUp sends a REP or a LOS to the node above, as the bytes that arrived.
// A root node sends nothing and counts the message as processed.
func (n *Node) forwardUp(ctx context.Context, state *pathState, in Inbound) {
	if !state.hasParent {
		n.metrics.Message(in.Kind, OutcomeProcessed)
		return
	}
	n.sendOrLog(ctx, Dispatch{Kind: in.Kind, Payload: in.Payload})
	n.metrics.Message(in.Kind, OutcomeForwarded)
}
