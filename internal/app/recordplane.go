package app

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/prusa3d/prusa-connect/internal/app/resourcehost"
	"github.com/prusa3d/prusa-connect/internal/extension/supervised"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/stateapi"
)

// ErrRecordBackendAmbiguous reports two serving record backends. The record
// door has one answer per namespace and no verb to split them by, so the host
// refuses that configuration and does not pick one.
var ErrRecordBackendAmbiguous = errors.New("app: two record backends are serving and neither is the one")

// recordDoor is the state door the module plane opens, resolved per call.
//
// It breaks a startup cycle. The backend is a supervised instance of kind db,
// supervised by the plane it serves, so the plane cannot be handed the backend
// when it is built. The door is constructed empty, passed to resourcehost.New
// as a function, and the host is written into it right afterwards. A call
// that arrives before that resolves nothing, and a module reads that as the
// host serving no state.
type recordDoor struct{ host *resourcehost.Host }

// backend is the one serving record backend, or nil where none is.
//
// Two serving instances are refused. The read side of the history kind can
// pick by verb because its instances declare which verbs they answer. A record
// namespace has no such split, so with two backends a module could get a
// different store after a restart.
func (d *recordDoor) backend() (*supervised.Backend, error) {
	if d == nil || d.host == nil {
		return nil, nil //nolint:nilnil // no backend and no failure is what the caller branches on.
	}

	var (
		found  *supervised.Backend
		claims []string
	)

	for instance, runner := range d.host.Supervising(ref.KindDB) {
		attached, err := supervised.AttachRecords(runner)
		if err != nil {
			continue
		}

		found = attached

		claims = append(claims, instance.String())
	}

	if len(claims) > 1 {
		slices.Sort(claims)

		return nil, fmt.Errorf("%w: %s", ErrRecordBackendAmbiguous, strings.Join(claims, " and "))
	}

	return found, nil
}

// records is what resourcehost.Config.State is bound to: the store for one
// module's namespace, resolved when the module is installed and not when the
// plane is built.
//
// A deployment with no serving backend answers nil. The host then installs no
// state methods, so a module calling state.get reads CodeMethodNotFound.
func (d *recordDoor) records(namespace string, limit int64) (stateapi.Records, error) {
	backend, err := d.backend()
	if err != nil {
		return nil, err
	}

	if backend == nil {
		return nil, nil //nolint:nilnil // no store and no failure is the host's own nil path.
	}

	return backend.Records(namespace, limit)
}

// Namespaces answers how much every namespace holds: the key count, the bytes
// and the limit. It never answers a stored value.
//
// It authorizes nothing. The transport decides who may see a row, against the
// instance that claims the namespace, because the transport reaches both the
// module plane and the graph. A permission is decided in one place.
func (d *recordDoor) Namespaces(ctx context.Context) ([]httpapi.StateNamespace, error) {
	backend, err := d.backend()
	if err != nil {
		return nil, err
	}

	if backend == nil {
		return nil, nil
	}

	held, err := backend.Namespaces(ctx)
	if err != nil {
		return nil, fmt.Errorf("read the module namespaces: %w", err)
	}

	out := make([]httpapi.StateNamespace, 0, len(held))
	for _, one := range held {
		limit := int64(stateapi.DefaultNamespaceBytes)
		if d.host != nil {
			limit = d.host.RecordLimit(one.Namespace)
		}

		out = append(out, httpapi.StateNamespace{
			Namespace: one.Namespace,
			Keys:      one.Keys,
			Bytes:     one.Bytes,
			Limit:     limit,
		})
	}

	return out, nil
}

// stateStore is the accounting surface, resolved per request. A deployment
// with no serving backend resolves no store, and the route is still published
// in the OpenAPI document.
func (d *recordDoor) stateStore() httpapi.StateResolver {
	return func() (httpapi.StateStore, error) {
		backend, err := d.backend()
		if err != nil {
			return nil, err
		}

		if backend == nil {
			return nil, nil
		}

		return d, nil
	}
}

// settingsPrefix is where a module's browser half keeps one person's settings
// inside the module's own namespace.
//
// The records go in the module's namespace and not in a namespace of the
// host's, so there is one key/value store and no second table beside it. The
// module's own process reads its whole namespace, so a module can read and
// rewrite what its settings pane stored. The bytes count toward the one
// namespace budget an operator reads at /api/v1/state/namespaces.
const settingsPrefix = "ui/settings/"

// settingsKey is where one person's record under one module is kept. The
// principal is a path element of its own, so two people never share a key, and
// the transport refuses a principal carrying a separator before it reaches
// here.
func settingsKey(principal, key string) stateapi.Key {
	return stateapi.Key(settingsPrefix + principal + "/" + key)
}

// errNoRecordBackend reports a deployment running no record backend. The
// transport resolves the store first and answers STATE_NOT_CONFIGURED, so this
// covers a backend that stopped between the resolve and the call. The write
// then fails with an error and is not dropped silently.
var errNoRecordBackend = errors.New("app: no record backend is serving")

// moduleSettings is httpapi.ModuleStateStore over the same record backend the
// accounting reads. It uses that backend because its rows survive a restart on
// both SQLite and Postgres: adapters/db/internal/recstore carries a migration
// directory per engine and every statement in it runs against both.
type moduleSettings struct{ door *recordDoor }

// records is the namespace's door, or the refusal of a deployment running no
// record backend. It is held to the limit the module that claims the namespace
// was granted, so a person writing settings cannot take the namespace past what
// the module itself may hold.
func (s moduleSettings) records(namespace string) (stateapi.Records, error) {
	limit := int64(stateapi.DefaultNamespaceBytes)
	if s.door.host != nil {
		limit = s.door.host.RecordLimit(namespace)
	}

	records, err := s.door.records(namespace, limit)
	if err != nil {
		return nil, err
	}

	if records == nil {
		return nil, fmt.Errorf("open the records of %s: %w", namespace, errNoRecordBackend)
	}

	return records, nil
}

// Keys answers the keys one person holds under one module, with the prefix
// taken off.
func (s moduleSettings) Keys(ctx context.Context, namespace, principal string) ([]string, error) {
	records, err := s.records(namespace)
	if err != nil {
		return nil, err
	}

	prefix := string(settingsKey(principal, ""))

	held, err := records.List(ctx, stateapi.Key(prefix))
	if err != nil {
		return nil, fmt.Errorf("list the records of %s: %w", namespace, err)
	}

	keys := make([]string, 0, len(held))
	for _, one := range held {
		keys = append(keys, strings.TrimPrefix(string(one), prefix))
	}

	return keys, nil
}

// Value answers one record, and httpapi.ErrNoStateRecord for a key nothing is
// stored under.
func (s moduleSettings) Value(ctx context.Context, namespace, principal, key string) ([]byte, error) {
	records, err := s.records(namespace)
	if err != nil {
		return nil, err
	}

	value, err := records.Get(ctx, settingsKey(principal, key))
	if err != nil {
		if errors.Is(err, stateapi.ErrNoRecord) {
			return nil, httpapi.ErrNoStateRecord
		}

		return nil, fmt.Errorf("read a record of %s: %w", namespace, err)
	}

	return value.Bytes, nil
}

// Write stores one record whatever is there. Put and not CompareAndPut: a
// settings pane is one person editing their own record, so there is no second
// writer for a version to fence against.
func (s moduleSettings) Write(
	ctx context.Context, namespace, principal, key string, value []byte,
) error {
	records, err := s.records(namespace)
	if err != nil {
		return err
	}

	if _, err = records.Put(ctx, settingsKey(principal, key), value); err != nil {
		return fmt.Errorf("write a record of %s: %w", namespace, err)
	}

	return nil
}

// Remove deletes one record, and answers httpapi.ErrNoStateRecord for a key
// nothing is stored under.
func (s moduleSettings) Remove(ctx context.Context, namespace, principal, key string) error {
	records, err := s.records(namespace)
	if err != nil {
		return err
	}

	if err = records.Delete(ctx, settingsKey(principal, key)); err != nil {
		if errors.Is(err, stateapi.ErrNoRecord) {
			return httpapi.ErrNoStateRecord
		}

		return fmt.Errorf("remove a record of %s: %w", namespace, err)
	}

	return nil
}

// settingsStore is the per-key surface, resolved per request. A deployment with
// no serving backend resolves nothing, and the transport reports that to the
// caller.
func (d *recordDoor) settingsStore() httpapi.ModuleStateResolver {
	return func() (httpapi.ModuleStateStore, error) {
		backend, err := d.backend()
		if err != nil {
			return nil, err
		}

		if backend == nil {
			return nil, nil // no store and no failure is what the transport branches on.
		}

		return moduleSettings{door: d}, nil
	}
}
