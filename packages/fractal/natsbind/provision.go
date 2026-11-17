package natsbind

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nats.go"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	fractalnode "github.com/prusa3d/prusa-connect/sdk/fractal/node"
)

// maxAckPending is how many delivered messages a consumer may have without an
// acknowledgement. With one, the consumer delivers the next message only after
// the reader acknowledged the last, so the messages of one link arrive in
// order. The protocol fixes the value, and it is not a configuration key.
const maxAckPending = 1

// prusaConnect connects this node to its own NATS server, in the node's own
// account. The user has no subject restrictions, because no other node
// connects as a client in this account. The restricted users are in the
// bootstrap account and in the account of each link.
func (b *Binding) prusaConnect() error {
	option, err := b.accounts.connectOption(accountLocal, jwt.UserPermissionLimits{})
	if err != nil {
		return fmt.Errorf("mint this node's own user: %w", err)
	}
	conn, err := b.dialSelf("fractal-"+string(b.cfg.NodeID), option)
	if err != nil {
		return fmt.Errorf("connect to this node's own server: %w", err)
	}
	js, err := conn.JetStream()
	if err != nil {
		conn.Close()
		return fmt.Errorf("reach this node's own jetstream: %w", err)
	}
	b.mu.Lock()
	b.local, b.localJS = conn, js
	b.mu.Unlock()
	return nil
}

// dialSelf opens one client connection to this node's own NATS server. The
// connection reconnects without limit.
func (b *Binding) dialSelf(name string, option nats.Option) (*nats.Conn, error) {
	b.mu.Lock()
	srv := b.srv
	b.mu.Unlock()
	if srv == nil {
		return nil, ErrNotStarted
	}
	opts := []nats.Option{
		nats.Name(name),
		option,
		nats.MaxReconnects(-1),
		nats.ReconnectWait(reconnectWait),
		nats.ErrorHandler(func(_ *nats.Conn, sub *nats.Subscription, err error) {
			subject := ""
			if sub != nil {
				subject = sub.Subject
			}
			b.log.Warn("a connection of this node reported an error",
				"name", name, "subject", subject, "error", err)
		}),
	}
	if b.cfg.TLS.Serves() {
		opts = append(opts, nats.Secure(ClientTLS(b.anchors, "")))
	}
	conn, err := nats.Connect(srv.ClientURL(), opts...)
	if err != nil {
		return nil, fmt.Errorf("dial this node's own server: %w", err)
	}
	return conn, nil
}

// provisionSelf creates the NATS resources in this node's own account. On a
// node that joined another, it creates the stream with the results that wait
// for the node above. It creates the key-value buckets that record the method
// invocations this node executed and the results it processed. It subscribes
// to the subjects on which the node above sends requested property changes,
// requests for a full state report, its own state reports and the message
// that it lost the node above it.
func (b *Binding) provisionSelf(ctx context.Context, path fractal.NodePath) error {
	b.mu.Lock()
	js, conn := b.localJS, b.local
	b.mu.Unlock()
	if js == nil || conn == nil {
		return ErrNotStarted
	}
	if b.joiner != nil {
		if err := ensureStream(ctx, js, &nats.StreamConfig{
			Name:       resStreamName,
			Subjects:   []string{SubjectRES},
			Retention:  nats.WorkQueuePolicy,
			Storage:    nats.FileStorage,
			Duplicates: b.cfg.DedupWindow,
			MaxAge:     b.cfg.StreamMaxAge,
			MaxBytes:   b.cfg.StreamMaxBytes,
			Discard:    nats.DiscardNew,
		}); err != nil {
			return fmt.Errorf("build the result stream of %s: %w", path, err)
		}
	}
	dedup, err := newKVDedup(ctx, js, b.cfg.DedupWindow, b.clock())
	if err != nil {
		return err
	}
	b.mu.Lock()
	b.dedup = dedup
	b.mu.Unlock()

	id := b.cfg.NodeID
	for _, s := range []struct {
		subject string
		kind    fractal.Kind
	}{
		{SubjectDES(id), fractal.KindDES},
		{SubjectRBR(id), fractal.KindRBR},
		{SubjectAREP(id), fractal.KindAREP},
		{SubjectALOS(id), fractal.KindALOS},
	} {
		kind := s.kind
		if _, subErr := conn.Subscribe(s.subject, func(msg *nats.Msg) {
			b.engine.Deliver(ctx, fractalnodeInbound(kind, msg.Data, "", nil))
		}); subErr != nil {
			return fmt.Errorf("subscribe %s: %w", s.subject, subErr)
		}
	}
	return nil
}

// ensureRPCStream creates the stream with the method invocations for one node
// below. The stream is a work queue stored in files: it removes a message
// after the reader acknowledged it. A message waits there while the node below
// cannot be reached, and it survives a restart.
func (b *Binding) ensureRPCStream(ctx context.Context, js nats.JetStreamContext, id fractal.NodeID) error {
	if err := ensureStream(ctx, js, &nats.StreamConfig{
		Name:       rpcStreamName(id),
		Subjects:   []string{SubjectRPC(id)},
		Retention:  nats.WorkQueuePolicy,
		Storage:    nats.FileStorage,
		Duplicates: b.cfg.DedupWindow,
		MaxAge:     b.cfg.StreamMaxAge,
		MaxBytes:   b.cfg.StreamMaxBytes,
		Discard:    nats.DiscardNew,
	}); err != nil {
		return fmt.Errorf("build the method stream of %s: %w", id, err)
	}
	return nil
}

// ensureStream creates a stream when it does not exist. It never deletes or
// replaces an existing stream, because that would delete the messages that
// wait in it. It only changes the
// discard policy of an existing stream to the one of cfg, so a full stream
// refuses a new message rather than delete a waiting one.
func ensureStream(ctx context.Context, js nats.JetStreamContext, cfg *nats.StreamConfig) error {
	info, err := js.StreamInfo(cfg.Name, nats.Context(ctx))
	switch {
	case err == nil && info.Config.Discard == cfg.Discard:
		return nil
	case err == nil:
		updated := info.Config
		updated.Discard = cfg.Discard
		if _, updateErr := js.UpdateStream(&updated, nats.Context(ctx)); updateErr != nil {
			return fmt.Errorf("update the stream %s: %w", cfg.Name, updateErr)
		}
		return nil
	case !errors.Is(err, nats.ErrStreamNotFound):
		return fmt.Errorf("read the stream %s: %w", cfg.Name, err)
	}
	if _, addErr := js.AddStream(cfg, nats.Context(ctx)); addErr != nil {
		if errors.Is(addErr, nats.ErrStreamNameAlreadyInUse) {
			return nil
		}
		return fmt.Errorf("create the stream %s: %w", cfg.Name, addErr)
	}
	return nil
}

// ensureConsumer creates a durable pull consumer when it does not exist. A
// consumer is the reader position of a stream that the server stores. An
// existing consumer stays, with the messages it has not delivered yet.
func ensureConsumer(ctx context.Context, js nats.JetStreamContext, stream string, cfg *nats.ConsumerConfig) error {
	_, err := js.ConsumerInfo(stream, cfg.Durable, nats.Context(ctx))
	switch {
	case err == nil:
		return nil
	case !errors.Is(err, nats.ErrConsumerNotFound):
		return fmt.Errorf("read the consumer %s of %s: %w", cfg.Durable, stream, err)
	}
	if _, addErr := js.AddConsumer(stream, cfg, nats.Context(ctx)); addErr != nil {
		if errors.Is(addErr, nats.ErrConsumerNameAlreadyInUse) {
			return nil
		}
		return fmt.Errorf("create the consumer %s of %s: %w", cfg.Durable, stream, addErr)
	}
	return nil
}

// ensureKV returns a key-value bucket and creates it when it does not exist.
func ensureKV(js nats.JetStreamContext, cfg *nats.KeyValueConfig) (nats.KeyValue, error) {
	bucket, err := js.KeyValue(cfg.Bucket)
	switch {
	case err == nil:
		return bucket, nil
	case !errors.Is(err, nats.ErrBucketNotFound):
		return nil, fmt.Errorf("read the bucket %s: %w", cfg.Bucket, err)
	}
	bucket, err = js.CreateKeyValue(cfg)
	if err != nil {
		return nil, fmt.Errorf("create the bucket %s: %w", cfg.Bucket, err)
	}
	return bucket, nil
}

// consumerConfig returns the configuration of the consumer for method
// invocations and of the consumer for results. The consumer delivers one
// message at a time and needs an acknowledgement for each, which keeps the
// messages of one link in order.
func (b *Binding) consumerConfig(durable, filter string) *nats.ConsumerConfig {
	return &nats.ConsumerConfig{
		Durable:       durable,
		AckPolicy:     nats.AckExplicitPolicy,
		MaxAckPending: maxAckPending,
		AckWait:       b.cfg.AckWait,
		FilterSubject: filter,
	}
}

// fractalnodeInbound builds the Inbound value the binding passes to
// Engine.Deliver. from is the Node ID of the node below that sent the message,
// or empty for a message from the node above.
func fractalnodeInbound(
	kind fractal.Kind,
	payload []byte,
	from fractal.NodeID,
	ack func(ctx context.Context) error,
) fractalnode.Inbound {
	return fractalnode.Inbound{Payload: payload, Ack: ack, From: from, Kind: kind}
}

// now returns the current time.
func (b *Binding) now() time.Time { return time.Now() }

// clock returns b.now as a function, for the deduplication buckets.
func (b *Binding) clock() func() time.Time { return b.now }
