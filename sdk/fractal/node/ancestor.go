package node

import (
	"context"
	"encoding/json"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	"github.com/prusa3d/prusa-connect/sdk/fractal/schema"
	"github.com/prusa3d/prusa-connect/sdk/fractal/shadow"
)

// fullReportTimeout bounds the Owner.Snapshot calls of one full state report,
// for an owner that watches its context.
const fullReportTimeout = 10 * time.Second

// ParentConnected tells the node that the link to the node above is up. The
// transport calls it. The node sends an ARBR, the request for the full state
// of the nodes above, as soon as Config.ARBRMinInterval allows.
func (n *Node) ParentConnected(ctx context.Context) {
	n.log.InfoContext(ctx, "the link to the node above is up")
	n.parentLinkUp.Store(true)
	n.parentLinks.Add(1)
	signal(n.upkeepDue)
}

// ParentLost tells the node that the link to the node above is down. The node
// marks its whole ancestor shadow stale and sends an ALOS to every node
// directly below. The transport must call it only for a link that has been
// up.
func (n *Node) ParentLost(ctx context.Context) {
	n.parentLinkUp.Store(false)
	path, ok := n.pathIfSet()
	if !ok {
		n.log.WarnContext(ctx, "the link to the node above went down before this node had a node path")
		return
	}
	changed := n.ancestors.MarkStaleAbove(path)
	n.log.InfoContext(ctx, "the link to the node above is down", "stale", len(changed))
	payload, err := json.Marshal(fractal.ALOS{Version: fractal.Version, Sender: path})
	if err != nil {
		n.log.ErrorContext(ctx, "encode alos", "error", err)
		return
	}
	n.fanOutDown(ctx, fractal.KindALOS, payload)
}

// deliverAREP writes a report from a node above to the ancestor shadow and
// forwards it down, also for an interface this node does not keep. It drops
// an AREP older than the stored one.
func (n *Node) deliverAREP(ctx context.Context, state *pathState, in Inbound) {
	m, ok := decode[fractal.AREP](ctx, n, in)
	if !ok {
		return
	}
	if !m.Sender.IsAncestorOf(state.path) {
		n.drop(ctx, in, DropSenderInvalid)
		return
	}
	result := n.ancestors.ApplyREP(fractal.REP(m))
	if result.Outcome == shadow.OutcomeDiscarded {
		n.metrics.Drop(fractal.KindAREP, DropStaleReport)
		n.metrics.Message(fractal.KindAREP, OutcomeDropped)
		return
	}
	n.fanOutDown(ctx, fractal.KindAREP, in.Payload)
	n.metrics.Message(fractal.KindAREP, OutcomeForwarded)
}

// deliverALOS marks every node above the sender of an ALOS stale in the
// ancestor shadow, not the sender itself, and forwards the message down.
func (n *Node) deliverALOS(ctx context.Context, state *pathState, in Inbound) {
	m, ok := decode[fractal.ALOS](ctx, n, in)
	if !ok {
		return
	}
	if !m.Sender.IsAncestorOf(state.path) {
		n.drop(ctx, in, DropSenderInvalid)
		return
	}
	changed := n.ancestors.MarkStaleAbove(m.Sender)
	n.log.InfoContext(ctx, "a node above lost the node above it", "sender", m.Sender, "stale", len(changed))
	n.fanOutDown(ctx, fractal.KindALOS, in.Payload)
	n.metrics.Message(fractal.KindALOS, OutcomeForwarded)
}

// deliverARBR handles an ARBR from a node below. Every ARBR within one window
// gets one answer, when the window closes.
func (n *Node) deliverARBR(ctx context.Context, in Inbound) {
	if _, ok := decode[fractal.ARBR](ctx, n, in); !ok {
		return
	}
	n.arbrPayload.Store(&in.Payload)
	signal(n.arbrAsked)
	n.metrics.Message(fractal.KindARBR, OutcomeProcessed)
}

// upkeep is the goroutine that owns the ARBR timers. window collects the ARBR
// messages from below, and bound delays the next ARBR of this node's own.
// The node owes an ARBR for every Node Path and after every ParentConnected.
// It never calls an owner, so a slow owner cannot delay the timers.
func (n *Node) upkeep(ctx context.Context) {
	defer n.wg.Done()
	var window, bound <-chan time.Time
	var sentFor, boundFor *pathState
	var sentAfter uint64
	for {
		state, links := n.state.Load(), n.parentLinks.Load()
		if state != boundFor {
			bound = nil
		}
		owed := state != sentFor || links != sentAfter
		if owed && bound == nil && n.parentLinkUp.Load() && n.sendARBR(ctx, state) {
			sentFor, sentAfter, boundFor = state, links, state
			bound = n.clock.After(n.arbrMinInterval)
		}
		select {
		case <-ctx.Done():
			return
		case <-n.arbrAsked:
			if window == nil {
				window = n.clock.After(n.arbrWindow)
			}
		case <-n.upkeepDue:
		case <-window:
			window = nil
			signal(n.windowClosed)
		case <-bound:
			bound = nil
		}
	}
}

// answerer is the goroutine that answers a closed window. Windows that close
// while an answer runs get one more answer together.
func (n *Node) answerer(ctx context.Context) {
	defer n.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case <-n.windowClosed:
			n.answerARBR(ctx)
		}
	}
}

// answerARBR answers every ARBR one window collected. It must send its own
// full report down first, then forward the last ARBR that arrived, unchanged.
func (n *Node) answerARBR(ctx context.Context) {
	reportCtx, cancel := context.WithTimeout(ctx, fullReportTimeout)
	n.reportAll(reportCtx, schema.ExposureDownward)
	cancel()
	if state := n.state.Load(); state != nil && state.hasParent {
		n.sendOrLog(ctx, Dispatch{Kind: fractal.KindARBR, Payload: *n.arbrPayload.Load()})
	}
}

// sendARBR sends a new ARBR to the node above. It returns false, and sends
// nothing, while the node has no Node Path and on a root.
func (n *Node) sendARBR(ctx context.Context, state *pathState) bool {
	if state == nil || !state.hasParent {
		return false
	}
	payload, err := json.Marshal(fractal.ARBR{Version: fractal.Version})
	if err != nil {
		n.log.ErrorContext(ctx, "encode arbr", "error", err)
		return false
	}
	n.sendOrLog(ctx, Dispatch{Kind: fractal.KindARBR, Payload: payload})
	return true
}

// signal wakes the goroutine that reads ch and never blocks. Signals sent
// before the goroutine reads one count as one.
func signal(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}

// ForgetAncestors discards the whole ancestor shadow. A node whose admission
// was revoked must call it as soon as it learns of the revocation. From then
// on an AREP creates nothing in the ancestor shadow, until SetPath gives the
// node a different Node Path.
func (n *Node) ForgetAncestors(ctx context.Context) {
	removed := n.ancestors.Rebase("")
	n.log.InfoContext(ctx, "the admission was revoked, so the node forgot the nodes above", "removed", len(removed))
}
