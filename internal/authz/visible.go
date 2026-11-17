package authz

import (
	"context"

	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
)

const (
	// The resource list. The permission is resolved inside the query and joined
	// to the directory, so one statement decides the rows and their order.
	// Fetching rows and filtering them in Go leaks through counts and offsets
	// even when the rows it returns are right, which is why there is no such
	// path here either.
	//
	// It is not paginated. A deployment runs about ten instances, and a limit
	// that truncates a visibility answer would hide instances the caller may
	// see.
	listResourcesSQL = `SELECT r.kind, r.resource_id, a.since
		FROM resource_access a
		JOIN authz_resource r ON r.kind = a.kind
		WHERE a.subject_type = ? AND a.subject_id = ? AND (a.actions & ?) <> 0
		ORDER BY r.kind, r.resource_id`

	// The whole directory, in the order the list returns it. It resolves no
	// permission: it is the raw read a reconciler needs to tell what the
	// directory holds from what the configuration declares.
	selectResourcesSQL = `SELECT kind, resource_id FROM authz_resource
		ORDER BY kind, resource_id`

	// The device half of a subscription's routing table. It is deliberately
	// not ListDevices: a page is what a screen asks for, and a routing table
	// is the whole set or it is a stream that silently drops what fell off the
	// end of page one.
	visibleDevicesSQL = `SELECT a.device_id, a.since, p.kind
		FROM device_access a
		JOIN authz_device p ON p.device_id = a.device_id
		WHERE a.subject_type = ? AND a.subject_id = ? AND (a.actions & ?) <> 0
		ORDER BY a.device_id`
)

// Visibility is what one principal may observe, and the generation of the graph
// it was computed at. It is the event stream filter. It reads the same two
// projections the point check and the list read, so a leak in one is a leak in
// the others and the differential tests catch it in all of them.
type Visibility struct {
	// Epoch is the generation the answer was computed at. A subscription
	// stamped with it re-authorizes as soon as Epoch moves past it.
	Epoch uint64
	// Devices is every device the principal may read telemetry from.
	Devices []VisibleDevice
	// Resources is every configured instance whose lifecycle the principal may
	// observe. It is not permission to read a camera's media, which this graph
	// does not model.
	Resources []VisibleResource
}

// VisibleDevice is one device a principal may observe, and the generation this
// principal's sight of it began at.
//
// Since is what bounds a replay on the event stream. Epoch is the graph as it
// stands, which is several writes past the first machine of a site that is
// powering up, so a reader floored by it is handed nothing of the machines that
// arrived before the last one. Zero means no generation was recorded.
type VisibleDevice struct {
	ID    string
	Since uint64
	// Kind is the device kind, so a camera's own events reach whoever observes it.
	Kind DeviceKind
}

// VisibleResource is one configured instance a principal may observe, and the
// generation this principal's sight of it began at. Since reads as VisibleDevice's
// does.
type VisibleResource struct {
	Ref   ref.Ref
	Since uint64
}

// Visible returns everything this principal may observe on the event bus. It is
// the whole set rather than a page, because it becomes a subscription's routing
// table and an origin left out of it is an event the reader never learns exists.
//
// The epoch is read before the rows and not after. A write landing in between
// then leaves the answer stamped older than the graph, so the reader
// re-authorizes once too often; stamping it after would leave the stamp newer
// than the rows it describes, and the change would never be noticed. A
// revocation is noticed only because of this order, so it is done here and not
// left to each caller.
//
// The two queries are not one snapshot. A write between them is covered by the
// same ordering: the stamp is already older than both, so the next tick
// recomputes.
func (s *Store) Visible(ctx context.Context, subject Subject) (Visibility, error) {
	epoch := s.Epoch()

	devices, err := s.visibleDevices(ctx, subject)
	if err != nil {
		return Visibility{}, err
	}

	resources, err := s.listResources(ctx, subject, ActionViewResource)
	if err != nil {
		return Visibility{}, err
	}

	return Visibility{Epoch: epoch, Devices: devices, Resources: resources}, nil
}

// ListResources returns the instances this subject may perform this action on,
// in an order that is a property of this query rather than of the database the
// server was installed on: both columns are byte-collated in the schema.
func (s *Store) ListResources(
	ctx context.Context, subject Subject, action Action,
) ([]ref.Ref, error) {
	seen, err := s.listResources(ctx, subject, action)
	if err != nil {
		return nil, err
	}

	out := make([]ref.Ref, 0, len(seen))
	for _, one := range seen {
		out = append(out, one.Ref)
	}

	return out, nil
}

// listResources is ListResources with the generation each sight began at, which
// only the event stream needs. One query serves both, so a caller reading the
// instances and a caller reading the instances and their generations cannot be
// answered by two different rules.
func (s *Store) listResources(
	ctx context.Context, subject Subject, action Action,
) ([]VisibleResource, error) {
	mask, err := checkable(ctx, subject, action)
	if err != nil {
		return nil, err
	}

	if domain, _ := action.domain(); domain != TypeResourceKind {
		return nil, unsupportedObject.New("this action is not defined on a resource instance").
			With(paramType(TypeResourceKind), paramAction(action))
	}

	rows, err := s.db.QueryContext(
		ctx, s.q(listResourcesSQL),
		subject.typ.String(), subject.id, int64(mask),
	)
	if err != nil {
		return nil, storeFailure("list resources", err)
	}

	defer func() { _ = rows.Close() }() // the scan error below is the one worth reporting

	var out []VisibleResource

	for rows.Next() {
		var (
			kind, id string
			since    uint64
		)

		if err = rows.Scan(&kind, &id, &since); err != nil {
			return nil, storeFailure("scan resource row", err)
		}

		// Rebuilt through the constructor, like a stored tuple: a directory row
		// that reached the table another way is refused rather than returned as
		// whatever it resembles.
		instance, buildErr := ref.New(ref.Kind(kind), id)
		if buildErr != nil {
			return nil, invalidStoredTuple.New("the resource directory holds a row that is not an instance").
				Wrap(buildErr)
		}

		out = append(out, VisibleResource{Ref: instance, Since: since})
	}

	if err = rows.Err(); err != nil {
		return nil, storeFailure("list resources", err)
	}

	return out, nil
}

// Resources returns every registered instance, whoever may see it. It is the
// counterpart of Tuples: the raw directory read, for the caller that has to
// reconcile the directory against what the configuration declares. That caller
// has to know what is in the directory, not what it may see.
func (s *Store) Resources(ctx context.Context) ([]ref.Ref, error) {
	return s.resources(ctx, s.db)
}

// resources is the directory read, against whichever handle the caller is on.
// SyncResources reads it inside the transaction it then writes, so the diff it
// takes cannot be against a directory another writer has already moved.
func (s *Store) resources(ctx context.Context, q queryer) ([]ref.Ref, error) {
	rows, err := q.QueryContext(ctx, s.q(selectResourcesSQL))
	if err != nil {
		return nil, storeFailure("read the resource directory", err)
	}

	defer func() { _ = rows.Close() }() // the scan error below is the one worth reporting

	var out []ref.Ref

	for rows.Next() {
		var kind, id string
		if err = rows.Scan(&kind, &id); err != nil {
			return nil, storeFailure("scan resource row", err)
		}

		instance, buildErr := ref.New(ref.Kind(kind), id)
		if buildErr != nil {
			return nil, invalidStoredTuple.New("the resource directory holds a row that is not an instance").
				Wrap(buildErr)
		}

		out = append(out, instance)
	}

	if err = rows.Err(); err != nil {
		return nil, storeFailure("read the resource directory", err)
	}

	return out, nil
}

// VisibleDevicesFor is the unpaginated device set for one action, which is what
// a caller narrowing a stream by something other than sight of the device asks
// for. It resolves the permission inside the query the way every other read
// here does, so a set computed for one action cannot be a set filtered in Go
// from another.
func (s *Store) VisibleDevicesFor(
	ctx context.Context, subject Subject, action Action,
) ([]VisibleDevice, error) {
	return s.devicesFor(ctx, subject, action)
}

// visibleDevices is the unpaginated device set behind Visible.
func (s *Store) visibleDevices(ctx context.Context, subject Subject) ([]VisibleDevice, error) {
	return s.devicesFor(ctx, subject, ActionObserve)
}

// devicesFor is the one statement both readings run.
func (s *Store) devicesFor(
	ctx context.Context, subject Subject, action Action,
) ([]VisibleDevice, error) {
	mask, err := checkable(ctx, subject, action)
	if err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(
		ctx, s.q(visibleDevicesSQL),
		subject.typ.String(), subject.id, int64(mask),
	)
	if err != nil {
		return nil, storeFailure("read visible devices", err)
	}

	defer func() { _ = rows.Close() }() // the scan error below is the one worth reporting

	var out []VisibleDevice

	for rows.Next() {
		var (
			id    string
			since uint64
			kind  string
		)

		if err = rows.Scan(&id, &since, &kind); err != nil {
			return nil, storeFailure("scan visible device", err)
		}

		out = append(out, VisibleDevice{ID: id, Since: since, Kind: DeviceKind(kind)})
	}

	if err = rows.Err(); err != nil {
		return nil, storeFailure("read visible devices", err)
	}

	return out, nil
}
