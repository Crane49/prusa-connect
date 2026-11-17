package natsbind

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
)

// kvDedup records the method invocations this node executed and the results
// it processed, so the node does not act on a repeated message. It uses two
// JetStream key-value buckets stored in files. An entry expires after
// Config.DedupWindow.
//
// The exec bucket has, for each method invocation this node executed, the
// result message. When the same invocation arrives again the engine sends the
// stored result and does not execute the method again. The proc bucket has
// the id of each result this node processed.
//
// kvDedup implements ExecutedStore, ProcessedStore and Resetter of
// sdk/fractal/node.
type kvDedup struct {
	js   nats.JetStreamContext
	exec nats.KeyValue
	proc nats.KeyValue
	now  func() time.Time
}

// newKVDedup creates both buckets, or opens them when they exist. They are
// stored in files, so after a restart within the window the node still does
// not execute a method twice.
func newKVDedup(
	ctx context.Context,
	js nats.JetStreamContext,
	window time.Duration,
	now func() time.Time,
) (*kvDedup, error) {
	exec, err := ensureKV(js, &nats.KeyValueConfig{
		Bucket:  execBucket,
		TTL:     window,
		Storage: nats.FileStorage,
	})
	if err != nil {
		return nil, fmt.Errorf("build the bucket of executed invocations: %w", err)
	}
	proc, err := ensureKV(js, &nats.KeyValueConfig{
		Bucket:  procBucket,
		TTL:     window,
		Storage: nats.FileStorage,
	})
	if err != nil {
		return nil, fmt.Errorf("build the bucket of processed results: %w", err)
	}
	_ = ctx // The key-value calls of the client take no context.
	return &kvDedup{js: js, exec: exec, proc: proc, now: now}, nil
}

// Executed returns the stored result message of the method invocation with
// this id. ok is false when this node has no record of it.
func (d *kvDedup) Executed(_ context.Context, id fractal.RPCID) (res []byte, ok bool, err error) {
	entry, err := d.exec.Get(string(id))
	switch {
	case errors.Is(err, nats.ErrKeyNotFound):
		return nil, false, nil
	case err != nil:
		return nil, false, fmt.Errorf("read the executed invocation %s: %w", id, err)
	}
	return entry.Value(), true, nil
}

// RecordExecuted stores the result message of the method invocation with this
// id. It returns after JetStream has stored the entry. The node that executes
// a method must work in this order: execute, record, publish the result,
// acknowledge.
func (d *kvDedup) RecordExecuted(_ context.Context, id fractal.RPCID, res []byte) error {
	if _, err := d.exec.Put(string(id), res); err != nil {
		return fmt.Errorf("record the executed invocation %s: %w", id, err)
	}
	return nil
}

// Processed reports whether this node has already processed the result with
// this id.
func (d *kvDedup) Processed(_ context.Context, id fractal.RPCID) (bool, error) {
	_, err := d.proc.Get(string(id))
	switch {
	case errors.Is(err, nats.ErrKeyNotFound):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("read the processed result %s: %w", id, err)
	}
	return true, nil
}

// RecordProcessed records that this node processed the result with this id.
func (d *kvDedup) RecordProcessed(_ context.Context, id fractal.RPCID) error {
	if _, err := d.proc.Put(string(id), nil); err != nil {
		return fmt.Errorf("record the processed result %s: %w", id, err)
	}
	return nil
}

// Reset deletes every entry of both buckets. The engine calls it when this
// node receives a different Node Path. The entries belong to messages that
// were addressed to the earlier path, and no such message can arrive under the
// new one.
func (d *kvDedup) Reset() {
	for _, bucket := range []nats.KeyValue{d.exec, d.proc} {
		keys, err := bucket.Keys()
		if err != nil {
			continue
		}
		for _, key := range keys {
			_ = bucket.Purge(key) // A key that is already deleted needs no error.
		}
	}
}
