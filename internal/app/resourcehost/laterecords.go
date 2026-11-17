package resourcehost

import (
	"context"
	"errors"
	"fmt"

	"github.com/prusa3d/prusa-connect/sdk/stateapi"
)

// ErrNoRecordBackend reports a state call made while no record backend was
// serving.
//
// A host that serves no state at all answers differently: it installs no
// method set, and a module reads CodeMethodNotFound on its first call. This
// error is for a declared backend that is starting, restarting or has failed.
// The module can retry.
var ErrNoRecordBackend = errors.New("resourcehost: no record backend is serving")

// lateRecords is one namespace's store, resolved on every call.
//
// A module's method set is installed once, when the child process is
// launched. The record backend is an instance of kind db that this host
// supervises too, and it may start after the module. A store resolved once at
// launch could be missing or out of date for the whole run of the module.
// Every call therefore resolves the store again, so the next call after a
// backend is replaced or restarted uses the new backend, and no module needs a
// restart.
type lateRecords struct {
	namespace string
	// limit is the byte limit the declaration in force at the start granted.
	limit int64
	open  func(string, int64) (stateapi.Records, error)
}

var _ stateapi.Records = (*lateRecords)(nil)

// records resolves the store for this namespace, or reports why it could not.
func (l *lateRecords) records() (stateapi.Records, error) {
	held, err := l.open(l.namespace, l.limit)
	if err != nil {
		return nil, fmt.Errorf("open the records of %s: %w", l.namespace, err)
	}

	if held == nil {
		return nil, fmt.Errorf("%w: %s", ErrNoRecordBackend, l.namespace)
	}

	return held, nil
}

func (l *lateRecords) Get(ctx context.Context, key stateapi.Key) (stateapi.Value, error) {
	held, err := l.records()
	if err != nil {
		return stateapi.Value{}, err
	}

	return held.Get(ctx, key)
}

func (l *lateRecords) Put(ctx context.Context, key stateapi.Key, value []byte) (uint64, error) {
	held, err := l.records()
	if err != nil {
		return 0, err
	}

	return held.Put(ctx, key, value)
}

func (l *lateRecords) CompareAndPut(
	ctx context.Context, key stateapi.Key, expected uint64, value []byte,
) (uint64, error) {
	held, err := l.records()
	if err != nil {
		return 0, err
	}

	return held.CompareAndPut(ctx, key, expected, value)
}

func (l *lateRecords) Delete(ctx context.Context, key stateapi.Key) error {
	held, err := l.records()
	if err != nil {
		return err
	}

	return held.Delete(ctx, key)
}

func (l *lateRecords) List(ctx context.Context, prefix stateapi.Key) ([]stateapi.Key, error) {
	held, err := l.records()
	if err != nil {
		return nil, err
	}

	return held.List(ctx, prefix)
}

func (l *lateRecords) Version(ctx context.Context, key stateapi.Key) (uint64, error) {
	held, err := l.records()
	if err != nil {
		return 0, err
	}

	return held.Version(ctx, key)
}

func (l *lateRecords) Usage(ctx context.Context) (stateapi.Usage, error) {
	held, err := l.records()
	if err != nil {
		return stateapi.Usage{}, err
	}

	return held.Usage(ctx)
}
