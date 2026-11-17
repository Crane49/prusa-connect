package node

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	"github.com/prusa3d/prusa-connect/sdk/fractal/schema"
)

// deliverRBR handles an RBR, the request for a full state report. It signals
// the answering goroutine, because the answer must never be dropped.
func (n *Node) deliverRBR(ctx context.Context, in Inbound) {
	if _, ok := decode[fractal.RBR](ctx, n, in); !ok {
		return
	}
	n.rbrPayload.Store(&in.Payload)
	signal(n.rbrAsked)
}

// rbrAnswerer is the goroutine that answers an RBR. The node must report its
// own interfaces first, then forward the request down.
func (n *Node) rbrAnswerer(ctx context.Context) {
	defer n.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case <-n.rbrAsked:
			payload := *n.rbrPayload.Load()
			reportCtx, cancel := context.WithTimeout(ctx, fullReportTimeout)
			n.reportAll(reportCtx, schema.ExposureUpward)
			cancel()
			n.fanOutDown(ctx, fractal.KindRBR, payload)
			n.metrics.Message(fractal.KindRBR, OutcomeProcessed)
		}
	}
}

// reportAll sends a full state report for every interface this node owns and
// exposes in the direction dir. The timestamp is the time the state was read.
func (n *Node) reportAll(ctx context.Context, dir schema.Exposure) {
	for _, handle := range n.Interfaces() {
		if handle.Exposure()&dir == 0 {
			continue
		}
		if err := n.reportFull(ctx, handle, dir); err != nil {
			n.log.ErrorContext(ctx, "report the state of an owned interface",
				"interface", handle.name, "instance", handle.instance, "error", err)
		}
	}
}

// reportFull reads the state of one instance and sends it. It reads again
// while a change was reported during the read, so a full report never
// overwrites a newer value. The context ends the retries.
func (n *Node) reportFull(ctx context.Context, i *Interface, dir schema.Exposure) error {
	for {
		i.sendMu.Lock()
		before := i.reports
		i.sendMu.Unlock()
		props, at, err := i.owner.Snapshot(ctx)
		if err != nil {
			return fmt.Errorf("read the state: %w", err)
		}
		if at == 0 {
			at = fractal.TimestampOf(n.clock.Now())
		}
		i.sendMu.Lock()
		if i.reports == before {
			err = n.reportLocked(ctx, i, props, at, dir)
			i.sendMu.Unlock()
			return err
		}
		i.sendMu.Unlock()
		if ctxErr := ctx.Err(); ctxErr != nil {
			n.log.ErrorContext(ctx, "the full report is not sent: a change was reported during every read of the state",
				"interface", i.name, "instance", i.instance, "error", ctxErr)
			return nil
		}
	}
}

// fanOutDown sends the same encoded downward broadcast to every node directly
// below this one.
func (n *Node) fanOutDown(ctx context.Context, kind fractal.Kind, payload []byte) {
	for child := range n.admissions.Children() {
		n.sendOrLog(ctx, Dispatch{Kind: kind, Payload: payload, Child: child})
	}
}

// broadcastRBR builds a new RBR and sends it to every node directly below this
// one.
func (n *Node) broadcastRBR(ctx context.Context) {
	payload, err := json.Marshal(fractal.RBR{Version: fractal.Version})
	if err != nil {
		n.log.ErrorContext(ctx, "encode rbr", "error", err)
		return
	}
	n.fanOutDown(ctx, fractal.KindRBR, payload)
}

// sendRBR builds a new RBR and sends it to one node directly below this one.
func (n *Node) sendRBR(ctx context.Context, child fractal.NodeID) {
	payload, err := json.Marshal(fractal.RBR{Version: fractal.Version})
	if err != nil {
		n.log.ErrorContext(ctx, "encode rbr", "error", err)
		return
	}
	n.sendOrLog(ctx, Dispatch{Kind: fractal.KindRBR, Payload: payload, Child: child})
}

// deliverDES handles a DES, the request to change property values. It forwards
// the message toward the receiver, or queues it when the receiver is this node.
func (n *Node) deliverDES(ctx context.Context, state *pathState, in Inbound) {
	m, ok := decode[fractal.DES](ctx, n, in)
	if !ok {
		return
	}
	if m.Receiver != state.path {
		n.routeDown(ctx, state, in, m.Receiver)
		return
	}
	taken := n.submit(n.work, func(workCtx context.Context) { n.applyDES(workCtx, m) })
	if !taken {
		n.dropBusy(ctx, in)
	}
}

// routeDown forwards a DES toward its receiver. It drops the message when the
// receiver is not below this node or the next node is not admitted.
func (n *Node) routeDown(ctx context.Context, state *pathState, in Inbound, receiver fractal.NodePath) {
	child, ok := n.nextHop(state, receiver)
	if !ok {
		n.drop(ctx, in, DropNotRoutable)
		return
	}
	n.sendOrLog(ctx, Dispatch{Kind: in.Kind, Payload: in.Payload, Child: child})
	n.metrics.Message(in.Kind, OutcomeForwarded)
	n.ack(ctx, in)
}

// nextHop returns the node directly below this one that a message for receiver
// goes to. ok is false when receiver is not below or that node is not admitted.
func (n *Node) nextHop(state *pathState, receiver fractal.NodePath) (fractal.NodeID, bool) {
	child, ok := state.path.NextHop(receiver)
	if !ok {
		return "", false
	}
	if !n.admissions.Admitted(child) {
		return "", false
	}
	return child, true
}

// applyDES validates a requested property change and passes it to Owner.Desire.
// A rejected request changes nothing and sends no report.
func (n *Node) applyDES(ctx context.Context, m fractal.DES) {
	handle, ok := n.lookup(m.Interface, m.Instance)
	if !ok {
		n.log.WarnContext(ctx, "a requested property change names an interface this node does not own",
			"interface", m.Interface, "instance", m.Instance)
		n.metrics.Drop(fractal.KindDES, DropRejected)
		n.metrics.Message(fractal.KindDES, OutcomeDropped)
		return
	}
	if handle.def != nil {
		if err := handle.def.ValidateDES(m.Props); err != nil {
			n.log.WarnContext(ctx, "a requested property change failed validation",
				"interface", m.Interface, "instance", m.Instance, "error", err)
			n.metrics.Drop(fractal.KindDES, DropRejected)
			n.metrics.Message(fractal.KindDES, OutcomeDropped)
			return
		}
	}
	changed, err := handle.owner.Desire(ctx, m.Props)
	if err != nil {
		n.log.InfoContext(ctx, "the owner refused a requested property change",
			"interface", m.Interface, "instance", m.Instance, "error", err)
		n.metrics.Drop(fractal.KindDES, DropRejected)
		n.metrics.Message(fractal.KindDES, OutcomeDropped)
		return
	}
	n.metrics.Message(fractal.KindDES, OutcomeProcessed)
	if len(changed) == 0 {
		return
	}
	if nowErr := n.report(
		ctx,
		handle,
		changed,
		fractal.TimestampOf(n.clock.Now()),
		schema.ExposureBoth,
	); nowErr != nil {
		n.log.ErrorContext(ctx, "report an accepted property change",
			"interface", m.Interface, "instance", m.Instance, "error", nowErr)
	}
}
