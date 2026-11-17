// Package hostfake holds the stand-ins the tests of this module run against,
// so the rules are proved with no host process on the other end.
//
// Records is a record store in memory. It keeps the promises sdk/stateapi
// makes: a version that rises on every write, CompareAndPut refusing a version
// the store is not at, List answering sorted keys and no values, and the three
// size limits. A test can also make it fail on purpose, which is how the
// refusal path of every route is reached.
package hostfake

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/prusa3d/prusa-connect/sdk/stateapi"
)

// stored is one record: its bytes and the version it is at.
type stored struct {
	bytes   []byte
	version uint64
}

// Records is a record store in memory, safe for use from several goroutines.
//
// The zero value is an empty store, ready to use.
type Records struct {
	mu   sync.Mutex
	rows map[stateapi.Key]stored

	// Limit is the byte limit the store holds the namespace to, as the host
	// would after the operator granted it. Zero is the default.
	Limit int64

	// Fail is called before every call. A non-nil answer is what the call
	// returns, so a test reaches the path a store failure takes. The name of
	// the call is "get", "compare_and_put", "delete" or "list".
	Fail func(call string, key stateapi.Key) error

	// Before is called after CompareAndPut read its arguments and before it
	// writes. A test uses it to land another write in between, which is how a
	// lost race is proved.
	Before func(key stateapi.Key)
}

var _ stateapi.Records = (*Records)(nil)

// Get answers the record under key.
func (r *Records) Get(_ context.Context, key stateapi.Key) (stateapi.Value, error) {
	if err := r.refuse("get", key); err != nil {
		return stateapi.Value{}, err
	}

	if err := key.Validate(); err != nil {
		return stateapi.Value{}, err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	row, found := r.rows[key]
	if !found {
		return stateapi.Value{}, fmt.Errorf("%w: %s", stateapi.ErrNoRecord, key)
	}

	return stateapi.Value{Bytes: append([]byte(nil), row.bytes...), Version: row.version}, nil
}

// Put writes value under key whatever is there.
func (r *Records) Put(ctx context.Context, key stateapi.Key, value []byte) (uint64, error) {
	held, err := r.Version(ctx, key)
	if err != nil {
		held = stateapi.AbsentVersion
	}

	return r.CompareAndPut(ctx, key, held, value)
}

// CompareAndPut writes value under key only if the record is at expected.
func (r *Records) CompareAndPut(
	_ context.Context, key stateapi.Key, expected uint64, value []byte,
) (uint64, error) {
	if err := r.refuse("compare_and_put", key); err != nil {
		return 0, err
	}

	if err := key.Validate(); err != nil {
		return 0, err
	}

	if err := stateapi.ValidateValue(value); err != nil {
		return 0, err
	}

	if r.Before != nil {
		r.Before(key)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.rows == nil {
		r.rows = map[stateapi.Key]stored{}
	}

	row := r.rows[key]
	if row.version != expected {
		return 0, fmt.Errorf("%w: %s is at %d and the write expected %d",
			stateapi.ErrVersion, key, row.version, expected)
	}

	if err := r.roomFor(key, value); err != nil {
		return 0, err
	}

	written := stored{bytes: append([]byte(nil), value...), version: row.version + 1}
	r.rows[key] = written

	return written.version, nil
}

// Delete removes the record under key.
func (r *Records) Delete(_ context.Context, key stateapi.Key) error {
	if err := r.refuse("delete", key); err != nil {
		return err
	}

	if err := key.Validate(); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, found := r.rows[key]; !found {
		return fmt.Errorf("%w: %s", stateapi.ErrNoRecord, key)
	}

	delete(r.rows, key)

	return nil
}

// List answers every key under prefix, in sorted order.
func (r *Records) List(_ context.Context, prefix stateapi.Key) ([]stateapi.Key, error) {
	if err := r.refuse("list", prefix); err != nil {
		return nil, err
	}

	if err := stateapi.ValidatePrefix(prefix); err != nil {
		return nil, err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]stateapi.Key, 0, len(r.rows))

	for key := range r.rows {
		if strings.HasPrefix(string(key), string(prefix)) {
			out = append(out, key)
		}
	}

	slices.Sort(out)

	return out, nil
}

// Version answers what the record under key is at.
func (r *Records) Version(ctx context.Context, key stateapi.Key) (uint64, error) {
	value, err := r.Get(ctx, key)
	if err != nil {
		return 0, err
	}

	return value.Version, nil
}

// Keys answers how many records the store holds, which is what a restart test
// reads to say the data is in the store and not in a process.
func (r *Records) Keys() int {
	r.mu.Lock()
	defer r.mu.Unlock()

	return len(r.rows)
}

// refuse answers what the Fail hook said about this call.
func (r *Records) refuse(call string, key stateapi.Key) error {
	if r.Fail == nil {
		return nil
	}

	return r.Fail(call, key)
}

// roomFor refuses a write that would take the store over the namespace limit,
// which is the limit the host holds a real module to.
func (r *Records) roomFor(key stateapi.Key, value []byte) error {
	total := stateapi.Cost(key, value)

	for held, row := range r.rows {
		if held != key {
			total += stateapi.Cost(held, row.bytes)
		}
	}

	if limit := r.limit(); total > limit {
		return fmt.Errorf("%w: the namespace would hold %d bytes and the limit is %d",
			stateapi.ErrNamespaceFull, total, limit)
	}

	return nil
}

// limit is Limit, or the default when the test set none.
func (r *Records) limit() int64 {
	if r.Limit == 0 {
		return stateapi.DefaultNamespaceBytes
	}

	return r.Limit
}

// Usage answers what the store holds and the limit it is held to, as
// state.usage does on a real host.
func (r *Records) Usage(context.Context) (stateapi.Usage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	usage := stateapi.Usage{Limit: r.limit()}
	for key, row := range r.rows {
		usage.Bytes += stateapi.Cost(key, row.bytes)
	}

	return usage, nil
}
