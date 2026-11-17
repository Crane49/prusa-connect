package natsbind

import (
	"context"
	"errors"
	"fmt"

	"github.com/nats-io/nats.go"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	fractalnode "github.com/prusa3d/prusa-connect/sdk/fractal/node"
)

// SendBestEffort publishes a message over core NATS, which does not store it.
// The engine uses it for state reports, requested property changes, connection
// loss messages, requests for a full state report, and AREP, ALOS and ARBR,
// the same three in the other direction. NATS drops a message
// that is sent while the connection is down, and the sender gets no error.
func (b *Binding) SendBestEffort(_ context.Context, d fractalnode.Dispatch) error {
	subject, err := subjectOf(d)
	if err != nil {
		return err
	}
	if d.Down() {
		l, ok := b.linkOf(d.Child)
		if !ok {
			return fmt.Errorf("%s: %w", d.Child, ErrNoLink)
		}
		l.mu.Lock()
		conn := l.conn
		l.mu.Unlock()
		if conn == nil {
			return fmt.Errorf("%s: %w", d.Child, ErrNoLink)
		}
		if pubErr := conn.Publish(subject, d.Payload); pubErr != nil {
			return fmt.Errorf("publish %s: %w", subject, pubErr)
		}
		return nil
	}
	b.mu.Lock()
	conn := b.local
	b.mu.Unlock()
	if conn == nil {
		return ErrNotStarted
	}
	if b.joiner == nil {
		return ErrNoUplink
	}
	if pubErr := conn.Publish(subject, d.Payload); pubErr != nil {
		return fmt.Errorf("publish %s: %w", subject, pubErr)
	}
	return nil
}

// SendDurable publishes a method invocation or a result to a JetStream stream,
// which keeps it until the next node has read and acknowledged it.
//
// It returns after JetStream has confirmed that it stored the message. Only
// then may the engine acknowledge the message it is forwarding. If the engine
// acknowledged first and the node stopped before the publish, the message
// would be lost.
//
// The Fractal id of the message is sent as the Nats-Msg-Id header. JetStream
// stores a message with the same id only once within the deduplication window.
func (b *Binding) SendDurable(ctx context.Context, d fractalnode.Dispatch) error {
	subject, err := subjectOf(d)
	if err != nil {
		return err
	}
	opts := []nats.PubOpt{nats.Context(ctx)}
	if d.ID != "" {
		opts = append(opts, nats.MsgId(string(d.ID)))
	}
	if d.Down() {
		l, ok := b.linkOf(d.Child)
		if !ok {
			return fmt.Errorf("%s: %w", d.Child, ErrNoLink)
		}
		return l.publishDurable(subject, d.Payload, opts)
	}
	b.mu.Lock()
	js := b.localJS
	b.mu.Unlock()
	switch {
	case js == nil:
		return ErrNotStarted
	case b.joiner == nil:
		return ErrNoUplink
	}
	if _, pubErr := js.Publish(subject, d.Payload, opts...); pubErr != nil {
		return fmt.Errorf("publish %s: %w", subject, pubErr)
	}
	return nil
}

// ErrNoSubject means the engine sent a message type in a direction that has
// no subject, for example a state report downward.
var ErrNoSubject = errors.New("the message type has no subject in that direction")

// subjectOf returns the subject for one message. The second token of a
// subject is the
// direction, "up" or "down". Every downward subject ends with the Node ID of
// the node below.
func subjectOf(d fractalnode.Dispatch) (string, error) {
	if d.Down() {
		switch d.Kind {
		case fractal.KindRPC:
			return SubjectRPC(d.Child), nil
		case fractal.KindDES:
			return SubjectDES(d.Child), nil
		case fractal.KindRBR:
			return SubjectRBR(d.Child), nil
		case fractal.KindAREP:
			return SubjectAREP(d.Child), nil
		case fractal.KindALOS:
			return SubjectALOS(d.Child), nil
		case fractal.KindREP, fractal.KindRES, fractal.KindLOS, fractal.KindARBR:
			return "", fmt.Errorf("%s downward: %w", d.Kind, ErrNoSubject)
		}
		return "", fmt.Errorf("%s: %w", d.Kind, ErrNoSubject)
	}
	switch d.Kind {
	case fractal.KindREP:
		return SubjectREP, nil
	case fractal.KindLOS:
		return SubjectLOS, nil
	case fractal.KindRES:
		return SubjectRES, nil
	case fractal.KindARBR:
		return SubjectARBR, nil
	case fractal.KindRPC, fractal.KindDES, fractal.KindRBR, fractal.KindAREP, fractal.KindALOS:
		return "", fmt.Errorf("%s upward: %w", d.Kind, ErrNoSubject)
	}
	return "", fmt.Errorf("%s: %w", d.Kind, ErrNoSubject)
}

// Executed returns the stored result message of the method invocation with
// this id. ok is false when this node has no record of it.
func (b *Binding) Executed(ctx context.Context, id fractal.RPCID) (res []byte, ok bool, err error) {
	d, err := b.dedupStore()
	if err != nil {
		return nil, false, err
	}
	return d.Executed(ctx, id)
}

// RecordExecuted stores the result message of the method invocation with this
// id.
func (b *Binding) RecordExecuted(ctx context.Context, id fractal.RPCID, res []byte) error {
	d, err := b.dedupStore()
	if err != nil {
		return err
	}
	return d.RecordExecuted(ctx, id, res)
}

// Processed reports whether this node has already processed the result with
// this id.
func (b *Binding) Processed(ctx context.Context, id fractal.RPCID) (bool, error) {
	d, err := b.dedupStore()
	if err != nil {
		return false, err
	}
	return d.Processed(ctx, id)
}

// RecordProcessed records that this node processed the result with this id.
func (b *Binding) RecordProcessed(ctx context.Context, id fractal.RPCID) error {
	d, err := b.dedupStore()
	if err != nil {
		return err
	}
	return d.RecordProcessed(ctx, id)
}

// Reset deletes every entry of both deduplication buckets. Before Start it
// does nothing.
func (b *Binding) Reset() {
	if d, err := b.dedupStore(); err == nil {
		d.Reset()
	}
}

// dedupStore returns the deduplication buckets, or ErrNotStarted before Start
// has created them.
func (b *Binding) dedupStore() (*kvDedup, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.dedup == nil {
		return nil, ErrNotStarted
	}
	return b.dedup, nil
}
