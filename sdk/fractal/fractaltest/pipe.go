package fractaltest

import (
	"context"
	"sync"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	"github.com/prusa3d/prusa-connect/sdk/fractal/node"
)

// Engine is the receiving end of a transport: the transport calls it for every
// message and for every change of a link. A *node.Node is an Engine. A test
// that checks only the transport uses a Recorder.
type Engine interface {
	// Deliver receives one message from the transport.
	Deliver(ctx context.Context, in node.Inbound)
	// ChildConnected tells the engine that the link to the node below with
	// this Node ID is up.
	ChildConnected(ctx context.Context, id fractal.NodeID)
	// ChildLost tells the engine that the link to the node below with this
	// Node ID is down.
	ChildLost(ctx context.Context, id fractal.NodeID)
	// ParentConnected tells the engine that the link to the node above is up.
	ParentConnected(ctx context.Context)
	// ParentLost tells the engine that the link to the node above went down
	// after it was up.
	ParentLost(ctx context.Context)
}

// Stats has the message counters of one direction of one link.
type Stats struct {
	// Sent counts the messages the sending node passed to the link.
	Sent int
	// Delivered counts the calls of Deliver on the receiving engine. A second
	// delivery of the same message counts again.
	Delivered int
	// Redelivered counts the deliveries of a durable message that the link had
	// delivered before and the receiver had not acknowledged.
	Redelivered int
	// Dropped counts the best-effort messages the link dropped because it was
	// down.
	Dropped int
	// Deduplicated counts the durable messages the link did not deliver,
	// because it had seen their id inside the deduplication window.
	Deduplicated int
}

// item is one message in the queue of a pipe. deliveries counts how often the
// pipe delivered it.
type item struct {
	dispatch   node.Dispatch
	deliveries int
	durable    bool
}

// pipe is one direction of one link. It has the queue of messages, the durable
// message that is delivered and not yet acknowledged (inflight), and the ids
// of the durable messages it has seen (sent).
//
// The pipe delivers a best-effort message while the link is up and drops it
// while the link is down. A durable message waits in the queue while the link
// is down. After the pipe delivers a durable message, it delivers no other
// message until the receiver acknowledges that one. fractal-nats.md sets the
// same limit for durable messages with a MaxAckPending of 1.
type pipe struct {
	engine   Engine
	observer func(Received)
	clock    node.Clock
	wake     chan struct{}
	sent     map[fractal.RPCID]time.Time
	from     fractal.NodeID
	queue    []*item
	inflight *item
	window   time.Duration
	stats    Stats
	mu       sync.Mutex
	up       bool
}

// newPipe builds one direction of a link. The pipe sets Inbound.From to from
// on every message it delivers. from is the Node ID of the node below for the
// direction that goes up, and empty for the direction that goes down.
func newPipe(from fractal.NodeID, clock node.Clock, window time.Duration) *pipe {
	return &pipe{
		clock:  clock,
		wake:   make(chan struct{}, 1),
		sent:   make(map[fractal.RPCID]time.Time),
		from:   from,
		window: window,
	}
}

// attach sets the engine the pipe delivers to, and wakes the pump so that
// queued messages go to it.
func (p *pipe) attach(e Engine) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.engine = e
	p.signal()
}

// send puts one message in the queue. It drops a durable message whose id the
// pipe saw inside the window, and a best-effort message sent while the link is
// down.
func (p *pipe) send(d node.Dispatch, durable bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stats.Sent++
	if durable && p.duplicate(d.ID) {
		p.stats.Deduplicated++
		return
	}
	if !durable && !p.up {
		p.stats.Dropped++
		return
	}
	p.queue = append(p.queue, &item{dispatch: d, durable: durable})
	p.signal()
}

// duplicate reports whether a message with this id was sent inside the
// deduplication window. When it was not, duplicate records the id with the
// current time. An empty id is never a duplicate: only a method invocation and
// its answer have an id.
func (p *pipe) duplicate(id fractal.RPCID) bool {
	if id == "" {
		return false
	}
	now := p.clock.Now()
	if at, ok := p.sent[id]; ok && now.Sub(at) <= p.window {
		return true
	}
	p.sent[id] = now
	return false
}

// signal wakes the pump. The channel has room for one value, so many signals
// before the pump runs cause one wake-up.
func (p *pipe) signal() {
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

// setUp marks this direction of the link as up or down. When the link goes
// down, setUp drops every best-effort message in the queue. It also clears
// inflight. The unacknowledged durable message is still first in the queue, so
// the pump delivers it again when the link returns.
func (p *pipe) setUp(up bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.up = up
	if up {
		p.signal()
		return
	}
	p.inflight = nil
	kept := p.queue[:0]
	for _, it := range p.queue {
		if it.durable {
			kept = append(kept, it)
			continue
		}
		p.stats.Dropped++
	}
	p.queue = kept
}

// pump delivers the queued messages each time signal wakes it, until ctx ends.
// It stops after a durable message and continues when acknowledge wakes it.
func (p *pipe) pump(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-p.wake:
		}
		for {
			it, engine, ok := p.take()
			if !ok {
				break
			}
			p.deliver(ctx, engine, it)
			if it.durable {
				break
			}
		}
	}
}

// take returns the next message to deliver and the engine to deliver it to.
// The last result is false when there is nothing to deliver now. A durable
// message stays first in the queue until acknowledge removes it, so no later
// message is delivered before it.
func (p *pipe) take() (*item, Engine, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.up || p.inflight != nil || len(p.queue) == 0 || p.engine == nil {
		return nil, nil, false
	}
	it := p.queue[0]
	if it.durable {
		p.inflight = it
	} else {
		p.queue = p.queue[1:]
	}
	it.deliveries++
	p.stats.Delivered++
	if it.deliveries > 1 {
		p.stats.Redelivered++
	}
	return it, p.engine, true
}

// deliver calls Deliver on the receiving engine with one message. A durable
// message gets an Ack function. The function set by watch sees the message
// first.
func (p *pipe) deliver(ctx context.Context, engine Engine, it *item) {
	in := node.Inbound{Kind: it.dispatch.Kind, Payload: it.dispatch.Payload, From: p.from}
	if it.durable {
		in.Ack = func(context.Context) error {
			p.acknowledge(it)
			return nil
		}
	}
	if observer := p.watcher(); observer != nil {
		observer(Received{Payload: in.Payload, From: in.From, Kind: in.Kind})
	}
	engine.Deliver(ctx, in)
}

// watch sets a function the pipe calls for every delivery.
func (p *pipe) watch(fn func(Received)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.observer = fn
}

// watcher returns the function set by watch.
func (p *pipe) watcher() func(Received) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.observer
}

// acknowledge removes a delivered durable message from the queue and wakes the
// pump for the next message.
func (p *pipe) acknowledge(it *item) {
	p.mu.Lock()
	if len(p.queue) > 0 && p.queue[0] == it {
		p.queue = p.queue[1:]
	}
	if p.inflight == it {
		p.inflight = nil
	}
	p.signal()
	p.mu.Unlock()
}

// snapshot returns a copy of the counters.
func (p *pipe) snapshot() Stats {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.stats
}

// waiting counts the messages still in the queue.
func (p *pipe) waiting() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.queue)
}
