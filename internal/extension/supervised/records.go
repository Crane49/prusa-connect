package supervised

import (
	"context"
	"errors"
	"fmt"

	"github.com/prusa3d/prusa-connect/internal/extension/supervisor"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/stateapi"
)

// ErrNotARecordBackend reports an instance that is not a record backend, or one
// whose target is not the contract a module's namespace would be minted from.
var ErrNotARecordBackend = errors.New("supervised: the instance is not a record backend")

// Backend is a supervised record backend: the store behind sdk/stateapi.
//
// It mints a Records per namespace. The Records a module receives is the
// backend's own value, unwrapped. The module is promised an interface that does
// not change with the backend, and a second supervision layer between the
// module and its own store would add a per-call budget the contract never
// named.
type Backend struct {
	instance *supervisor.Instance[any]
}

// AttachRecords wraps an instance the host is already supervising.
func AttachRecords(instance *supervisor.Instance[any]) (*Backend, error) {
	if instance == nil {
		return nil, fmt.Errorf("%w: no instance", ErrNotARecordBackend)
	}

	if kind := instance.Ref().Kind(); kind != ref.KindDB {
		return nil, fmt.Errorf("%w: %s is a %s", ErrNotARecordBackend, instance.Ref(), kind)
	}

	mismatch, serving := supervisor.Peek(instance, func(target any) error {
		if _, ok := target.(stateapi.Backend); ok {
			return nil
		}

		return fmt.Errorf("%w: %s built a %T, which mints no namespace",
			ErrNotARecordBackend, instance.Ref(), target)
	})

	if !serving {
		return nil, fmt.Errorf("%w: %w: %s is %s",
			ErrNotARecordBackend, ErrNotServing, instance.Ref(), instance.State())
	}

	if mismatch != nil {
		return nil, mismatch
	}

	return &Backend{instance: instance}, nil
}

// Ref returns the instance's own name.
func (b *Backend) Ref() ref.Ref { return b.instance.Ref() }

// Namespaces reports what each namespace stores. It goes through the instance's
// queue, because it reads the store and a slow backend must slow only itself.
func (b *Backend) Namespaces(ctx context.Context) ([]stateapi.Accounting, error) {
	return supervisor.Call(ctx, b.instance, "namespaces",
		func(callCtx context.Context, target any) ([]stateapi.Accounting, error) {
			backend, ok := target.(stateapi.Backend)
			if !ok {
				return nil, ErrNotARecordBackend
			}

			return backend.Namespaces(callCtx)
		})
}

// Records returns the store for one namespace, held to limit bytes. It refuses
// when the instance stopped serving between the resolve and this call.
func (b *Backend) Records(namespace string, limit int64) (stateapi.Records, error) {
	type answer struct {
		records stateapi.Records
		err     error
	}

	got, serving := supervisor.Peek(b.instance, func(target any) answer {
		backend, ok := target.(stateapi.Backend)
		if !ok {
			return answer{err: ErrNotARecordBackend}
		}

		records, err := backend.Records(namespace, limit)

		return answer{records: records, err: err}
	})

	if !serving {
		return nil, fmt.Errorf("%w: %w: %s is %s",
			ErrNotARecordBackend, ErrNotServing, b.instance.Ref(), b.instance.State())
	}

	return got.records, got.err
}
