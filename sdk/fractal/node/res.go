package node

import (
	"context"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
)

// deliverRES handles a RES, the answer to an RPC. It queues a RES addressed to
// this node and forwards any other to the node above.
func (n *Node) deliverRES(ctx context.Context, state *pathState, in Inbound) {
	m, ok := decode[fractal.RES](ctx, n, in)
	if !ok {
		return
	}
	if m.Receiver != state.path {
		n.forwardRES(ctx, state, in, m)
		return
	}
	taken := n.submit(n.work, func(workCtx context.Context) { n.acceptRES(workCtx, m, in) })
	if !taken {
		n.dropBusy(ctx, in)
	}
}

// forwardRES sends a RES to the node above and then calls Ack. A root node
// logs an error and drops a RES for another node.
func (n *Node) forwardRES(ctx context.Context, state *pathState, in Inbound, m fractal.RES) {
	if !state.hasParent {
		n.log.ErrorContext(ctx, "a root node received an answer addressed to another node",
			"id", m.ID, "receiver", m.Receiver)
		n.drop(ctx, in, DropNoParent)
		return
	}
	dispatch := Dispatch{Kind: fractal.KindRES, Payload: in.Payload, ID: m.ID}
	if err := n.send(ctx, dispatch); err != nil {
		n.log.ErrorContext(ctx, "forward an answer", "id", m.ID, "error", err)
		n.metrics.Drop(fractal.KindRES, DropSendFailed)
		return
	}
	n.metrics.Message(fractal.KindRES, OutcomeForwarded)
	n.ack(ctx, in)
}

// acceptRES gives a RES to the Call that waits for its id. It drops a RES
// whose id is already in the ProcessedStore.
func (n *Node) acceptRES(ctx context.Context, m fractal.RES, in Inbound) {
	seen, err := n.processed.Processed(ctx, m.ID)
	if err != nil {
		n.log.ErrorContext(ctx, "read the record of a processed answer", "id", m.ID, "error", err)
		return
	}
	if seen {
		n.log.InfoContext(ctx, "an answer arrived again", "id", m.ID)
		n.drop(ctx, in, DropDuplicate)
		return
	}
	if recordProcessedErr := n.processed.RecordProcessed(ctx, m.ID); recordProcessedErr != nil {
		n.log.ErrorContext(ctx, "record a processed answer", "id", m.ID, "error", recordProcessedErr)
		return
	}
	if !n.resolve(m) {
		n.log.InfoContext(ctx, "an answer arrived and no call waits for it", "id", m.ID)
	}
	n.metrics.Message(fractal.KindRES, OutcomeProcessed)
	n.ack(ctx, in)
}
