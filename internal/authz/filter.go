package authz

import (
	"context"

	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
)

const (
	// defaultPageLimit is one screen of devices.
	defaultPageLimit = 50
	// maxPageLimit bounds one response. A caller asking for more gets this,
	// not an error, so a fleet-wide export pages instead of allocating.
	maxPageLimit = 1000
)

const (
	listDevicesSQL = `SELECT p.device_id, p.name, p.kind, a.actions
		FROM device_access a
		JOIN authz_device p ON p.device_id = a.device_id
		WHERE a.subject_type = ? AND a.subject_id = ? AND (a.actions & ?) <> 0
		ORDER BY p.sort_key, p.device_id
		LIMIT ? OFFSET ?`

	countDevicesSQL = `SELECT COUNT(*)
		FROM device_access a
		JOIN authz_device p ON p.device_id = a.device_id
		WHERE a.subject_type = ? AND a.subject_id = ? AND (a.actions & ?) <> 0`

	// unseenDevicesSQL counts the devices in the directory this subject reaches
	// the action on nowhere. It is the whole of a fleet-wide decision: zero is
	// permitted and anything else is not.
	//
	// It is written as a count of what the subject cannot see rather than a
	// comparison of two counts. Two counts are two statements, and a device
	// registering between them makes the pair agree when the graph does not.
	unseenDevicesSQL = `SELECT COUNT(*)
		FROM authz_device p
		WHERE NOT EXISTS (
			SELECT 1 FROM device_access a
			WHERE a.device_id = p.device_id
				AND a.subject_type = ? AND a.subject_id = ? AND (a.actions & ?) <> 0
		)`

	// walkDevicesSQL takes the next window by where the last one ended rather
	// than by how many rows preceded it. The comparison is written out instead
	// of as a row value, because SQLite compiles a row-value comparison only
	// from 3.15 and this file has to render for both engines from one string.
	//
	// The index on (sort_key, device_id) serves this the way it serves the
	// ordering, so a deep window costs what a shallow one does. An OFFSET does
	// not: the engine counts the rows it skips.
	walkDevicesSQL = `SELECT p.device_id, p.name, p.kind, a.actions
		FROM device_access a
		JOIN authz_device p ON p.device_id = a.device_id
		WHERE a.subject_type = ? AND a.subject_id = ? AND (a.actions & ?) <> 0
		  AND (p.sort_key > ? OR (p.sort_key = ? AND p.device_id > ?))
		ORDER BY p.sort_key, p.device_id
		LIMIT ?`
)

// Page is one window of a list. The zero value asks for the first page at the
// default size.
type Page struct {
	Limit  int
	Offset int
}

// DeviceRow is one row of the device directory: what the list returns, and
// what registering a device writes.
type DeviceRow struct {
	ID   string
	Name string

	// Kind is what type of device this row is. The zero value means printer.
	//
	// It is a ref.DeviceKind and not a ref.Kind, because the set of device
	// kinds is open: adding a device class takes no edit to the core, so a
	// kind a module declared is as valid here as printer. The directory
	// accepts the kinds the verb table knows: the printer's own, and every
	// kind a compiled-in declaration carries.
	//
	// It is on the row and not in a table of its own because the directory is
	// one id space: a camera and a printer that share an id are one row and
	// one set of grants. UpsertDevices reads it to refuse that write.
	Kind ref.DeviceKind

	// Actions is every action the subject the list was resolved for may perform
	// on this device, in the vocabulary's order. It is the mask the same row
	// was filtered by, decoded, so it costs no second statement and cannot
	// disagree with the filter that admitted the row.
	//
	// It is never empty on a row a list returned: a row is admitted by holding
	// at least the action that was asked for.
	Actions []Action
}

// scanDeviceRow reads one row of the two directory queries. Both select the
// same three columns, so a column added to one and not the other is a scan
// error rather than a listing whose action sets are silently absent.
//
// The actions are cut down to what the request credential's scopes allow, as
// ObjectActions cuts them, so a list advertises no more than a check grants.
func scanDeviceRow(rows interface{ Scan(...any) error }, scope ceiling) (DeviceRow, error) {
	// The mask is scanned into the width it is, so a row outside the action
	// space, which only a hand-edited or corrupted projection can hold, is
	// refused by the driver's range check rather than truncated into a set of
	// actions nobody granted.
	var (
		row  DeviceRow
		kind string
		held uint32
	)

	if err := rows.Scan(&row.ID, &row.Name, &kind, &held); err != nil {
		return DeviceRow{}, storeFailure("scan device row", err)
	}

	row.Kind = ref.DeviceKind(kind)
	row.Actions = scope.clamp(actionSet(held)).actions()

	return row, nil
}

// ListDevices returns the devices this subject may perform this action on,
// ordered and paginated. The permission is resolved inside the query and joined
// to the directory, so one statement decides the rows, the order and the
// offset. Fetching rows and filtering them in Go leaks through pagination even
// when the rows it returns are right, which is why there is no such path here.
//
// The order is by the directory's case-folded sort key rather than by the name,
// so the same page holds the same rows on both engines whatever collation the
// database was created with.
func (s *Store) ListDevices(
	ctx context.Context, subject Subject, action Action, page Page,
) ([]DeviceRow, error) {
	mask, err := checkable(ctx, subject, action)
	if err != nil {
		return nil, err
	}

	if domain, _ := action.domain(); domain != TypeDevice {
		return nil, unsupportedObject.New("this action is not defined on a device").
			With(paramType(TypeDevice), paramAction(action))
	}

	limit, offset := page.Bounds()

	rows, err := s.db.QueryContext(
		ctx, s.q(listDevicesSQL),
		subject.typ.String(), subject.id, int64(mask), limit, offset,
	)
	if err != nil {
		return nil, storeFailure("list devices", err)
	}

	defer func() { _ = rows.Close() }() // the scan error below is the one worth reporting

	out := make([]DeviceRow, 0, limit)

	for rows.Next() {
		row, scanErr := scanDeviceRow(rows, ceilingFrom(ctx))
		if scanErr != nil {
			return nil, scanErr
		}

		out = append(out, row)
	}

	if err = rows.Err(); err != nil {
		return nil, storeFailure("list devices", err)
	}

	return out, nil
}

// WalkDevices calls visit for every device this subject may perform this
// action on, in the directory's own order, reading window rows at a time. It
// stops on the first error visit returns and reports it.
//
// It exists because a caller that has to read the whole authorized set cannot
// page through it with an offset. The directory takes a row whenever a device
// registers, and a row that sorts before the window a walk has reached moves
// every row after it along by one: the next window then repeats one row and
// never returns the one after it. A caller counting an authorized population is
// therefore wrong about a fleet that is being dialled into, which is every
// fleet.
//
// The cursor is the last row's sort key and id together. The key alone is not
// unique, because two devices may carry the same name, and a cursor that is
// not unique loses exactly the rows that share one.
//
// One window is read whole and its rows are closed before visit is called on
// any of them. A visit that writes to this store while a result set is open
// would be a second statement on a connection that is still streaming the
// first.
func (s *Store) WalkDevices(
	ctx context.Context, subject Subject, action Action, window int, visit func(DeviceRow) error,
) error {
	mask, err := checkable(ctx, subject, action)
	if err != nil {
		return err
	}

	if domain, _ := action.domain(); domain != TypeDevice {
		return unsupportedObject.New("this action is not defined on a device").
			With(paramType(TypeDevice), paramAction(action))
	}

	if window <= 0 {
		window = defaultPageLimit
	}

	var cursor DeviceRow

	for {
		rows, windowErr := s.walkWindow(ctx, subject, mask, cursor, window)
		if windowErr != nil {
			return windowErr
		}

		for _, row := range rows {
			if err = visit(row); err != nil {
				return err
			}
		}

		if len(rows) < window {
			return nil
		}

		cursor = rows[len(rows)-1]
	}
}

// walkWindow reads one window of the walk, starting after the row the cursor
// names. A zero cursor starts at the beginning: no sort key is below the empty
// string and no device id is empty, so the comparison admits every row.
func (s *Store) walkWindow(
	ctx context.Context, subject Subject, mask actionSet, after DeviceRow, window int,
) ([]DeviceRow, error) {
	key := sortKey(after.Name)

	rows, err := s.db.QueryContext(
		ctx, s.q(walkDevicesSQL),
		subject.typ.String(), subject.id, int64(mask), key, key, after.ID, window,
	)
	if err != nil {
		return nil, storeFailure("walk devices", err)
	}

	defer func() { _ = rows.Close() }() // the scan error below is the one worth reporting

	out := make([]DeviceRow, 0, window)

	for rows.Next() {
		row, scanErr := scanDeviceRow(rows, ceilingFrom(ctx))
		if scanErr != nil {
			return nil, scanErr
		}

		out = append(out, row)
	}

	if err = rows.Err(); err != nil {
		return nil, storeFailure("walk devices", err)
	}

	return out, nil
}

// CanReadFleetTelemetry answers whether this subject may read an answer that
// covers the whole fleet at once, which is true when it reaches observe on
// every device in the directory.
//
// It exists for a payload that cannot be filtered per caller. The Prometheus
// exposition is one document naming every device this process has seen, and a
// scrape is machine to machine, so there is no per-caller narrowing to do and
// the decision is all or nothing. The endpoint takes a named grant, and the
// per-device labels stay.
//
// The grant an operator writes is one org tuple on the scraper's service
// account, because an org role reaches every device in the org without a tuple
// per device. That is why this asks for the whole directory rather than for a
// role: it reads what the caller actually reaches, so any shape of grant that
// covers the fleet satisfies it and no second definition of "fleet-wide" is
// written down here.
//
// An empty directory is permitted. A deployment that has seen no device names
// none, so there is nothing to withhold.
//
// The decision is resolved in the query, in one statement. Counting the
// authorized devices and comparing that to a second count of the directory
// would be two statements, and a device registering between them makes the pair
// agree when the graph does not.
func (s *Store) CanReadFleetTelemetry(ctx context.Context, subject Subject) (bool, error) {
	mask, err := checkable(ctx, subject, ActionObserve)
	if err != nil {
		return false, err
	}

	var unseen int

	err = s.db.QueryRowContext(
		ctx, s.q(unseenDevicesSQL),
		subject.typ.String(), subject.id, int64(mask),
	).Scan(&unseen)
	if err != nil {
		return false, storeFailure("count unseen devices", err)
	}

	return unseen == 0, nil
}

// CountDevices returns how many devices ListDevices would return over every
// page, under the same predicate and the same join. It is a second statement,
// so a grant written between the two calls moves the total without moving the
// page: a caller that needs them consistent reads Epoch on either side and
// repeats the pair if it moved.
func (s *Store) CountDevices(ctx context.Context, subject Subject, action Action) (int, error) {
	mask, err := checkable(ctx, subject, action)
	if err != nil {
		return 0, err
	}

	if domain, _ := action.domain(); domain != TypeDevice {
		return 0, unsupportedObject.New("this action is not defined on a device").
			With(paramType(TypeDevice), paramAction(action))
	}

	var total int

	err = s.db.QueryRowContext(
		ctx, s.q(countDevicesSQL),
		subject.typ.String(), subject.id, int64(mask),
	).Scan(&total)
	if err != nil {
		return 0, storeFailure("count devices", err)
	}

	return total, nil
}

// Bounds clamps a page to something a database can be asked for: a zero or
// negative limit is the default page size, a limit above the cap is the cap,
// and a negative offset is the first page.
//
// It is exported because the fleet listing has to window rows this query cannot
// window for it. Liveness is process state and no statement can join against
// it, so the caller applies the same limit and offset over what it kept, and a
// second copy of these rules is how the two answers drift apart.
func (p Page) Bounds() (limit, offset int) {
	limit = p.Limit
	if limit <= 0 {
		limit = defaultPageLimit
	}

	if limit > maxPageLimit {
		limit = maxPageLimit
	}

	offset = max(p.Offset, 0)

	return limit, offset
}
