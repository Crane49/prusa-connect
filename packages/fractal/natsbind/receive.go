package natsbind

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
)

// The waiting times of pullRPC and pullRES.
const (
	// fetchWait is how long one Fetch waits for a message before drain asks
	// again.
	fetchWait = 2 * time.Second
	// bindRetry is how long pullRPC and pullRES wait before they try again to
	// bind to a consumer. The leafnode connection may be closed, or the node
	// at the other end may not have created the stream yet.
	bindRetry = 500 * time.Millisecond
)

// pullRPC reads the method invocations that the node above stores for this
// node, and passes them to the engine.
//
// The stream is in the JetStream domain of the node above. So this node
// creates and reads the consumer over the leafnode connection, with the $JS
// API prefix of that domain. It fetches one message at a time and
// acknowledges each, so the messages stay in order.
func (b *Binding) pullRPC(ctx context.Context) {
	own := b.cfg.NodeID
	for ctx.Err() == nil {
		parentDomain, ok := b.parentDomain()
		if !ok {
			waitFor(ctx, bindRetry)
			continue
		}
		sub, err := b.bindPull(ctx, pullTarget{
			domain:   parentDomain,
			stream:   rpcStreamName(own),
			durable:  rpcConsumerName(own),
			subject:  SubjectRPC(own),
			fromLink: false,
		})
		if err != nil {
			b.log.DebugContext(ctx, "the method consumer is not bound yet", "error", err)
			waitFor(ctx, bindRetry)
			continue
		}
		b.drain(ctx, sub, fractal.KindRPC, "")
		_ = sub.Unsubscribe() // The loop binds a new subscription, so an error here changes nothing.
	}
}

// pullRES reads the results that one node below stores for this node, and
// passes them to the engine.
//
// The stream is in the JetStream domain of the node below, and that node
// creates it. pullRES tries again every bindRetry until the stream exists,
// and then reads it over the leafnode connection.
func (b *Binding) pullRES(ctx context.Context, l *link) {
	for ctx.Err() == nil {
		l.mu.Lock()
		conn := l.conn
		l.mu.Unlock()
		if conn == nil {
			return
		}
		sub, err := b.bindPull(ctx, pullTarget{
			domain:   l.domain,
			stream:   resStreamName,
			durable:  resConsumerName(l.id),
			subject:  SubjectRES,
			fromLink: true,
			conn:     conn,
		})
		if err != nil {
			b.log.DebugContext(ctx, "the result consumer is not bound yet", "node", l.path, "error", err)
			waitFor(ctx, bindRetry)
			continue
		}
		b.drain(ctx, sub, fractal.KindRES, l.id)
		_ = sub.Unsubscribe() // The loop binds a new subscription, so an error here changes nothing.
	}
}

// pullTarget names one durable pull consumer, its stream and the JetStream
// domain of the stream. A nil conn means this node's own connection.
type pullTarget struct {
	conn     *nats.Conn
	domain   string
	stream   string
	durable  string
	subject  string
	fromLink bool
}

// bindPull creates the consumer when it does not exist and returns a pull
// subscription bound to it.
func (b *Binding) bindPull(ctx context.Context, t pullTarget) (*nats.Subscription, error) {
	conn := t.conn
	if conn == nil {
		b.mu.Lock()
		conn = b.local
		b.mu.Unlock()
	}
	if conn == nil {
		return nil, ErrNotStarted
	}
	js, err := conn.JetStream(nats.Domain(t.domain))
	if err != nil {
		return nil, fmt.Errorf("reach the jetstream of %s: %w", t.domain, err)
	}
	if ensureErr := ensureConsumer(ctx, js, t.stream, b.consumerConfig(t.durable, t.subject)); ensureErr != nil {
		return nil, ensureErr
	}
	sub, err := js.PullSubscribe(t.subject, t.durable, nats.Bind(t.stream, t.durable))
	if err != nil {
		return nil, fmt.Errorf("bind the consumer %s of %s: %w", t.durable, t.stream, err)
	}
	return sub, nil
}

// drain fetches one message at a time and passes it to Engine.Deliver. It
// returns when Fetch fails with an error other than a timeout, and the caller
// then binds again.
func (b *Binding) drain(ctx context.Context, sub *nats.Subscription, kind fractal.Kind, from fractal.NodeID) {
	for ctx.Err() == nil {
		msgs, err := fetchOne(ctx, sub)
		switch {
		case errors.Is(err, nats.ErrTimeout), errors.Is(err, context.DeadlineExceeded):
			continue
		case errors.Is(err, context.Canceled):
			return
		case err != nil:
			b.log.DebugContext(ctx, "a durable consumer stopped", "kind", kind, "error", err)
			return
		}
		for _, msg := range msgs {
			b.engine.Deliver(ctx, fractalnodeInbound(kind, msg.Data, from, ackOf(msg)))
		}
	}
}

// fetchOne waits up to fetchWait for one message. The context ends the wait
// at once, so Close does not wait for it. A message that came in as the
// context ended stays unacknowledged, and JetStream delivers it again.
func fetchOne(ctx context.Context, sub *nats.Subscription) ([]*nats.Msg, error) {
	fetchCtx, cancel := context.WithTimeout(ctx, fetchWait)
	defer cancel()
	msgs, err := sub.Fetch(1, nats.Context(fetchCtx))
	if err != nil {
		return nil, fmt.Errorf("fetch one message: %w", err)
	}
	return msgs, nil
}

// ackOf returns the function that acknowledges one JetStream message. The
// engine calls it after it has executed the message or stored it for the next
// node. If the node stops before that, JetStream delivers the message again,
// so the message is not lost.
func ackOf(msg *nats.Msg) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		if err := msg.Ack(nats.Context(ctx)); err != nil {
			return fmt.Errorf("acknowledge a durable message: %w", err)
		}
		return nil
	}
}

// parentDomain returns the JetStream domain of the node above, derived from
// this node's Node Path. ok is false when this node has no Node Path yet or is
// a root node.
func (b *Binding) parentDomain() (string, bool) {
	path := b.Path()
	if path == "" {
		return "", false
	}
	parent, ok := path.Parent()
	if !ok {
		return "", false
	}
	return Domain(parent), true
}

// waitFor blocks for d, or until the context ends.
func waitFor(ctx context.Context, d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}
