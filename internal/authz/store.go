package authz

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/prusa3d/prusa-connect/internal/kernel/machinetext"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
)

// Dialect names the SQL engine behind a Store. Both are first class: every
// query, migration and measurement runs against both.
type Dialect uint8

// The supported engines. The zero value names none, so a Store cannot be built
// without saying which engine it talks to.
const (
	DialectSQLite Dialect = iota + 1
	DialectPostgres
)

// ErrUnsupportedDialect reports a Store built without a known engine.
var ErrUnsupportedDialect = errors.New("authz: unsupported sql dialect")

// ErrNoDatabase reports a Store built without a database handle.
var ErrNoDatabase = errors.New("authz: nil database handle")

// ErrPostgresUnlinked reports a configuration asking for Postgres from a binary
// that carries no Postgres driver. The driver is behind the postgres build tag,
// which a configuration naming that engine derives through adapters/tags.json,
// so this is what a binary built for one engine and pointed at the other says.
var ErrPostgresUnlinked = errors.New("authz: this build carries no postgres driver")

// Store holds the tuples, the device directory and the projection they are
// compiled into. It owns no connection lifecycle: the caller opens the database
// and keeps the handle, so authorization writes land in the same transaction
// discipline as the domain writes beside them.
type Store struct {
	db      *sql.DB
	dialect Dialect

	// orgAdminHandover opens the org handover on the grant surface. Off, an
	// org's admin role cannot be written through the surface and a deployment
	// whose only administrator locks themselves out recovers by changing
	// core.principal and restarting. On, that grant is writable and the org
	// edge on an object carrying actions is not, because a caller could
	// otherwise escalate through that edge. See adminNotConferrable.
	orgAdminHandover bool

	// epoch is the generation of the graph, cached for the one caller that
	// cannot afford a query: the stream hub reads it on every subscriber's
	// tick. It is written after the transaction that moved the row commits, so
	// a rolled-back write never becomes a visible generation. See Epoch.
	epoch atomic.Uint64
}

// queryer is the part of database/sql both a *sql.DB and a *sql.Tx satisfy, so
// one query body serves the pooled path and the transactional one.
type queryer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

const (
	insertTupleSQL = `INSERT INTO authz_tuple
		(object_type, object_id, relation, subject_type, subject_id, subject_relation)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT DO NOTHING`

	deleteTupleSQL = `DELETE FROM authz_tuple
		WHERE object_type = ? AND object_id = ? AND relation = ?
		  AND subject_type = ? AND subject_id = ? AND subject_relation = ?`

	selectTuplesSQL = `SELECT object_type, object_id, relation, subject_type, subject_id, subject_relation
		FROM authz_tuple`

	// A rename has to move the row in the sort order, so it is a change; the
	// same name written twice is not, and must not bump the epoch.
	upsertDeviceSQL = `INSERT INTO authz_device (device_id, name, sort_key, kind) VALUES (?, ?, ?, ?)
		ON CONFLICT (device_id) DO UPDATE SET name = excluded.name, sort_key = excluded.sort_key
		WHERE authz_device.name <> excluded.name`

	// What kind the directory already holds this id under, read inside the
	// writing transaction so that a kind decided against the pool cannot be
	// stale by the time the row is written.
	selectDeviceKindSQL = `SELECT kind FROM authz_device WHERE device_id = ?`

	deleteDeviceSQL = `DELETE FROM authz_device WHERE device_id = ?`

	// Unregistering a device takes its grants with it. An operator-chosen id
	// is reusable after an RMA, and a reused id must not inherit the grants of
	// the machine it replaced.
	deleteDeviceTuplesSQL = `DELETE FROM authz_tuple WHERE object_type = 'device' AND object_id = ?`

	// The directory, with the kind each row carries. The kind is read because
	// the verb table is keyed by device kind.
	selectDevicesSQL = `SELECT device_id, kind FROM authz_device`

	upsertResourceSQL = `INSERT INTO authz_resource (kind, resource_id) VALUES (?, ?)
		ON CONFLICT DO NOTHING`

	deleteResourceSQL = `DELETE FROM authz_resource WHERE kind = ? AND resource_id = ?`

	bumpEpochSQL = `UPDATE authz_epoch SET epoch = epoch + 1 WHERE id = 1`

	selectEpochSQL = `SELECT epoch FROM authz_epoch WHERE id = 1`
)

// Option settles something about a Store that has no useful default and no
// place in the schema.
type Option func(*Store)

// WithOrgAdminHandover opens the org handover on the grant surface.
//
// Off is the default and the deployment the product ships: the admin role on an
// org is not writable through the grant surface, so an administrator who
// locks themselves out recovers by changing core.principal and restarting.
//
// On, that grant is writable and a write of the org edge on an object carrying
// actions is refused instead. The two move together under one flag, because
// the second refusal makes the first grant safe to write. See
// adminNotConferrable and orgEdgeNotWritable.
func WithOrgAdminHandover(open bool) Option {
	return func(s *Store) { s.orgAdminHandover = open }
}

// Open returns a Store over an already-open database whose schema the host has
// migrated, Migrations included. Call Reconcile before the first write.
func Open(db *sql.DB, dialect Dialect, options ...Option) (*Store, error) {
	if db == nil {
		return nil, ErrNoDatabase
	}

	if dialect != DialectSQLite && dialect != DialectPostgres {
		return nil, fmt.Errorf("%w: %d", ErrUnsupportedDialect, dialect)
	}

	store := &Store{db: db, dialect: dialect}
	for _, option := range options {
		option(store)
	}

	return store, nil
}

// Write stores tuples and recompiles the projection, both in one transaction.
// A tuple already present is left alone. Writing nothing is not an error and
// still bumps nothing.
func (s *Store) Write(ctx context.Context, tuples ...Tuple) error {
	if len(tuples) == 0 {
		return nil
	}

	return s.mutate(ctx, func(ctx context.Context, tx *sql.Tx, moved *scope) (int64, error) {
		var written int64

		for _, tuple := range tuples {
			if tuple.IsZero() {
				return 0, unknownType.New("write of an unconstructed tuple")
			}

			moved.moved(tuple.object)

			result, err := tx.ExecContext(ctx, s.q(insertTupleSQL), tupleArgs(tuple)...)
			if err != nil {
				return 0, storeFailure("insert tuple", err)
			}

			affected, err := result.RowsAffected()
			if err != nil {
				return 0, storeFailure("count inserted tuples", err)
			}

			written += affected
		}

		return written, nil
	})
}

// Delete removes tuples and recompiles the projection in the same transaction,
// so a revoked grant is gone from every surface at the same instant. Deleting a
// tuple that is not there is not an error.
func (s *Store) Delete(ctx context.Context, tuples ...Tuple) error {
	if len(tuples) == 0 {
		return nil
	}

	return s.mutate(ctx, func(ctx context.Context, tx *sql.Tx, moved *scope) (int64, error) {
		var removed int64

		for _, tuple := range tuples {
			if tuple.IsZero() {
				return 0, unknownType.New("delete of an unconstructed tuple")
			}

			moved.moved(tuple.object)

			result, err := tx.ExecContext(ctx, s.q(deleteTupleSQL), tupleArgs(tuple)...)
			if err != nil {
				return 0, storeFailure("delete tuple", err)
			}

			affected, err := result.RowsAffected()
			if err != nil {
				return 0, storeFailure("count deleted tuples", err)
			}

			removed += affected
		}

		return removed, nil
	})
}

// UpsertDevice registers a device in the directory the list joins, or renames
// one already there. A device that is not registered is invisible to every
// surface, however many tuples name it.
func (s *Store) UpsertDevice(ctx context.Context, id, name string) error {
	return s.UpsertDevices(ctx, DeviceRow{ID: id, Name: name})
}

// UpsertDevices registers or renames devices, recompiling the projection
// once for the whole batch.
//
// A row's kind is fixed at registration. The directory is one id space, so a
// camera registered over a printer of the same id would be that printer as far
// as every grant, every list and every projected row is concerned, and the
// operator who chose the two names would see nothing said about it. That write
// is refused instead, which is what ErrDeviceKindConflict reports.
func (s *Store) UpsertDevices(ctx context.Context, devices ...DeviceRow) error {
	if len(devices) == 0 {
		return nil
	}

	for _, device := range devices {
		if err := validDeviceRow(device); err != nil {
			return err
		}
	}

	return s.mutate(ctx, func(ctx context.Context, tx *sql.Tx, moved *scope) (int64, error) {
		var written int64

		for _, device := range devices {
			moved.movedDevice(device.ID)

			affected, err := s.registerDevice(ctx, tx, device)
			if err != nil {
				return 0, err
			}

			written += affected
		}

		return written, nil
	})
}

// RegisterDevice registers one device and writes the structure it arrives in,
// in one transaction.
//
// It exists because a machine dialling in is two writes that must not be two
// mutations. Every mutation recompiles the device, moves the epoch and commits,
// so registering the device and placing it in its org separately pays for all
// three twice per machine on the single writer SQLite has. See
// BenchmarkDeviceArrival for both numbers.
//
// Two transactions would also leave a window. Between them the device is in
// the directory and in no org, so nobody may see it and no grant explains why.
//
// A device a person adds, such as a camera, goes through DeclareDevice instead.
func (s *Store) RegisterDevice(ctx context.Context, device DeviceRow, structure ...Tuple) error {
	if err := validDeviceRow(device); err != nil {
		return err
	}

	for _, tuple := range structure {
		if tuple.IsZero() {
			return unknownType.New("write of an unconstructed tuple")
		}
	}

	return s.mutate(ctx, func(ctx context.Context, tx *sql.Tx, moved *scope) (int64, error) {
		moved.movedDevice(device.ID)

		changed, err := s.registerDevice(ctx, tx, device)
		if err != nil {
			return 0, err
		}

		for _, tuple := range structure {
			moved.moved(tuple.object)

			written, execErr := s.rowsAffected(ctx, tx, insertTupleSQL, "insert tuple", tupleArgs(tuple)...)
			if execErr != nil {
				return 0, execErr
			}

			changed += written
		}

		return changed, nil
	})
}

// validDeviceRow refuses a registration before a transaction opens. A row that
// names no legal object, carries an unusable name or claims a kind this build
// does not carry is the caller's mistake, and finding it with the write lock
// held costs every other writer.
func validDeviceRow(device DeviceRow) error {
	if _, err := NewObject(TypeDevice, device.ID); err != nil {
		return err
	}

	if err := validateDeviceName(device.Name); err != nil {
		return err
	}

	if !KnownKind(device.kind()) {
		return invalidID.New(
			"a device is registered under a device kind this build does not carry. A kind exists "+
				"here because something compiled its declaration in").
			With(paramID(device.ID), paramDeviceKind(device.kind()))
	}

	return nil
}

// registerDevice writes one directory row inside the caller's transaction and
// reports whether it moved anything. Renaming moves it; writing the same name
// twice does not, which is what keeps a re-registration from bumping the epoch.
func (s *Store) registerDevice(ctx context.Context, tx *sql.Tx, device DeviceRow) (int64, error) {
	if err := s.holdKind(ctx, tx, device); err != nil {
		return 0, err
	}

	return s.rowsAffected(ctx, tx, upsertDeviceSQL, "upsert device",
		device.ID, device.Name, sortKey(device.Name), device.kind().String())
}

// holdKind refuses a registration that would change what an id names. It reads
// inside the writing transaction, so a kind decided against the pool cannot be
// stale by the time the row is written.
func (s *Store) holdKind(ctx context.Context, tx *sql.Tx, device DeviceRow) error {
	var held string

	err := tx.QueryRowContext(ctx, s.q(selectDeviceKindSQL), device.ID).Scan(&held)

	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil
	case err != nil:
		return storeFailure("read the registered kind", err)
	case held == device.kind().String():
		return nil
	default:
		return deviceKindConflict.New("that id already names a device of another kind").
			With(paramID(device.ID), paramDeviceKind(ref.DeviceKind(held)))
	}
}

// kind is the kind this row registers under. The zero value is printer.
func (r DeviceRow) kind() ref.DeviceKind {
	if r.Kind == "" {
		return ref.DeviceKindPrinter
	}

	return r.Kind
}

// RemoveDevice unregisters a device and deletes every tuple written on it.
// Both halves are one transaction: an operator-chosen id is reusable after a
// replacement, and re-registering it must not resurrect the grants of the
// machine that carried it before.
func (s *Store) RemoveDevice(ctx context.Context, id string) error {
	if _, err := NewObject(TypeDevice, id); err != nil {
		return err
	}

	return s.mutate(ctx, func(ctx context.Context, tx *sql.Tx, moved *scope) (int64, error) {
		moved.movedDevice(id)

		var removed int64

		for _, query := range []string{deleteDeviceSQL, deleteDeviceTuplesSQL} {
			result, err := tx.ExecContext(ctx, s.q(query), id)
			if err != nil {
				return 0, storeFailure("remove device", err)
			}

			affected, err := result.RowsAffected()
			if err != nil {
				return 0, storeFailure("count removed rows", err)
			}

			removed += affected
		}

		return removed, nil
	})
}

// RegisterResources records resource instances in the directory every resource
// list and every instance point check joins. An instance absent here is absent
// from both, however many grants name its kind, which is what makes a refusal
// for an instance that does not exist the same refusal as one for an instance
// the caller may not see.
//
// It takes the whole set in one call because startup registers the merged
// configuration at once, and one call is one transaction, one epoch bump and
// one recompile.
func (s *Store) RegisterResources(ctx context.Context, instances ...ref.Ref) error {
	if len(instances) == 0 {
		return nil
	}

	for _, instance := range instances {
		if err := configurableInstance(instance); err != nil {
			return err
		}
	}

	return s.mutate(ctx, func(ctx context.Context, tx *sql.Tx, moved *scope) (int64, error) {
		moved.movedNoProjection()

		var written int64

		for _, instance := range instances {
			result, err := tx.ExecContext(
				ctx, s.q(upsertResourceSQL), instance.Kind().String(), instance.ID(),
			)
			if err != nil {
				return 0, storeFailure("register resource", err)
			}

			affected, err := result.RowsAffected()
			if err != nil {
				return 0, storeFailure("count registered resources", err)
			}

			written += affected
		}

		return written, nil
	})
}

// SyncResources makes the directory hold exactly these instances: it registers
// what is missing and unregisters what nobody declares any more. One
// transaction, one epoch bump and one recompile, whatever moved.
//
// One transaction rather than one per removed instance. A removal per
// transaction is a full reprojection each time, and it leaves the directory
// half-reconciled if the process stops in the middle: rows for instances nobody
// declares are resources that exist to the graph and to nothing else, and the
// next start reads the directory rather than a document.
//
// The directory is read inside the writing transaction, so the diff cannot be
// taken against a state another writer has already moved past.
//
// It removes no tuple, for the reason RemoveResource gives.
func (s *Store) SyncResources(ctx context.Context, instances ...ref.Ref) error {
	for _, instance := range instances {
		if err := configurableInstance(instance); err != nil {
			return err
		}
	}

	declared := make(map[ref.Ref]struct{}, len(instances))
	for _, instance := range instances {
		declared[instance] = struct{}{}
	}

	return s.mutate(ctx, func(ctx context.Context, tx *sql.Tx, moved *scope) (int64, error) {
		moved.movedNoProjection()

		registered, err := s.resources(ctx, tx)
		if err != nil {
			return 0, err
		}

		var changed int64

		withdrawn, err := s.withdrawResources(ctx, tx, registered, declared)
		if err != nil {
			return 0, err
		}

		registrations, err := s.registerResources(ctx, tx, moved, instances)
		if err != nil {
			return 0, err
		}

		return changed + withdrawn + registrations, nil
	})
}

// withdrawResources removes the directory rows of every instance the
// configuration no longer declares.
func (s *Store) withdrawResources(
	ctx context.Context, tx *sql.Tx, registered []ref.Ref, declared map[ref.Ref]struct{},
) (int64, error) {
	var changed int64

	for _, instance := range registered {
		if _, still := declared[instance]; still {
			continue
		}

		removed, err := s.rowsAffected(ctx, tx, deleteResourceSQL,
			"remove resource", instance.Kind().String(), instance.ID())
		if err != nil {
			return 0, err
		}

		changed += removed
	}

	return changed, nil
}

// registerResources writes the directory row of every declared instance, and
// the first grant of every kind whose principal must not start out starved.
func (s *Store) registerResources(
	ctx context.Context, tx *sql.Tx, moved *scope, instances []ref.Ref,
) (int64, error) {
	var changed int64

	for _, instance := range instances {
		written, err := s.rowsAffected(ctx, tx, upsertResourceSQL,
			"register resource", instance.Kind().String(), instance.ID())
		if err != nil {
			return 0, err
		}

		granted, err := s.grantFirstSight(ctx, tx, moved, instance, written)
		if err != nil {
			return 0, err
		}

		changed += written + granted
	}

	return changed, nil
}

// rowsAffected runs one statement of the directory and reports how many rows it
// moved, so a caller counting a mixed batch writes the error handling once.
func (s *Store) rowsAffected(
	ctx context.Context, tx *sql.Tx, query, what string, args ...any,
) (int64, error) {
	result, err := tx.ExecContext(ctx, s.q(query), args...)
	if err != nil {
		return 0, storeFailure(what, err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return 0, storeFailure("count rows for "+what, err)
	}

	return affected, nil
}

// RemoveResource unregisters one instance. It removes no tuple, because the
// vocabulary writes no tuple on an instance: a grant is held on the kind, and
// deleting an instance neither narrows nor widens it. What it does move is the
// epoch, because the set of origins a live subscription may read has changed.
func (s *Store) RemoveResource(ctx context.Context, instance ref.Ref) error {
	if err := configurableInstance(instance); err != nil {
		return err
	}

	return s.mutate(ctx, func(ctx context.Context, tx *sql.Tx, moved *scope) (int64, error) {
		moved.movedNoProjection()

		result, err := tx.ExecContext(
			ctx, s.q(deleteResourceSQL), instance.Kind().String(), instance.ID(),
		)
		if err != nil {
			return 0, storeFailure("remove resource", err)
		}

		removed, err := result.RowsAffected()
		if err != nil {
			return 0, storeFailure("count removed resources", err)
		}

		return removed, nil
	})
}

// configurableInstance refuses a ref the resource surface does not govern: an
// unconstructed one, or one naming a kind whose instances the configuration
// plane does not own. A printer is authorized as a printer and never as a
// resource, and letting one through here would be a second, weaker path to it.
func configurableInstance(instance ref.Ref) error {
	if instance.IsZero() {
		return unknownType.New("resource reference was never constructed").
			With(paramType(TypeResourceKind))
	}

	if !instance.Kind().Configurable() {
		return unknownType.New("the resource surface does not govern this kind").
			With(paramType(Type(instance.Kind().String())))
	}

	return nil
}

// Tuples returns every stored tuple. It is the raw read: no relation is
// resolved and no rewrite is applied, which is what makes it usable for an
// audit answer and for a differential test.
func (s *Store) Tuples(ctx context.Context) ([]Tuple, error) {
	return loadTuples(ctx, s.db, s.dialect)
}

// Epoch returns the permissions epoch, bumped by every write that changes the
// graph or what is visible in it. A cached decision or a live subscription
// stamped with an older value has to be re-authorized before it is trusted
// again.
//
// It is one atomic load and never a query, because the stream hub reads it on
// every subscriber's tick, and a query per tick costs too much there. The
// value is published only after the transaction that moved it commits.
//
// A caller pairing it with a visibility read must read this first and the rows
// second. In that order a write that lands in between leaves the stamp older
// than the graph, so the reader re-authorizes once too often; in the other
// order it leaves the stamp newer than the rows it describes, and the change is
// never noticed. Visible does it in the safe order.
//
// The cache is this process's. A second writer against the same database moves
// the row without moving this.
func (s *Store) Epoch() uint64 { return s.epoch.Load() }

// readEpoch reads the counter from its row. It is the authoritative value, and
// the one Epoch is seeded and refreshed from.
//
// It scans into the unsigned type the counter is, so a negative value, which
// only a hand-edited or corrupted row can hold, is refused by the driver's
// range check rather than wrapping into a generation every stamp is behind.
func (s *Store) readEpoch(ctx context.Context, q queryer) (uint64, error) {
	var epoch uint64
	if err := q.QueryRowContext(ctx, s.q(selectEpochSQL)).Scan(&epoch); err != nil {
		return 0, storeFailure("read epoch", err)
	}

	return epoch, nil
}

// Rebuild recompiles the whole projection from the tuples. Every mutation does
// this already; it is exported for the case where the tuple table was loaded
// out of band.
func (s *Store) Rebuild(ctx context.Context) error {
	return s.mutate(ctx, func(context.Context, *sql.Tx, *scope) (int64, error) { return 1, nil })
}

// mutate runs fn and, if fn changed anything, bumps the epoch and recompiles
// the projection in the same transaction, so no surface can observe tuples and
// projection disagreeing. fn reports how many rows it changed: a write that
// stores a tuple already present changes nothing, and must not invalidate every
// cached decision in the deployment for a no-op.
//
// fn also reports which objects it moved, on the scope it is handed, and the
// recompile is scoped to those. A fn that reports nothing gets the whole
// recompile, so forgetting is slow rather than wrong. See scope.
//
// The epoch update takes a row lock, so writers serialise on it and the epoch
// is a total order over permission changes. It cannot go backwards. A missing
// seed row is reported here as an error, so the bump is never skipped
// silently.
//
// The epoch is one counter for the whole graph, and a scoped recompile does
// not narrow it. It is one atomic load on the stream hub's per-subscriber tick.
// A generation per principal would make that a keyed read there, and a row
// written per principal here. So a fleet arriving moves the epoch once per
// machine, for every reader.
func (s *Store) mutate(
	ctx context.Context, fn func(context.Context, *sql.Tx, *scope) (int64, error),
) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return storeFailure("begin transaction", err)
	}

	// Rollback after a commit is a no-op, and its error on the failure path
	// says nothing the returned error does not already say.
	defer func() { _ = tx.Rollback() }()

	// The bump comes first so that its row lock is held while fn decides, and
	// not only while fn writes. fn carries the containment rules, and each of
	// them reads the graph and refuses a mutation by what it produces. On
	// Postgres a transaction opens at READ COMMITTED, so two callers deciding
	// at once each read a snapshot with the other's write absent, both rules
	// pass and both writes land. Taking the lock here makes the decision and
	// the write one serialised unit on both engines. On SQLite
	// _txlock=immediate gives the same result, but that is a property of the
	// DSN and not of this rule. See
	// TestTwoConcurrentWithdrawalsCannotStrandTheObject.
	//
	// A mutation that turns out to change nothing rolls the bump back with the
	// rest of the transaction, so a no-op still publishes no generation.
	if err = bumpEpoch(ctx, tx, s.dialect); err != nil {
		return err
	}

	// Read inside the transaction that moved it, so the cache holds the value
	// this write produced rather than whatever a concurrent writer left.
	epoch, err := s.readEpoch(ctx, tx)
	if err != nil {
		return err
	}

	var moved scope

	changed, err := fn(ctx, tx, &moved)
	if err != nil {
		return err
	}

	if changed == 0 {
		return nil
	}

	// The scoped recompile first, and the whole one from here when it declines.
	// Running the fallback from inside reprojectScope costs 40 ms a mutation on
	// the pure-Go SQLite driver and nothing on Postgres, for reasons that are
	// not work either path does. reprojectScope's doc has the measurement.
	scoped, err := s.reprojectScope(ctx, tx, &moved, epoch)
	if err != nil {
		return err
	}

	if !scoped {
		if err = s.reproject(ctx, tx, epoch); err != nil {
			return err
		}
	}

	if err = tx.Commit(); err != nil {
		return storeFailure("commit transaction", err)
	}

	s.publishEpoch(epoch)

	return nil
}

// publishEpoch moves the cache to the generation this transaction produced, and
// never backwards.
//
// The row lock orders the transactions, so they commit in the order they bumped
// the counter. It does not order what happens after the commit returns: two
// writers can be descheduled there and store their values in either order. A
// plain store then leaves the cache a generation behind for good, because
// nothing reads the row again until the next write, and the stream hub takes
// every subscriber's re-read decision off this value. A subscriber would keep a
// grant that was withdrawn.
//
// The loop retries only against another writer that moved it in between, so it
// is uncontended in the ordinary case.
func (s *Store) publishEpoch(epoch uint64) {
	for {
		held := s.epoch.Load()
		if epoch <= held {
			return
		}

		if s.epoch.CompareAndSwap(held, epoch) {
			return
		}
	}
}

// bumpEpoch moves the counter every cached decision and live subscription is
// stamped with. A missing seed row would silently stop it moving, which is the
// failure mode that makes stale authorization invisible, so it is an error.
func bumpEpoch(ctx context.Context, tx *sql.Tx, dialect Dialect) error {
	result, err := tx.ExecContext(ctx, dialect.render(bumpEpochSQL))
	if err != nil {
		return storeFailure("bump epoch", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return storeFailure("count epoch bump", err)
	}

	if affected == 0 {
		return missingEpoch.New("the authz_epoch seed row is missing")
	}

	return nil
}

// q renders a query for this store's dialect.
func (s *Store) q(query string) string { return s.dialect.render(query) }

// render rewrites a query for this dialect. The queries in this package are
// written with '?' placeholders and contain no literal '?', so renumbering them
// for Postgres is mechanical and touches nothing but the placeholders.
func (d Dialect) render(query string) string {
	if d != DialectPostgres {
		return query
	}

	var b strings.Builder

	b.Grow(len(query) + strings.Count(query, "?"))

	n := 0

	for i := range len(query) {
		if query[i] != '?' {
			b.WriteByte(query[i])

			continue
		}

		n++

		b.WriteByte('$')
		b.WriteString(strconv.Itoa(n))
	}

	return b.String()
}

func tupleArgs(t Tuple) []any {
	return []any{
		t.object.typ.String(), t.object.id, t.relation.String(),
		t.subject.typ.String(), t.subject.id, t.subject.rel.String(),
	}
}

func loadTuples(ctx context.Context, q queryer, dialect Dialect) ([]Tuple, error) {
	return queryTuples(ctx, q, dialect, selectTuplesSQL)
}

// queryTuples runs one of this package's tuple reads and rebuilds every row
// through the constructors. The whole table and one object's tuples are the
// same scan, so the two paths cannot differ in what they refuse.
func queryTuples(
	ctx context.Context, q queryer, dialect Dialect, query string, args ...any,
) ([]Tuple, error) {
	rows, err := q.QueryContext(ctx, dialect.render(query), args...)
	if err != nil {
		return nil, storeFailure("read tuples", err)
	}

	defer func() { _ = rows.Close() }() // the scan error below is the one worth reporting

	var out []Tuple

	for rows.Next() {
		var objectType, objectID, relation, subjectType, subjectID, subjectRelation string
		if err = rows.Scan(
			&objectType, &objectID, &relation, &subjectType, &subjectID, &subjectRelation,
		); err != nil {
			return nil, storeFailure("scan tuple", err)
		}

		tuple, buildErr := storedTuple(objectType, objectID, relation, subjectType, subjectID, subjectRelation)
		if buildErr != nil {
			return nil, buildErr
		}

		out = append(out, tuple)
	}

	if err = rows.Err(); err != nil {
		return nil, storeFailure("read tuples", err)
	}

	return out, nil
}

// storedTuple rebuilds a tuple from a row through the same constructor a caller
// goes through. A row that reached the table another way, from a restored
// backup, an import or a hand-edited database, is refused here rather than
// resolved as whatever it resembles: the vocabulary is a property of the graph,
// not of one code path into it.
func storedTuple(objectType, objectID, relation, subjectType, subjectID, subjectRelation string) (Tuple, error) {
	object, err := NewObject(Type(objectType), objectID)
	if err != nil {
		return Tuple{}, invalidStoredTuple.New("stored object is not in the vocabulary").Wrap(err)
	}

	subject, err := storedSubject(subjectType, subjectID, subjectRelation)
	if err != nil {
		return Tuple{}, err
	}

	tuple, err := NewTuple(object, Relation(relation), subject)
	if err != nil {
		return Tuple{}, invalidStoredTuple.New("stored tuple is not in the vocabulary").Wrap(err)
	}

	return tuple, nil
}

func storedSubject(subjectType, subjectID, subjectRelation string) (Subject, error) {
	if subjectRelation == "" {
		subject, err := NewSubject(Type(subjectType), subjectID)
		if err != nil {
			return Subject{}, invalidStoredTuple.New("stored subject is not in the vocabulary").Wrap(err)
		}

		return subject, nil
	}

	subject, err := NewUserset(Type(subjectType), subjectID, Relation(subjectRelation))
	if err != nil {
		return Subject{}, invalidStoredTuple.New("stored userset is not in the vocabulary").Wrap(err)
	}

	return subject, nil
}

// maxDeviceNameLen bounds what the directory stores. A name is free text from
// an operator, and it is sorted and returned to every caller.
const maxDeviceNameLen = 256

func validateDeviceName(name string) error {
	if name == "" {
		return invalidDeviceName.New("device name is empty").With(paramName(name))
	}

	if len(name) > maxDeviceNameLen {
		// machinetext.Clip and not a slice expression. The param reaches the
		// caller inside a JSON error body, and a device name is text somebody
		// chose in their own language, so a cut at byte 256 can land inside a
		// character and the encoder would write U+FFFD.
		return invalidDeviceName.New("device name is too long").
			With(paramName(machinetext.Clip(name, maxDeviceNameLen)))
	}

	return nil
}

// sortKey is what the list orders by. Ordering by the name itself would order
// by the database collation, which differs between the engines and between two
// Postgres installations, so the same page would hold different rows. The key
// is case-folded here and compared as bytes on both engines, which makes the
// order a property of this function rather than of the server it runs on.
func sortKey(name string) string { return strings.ToLower(name) }

func loadDevices(ctx context.Context, q queryer, dialect Dialect) ([]directoryRow, error) {
	rows, err := q.QueryContext(ctx, dialect.render(selectDevicesSQL))
	if err != nil {
		return nil, storeFailure("read device directory", err)
	}

	defer func() { _ = rows.Close() }() // the scan error below is the one worth reporting

	var out []directoryRow

	for rows.Next() {
		var row directoryRow
		if err = rows.Scan(&row.id, &row.kind); err != nil {
			return nil, storeFailure("scan device", err)
		}

		out = append(out, row)
	}

	if err = rows.Err(); err != nil {
		return nil, storeFailure("read device directory", err)
	}

	return out, nil
}
