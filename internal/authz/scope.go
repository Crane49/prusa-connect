package authz

import (
	"context"
	"database/sql"
	"slices"
	"strings"
)

const (
	// Every object that names this one as its subject. The walk below reads the
	// graph downwards, and every downward edge is stored on the child: a device
	// names its parent, a device group names its parent and its org, a resource
	// kind names its org. So the reverse of every one of them is this one
	// query, and authz_tuple_by_subject is exactly its prefix.
	//
	// The limit is what keeps a fleet-wide write cheap to recognise. An org
	// names every device under it, and reading four hundred rows to conclude
	// that the whole recompile is the cheaper path is four hundred rows wasted.
	selectDependentsSQL = `SELECT object_type, object_id FROM authz_tuple
		WHERE subject_type = ? AND subject_id = ?
		LIMIT ?`

	// Every tuple written on one object, which is the primary key's prefix.
	selectTuplesOnSQL = selectTuplesSQL + ` WHERE object_type = ? AND object_id = ?`

	deleteDeviceAccessSQL = `DELETE FROM device_access WHERE device_id = ?`

	deleteResourceAccessSQL = `DELETE FROM resource_access WHERE kind = ?`

	// Which of these ids the directory still holds, and what kind each one is. A
	// device absent from it projects no row, exactly as it does in a whole
	// recompile. The kind comes back with the id because the verb table is keyed
	// by device kind. A scoped recompile that knew an id and not its kind could
	// not say which verbs exist on the device, and it would disagree with the
	// whole recompile.
	selectDirectoryRowsPrefix = `SELECT device_id, kind FROM authz_device WHERE device_id IN (`
)

// scopedCeiling bounds how many objects one scoped recompile may visit before
// it gives up and recompiles the whole graph instead.
//
// The scoped path costs one indexed query per object it visits and the whole
// recompile costs one query for every tuple there is, so the two cross over.
// A grant written on an org is the case that crosses it: every device in the
// org and every resource kind under it moves, which is the recompile the whole
// path already does in two queries. Sixty-four keeps a device arrival, a grant
// on a device and a declared camera well inside the scoped path, and sends the
// fleet-wide write to the path that is cheaper for it.
const scopedCeiling = 64

// scope is what one mutation moved, collected inside the transaction that moved
// it. The projection is then recompiled for those objects and for whatever
// hangs below them, rather than for the whole graph.
//
// The zero value asks for a whole recompile. A mutation that reports nothing is
// therefore slow and never wrong. Every enforcement surface reads this table,
// so slow is the safe failure.
type scope struct {
	reported bool
	// whole asks for the recompile of everything, whatever objects were named.
	// It is for a mutation that moves no tuple and still changes what the
	// projected rows should be. A walk over the objects such a mutation named
	// would visit nothing and leave stale rows on every device in the fleet.
	whole   bool
	objects []Object
}

// moved records an object a mutation wrote a tuple on, or whose directory row
// it changed. Naming an object that did not move costs one recompile of that
// object and nothing else, so a caller in doubt names it.
func (s *scope) moved(object Object) {
	s.reported = true
	s.objects = append(s.objects, object)
}

// movedDevice is moved for a directory row, which carries an id and not an
// object. An id the vocabulary refuses cannot be in the directory, so a
// refusal here is a whole recompile rather than an error.
func (s *scope) movedDevice(id string) {
	object, err := NewObject(TypeDevice, id)
	if err != nil {
		return
	}

	s.moved(object)
}

// movedNoProjection records a mutation that changed no tuple and no device
// directory row. The resource directory is the one such write: the projection
// is compiled over the closed kind set and never over the instances, so
// registering an instance moves the epoch and no projected row.
func (s *scope) movedNoProjection() { s.reported = true }

// reprojectScope recompiles the projection for what this mutation moved, and
// reports whether it did. It answers false for a mutation that reported nothing
// and for one whose reach is wide enough that recompiling object by object costs
// more than recompiling everything. Its caller runs the whole recompile then.
//
// The caller runs the whole recompile, and this function does not. Run from
// here it costs a measured 40 ms more per mutation on the pure-Go SQLite
// driver, on a fleet-wide grant that takes the fallback.
// BenchmarkOrgGrantWriteAndWithdraw reads 224 ms with the fallback here and
// 137 ms with it in mutate. The same benchmark on Postgres is 527 ms either
// way, and removing every query from the walk below does not move the SQLite
// figure. So the cost is not work either path does, and its cause is not
// known.
//
// A scoped recompile must write the rows reproject would write. That follows
// from the compiler and not from this walk: project resolves each device and
// each kind on its own, so the projection is the union of the rows of every
// object in it. Recompiling a subset therefore writes exactly the rows a whole
// recompile would write for that subset, provided the subset holds every
// object whose rows moved. dependents computes that subset, and
// TestTheScopedProjectionMatchesAWholeRebuild compares the two after every
// shape of mutation.
func (s *Store) reprojectScope(
	ctx context.Context, tx *sql.Tx, moved *scope, epoch uint64,
) (bool, error) {
	if !moved.reported {
		return false, nil
	}

	// The scoped walk covers only the objects a mutation named, and this
	// mutation invalidated rows it did not name.
	if moved.whole {
		return false, nil
	}

	// Reported, and nothing projected moved. The resource directory is the one
	// such write, and it is done: no row of either projection depends on it.
	if len(moved.objects) == 0 {
		return true, nil
	}

	devices, kinds, whole, err := s.dependents(ctx, tx, moved.objects)
	if err != nil {
		return false, err
	}

	if whole {
		return false, nil
	}

	tuples, err := s.scopedTuples(ctx, tx, devices, kinds)
	if err != nil {
		return false, err
	}

	// The directory read covers every device the walk can touch and not only
	// the targets. reachDevice climbs the parent chain and reads each parent's
	// kind, so a parent left out here would expand its declared grants to
	// nothing while the whole recompile expands them to bits, and the two would
	// disagree on the same database.
	directory, err := s.directoryRows(ctx, tx, devicesReached(devices, tuples))
	if err != nil {
		return false, err
	}

	deviceKinds, deviceIDs := scopedDirectory(directory)

	verbs, err := s.loadVerbIndex(ctx, tx, deviceKinds, deviceIDs)
	if err != nil {
		return false, err
	}

	g := indexTuples(tuples)
	g.verbs = verbs

	err = s.writeScoped(ctx, tx, g, scopedTargets{
		devices:   devices,
		directory: directory,
		kinds:     kinds,
		epoch:     epoch,
	})

	return err == nil, err
}

// devicesReached is every device id a scoped compile can look at: the targets,
// and every device either side of a tuple it collected. A device names its
// parent on the subject side, so both sides are read.
func devicesReached(targets []string, tuples []Tuple) []string {
	seen := make(map[string]bool, len(targets))
	out := make([]string, 0, len(targets))

	add := func(id string) {
		if seen[id] {
			return
		}

		seen[id] = true

		out = append(out, id)
	}

	for _, id := range targets {
		add(id)
	}

	for _, tuple := range tuples {
		if tuple.object.typ == TypeDevice {
			add(tuple.object.id)
		}

		if tuple.subject.typ == TypeDevice {
			add(tuple.subject.id)
		}
	}

	return out
}

// scopedDirectory is the directory a scoped compile holds, in the two shapes it
// is read in: the kinds the declared index is built over, and the ids the
// declared grants are read for. The ids are sorted so two engines run the same
// statement for the same mutation.
func scopedDirectory(directory map[string]directoryRow) (map[string]string, []string) {
	kinds := make(map[string]string, len(directory))
	ids := make([]string, 0, len(directory))

	for id, row := range directory {
		kinds[id] = row.kind

		ids = append(ids, id)
	}

	slices.Sort(ids)

	return kinds, ids
}

// scopedTargets is what one scoped recompile writes: the devices and kinds
// whose rows are replaced, and which of the devices the directory still holds.
type scopedTargets struct {
	devices []string
	// directory is every device row the compile could read, keyed by id. A
	// target absent from it is a device the directory no longer holds, which
	// projects no row.
	directory map[string]directoryRow
	kinds     []string
	// epoch is the generation this mutation produced, which is where a sight
	// this recompile creates begins.
	epoch uint64
}

// dependents is every object whose projected rows can move when the objects in
// seeds do, reported as the devices and the resource kinds the two projections
// are keyed by.
//
// It walks one edge, in reverse: whatever names the current object as its
// subject. Every edge the compiler follows downwards is stored on the lower
// object, so that one query covers all of them. A device names the device it
// sits in; a device names its group and its org; a device group names its
// parent group and its org; a resource kind names its org; and a role written
// against group:x#member names the group, which is how a membership change
// reaches the objects that grant to that group.
//
// It reports whole when the walk passes scopedCeiling, which is the fleet-wide
// write the whole recompile is cheaper for.
func (s *Store) dependents(
	ctx context.Context, tx *sql.Tx, seeds []Object,
) ([]string, []string, bool, error) {
	var devices, kinds []string

	seen := make(map[Object]bool, len(seeds))
	queue := slices.Clone(seeds)

	for len(queue) > 0 {
		object := queue[0]
		queue = queue[1:]

		if seen[object] {
			continue
		}

		if len(seen) >= scopedCeiling {
			return nil, nil, true, nil
		}

		seen[object] = true

		// A resource kind is the one object no tuple names as a subject, so it
		// is the one that ends the walk rather than continuing it.
		if object.typ == TypeResourceKind {
			kinds = append(kinds, object.id)

			continue
		}

		if object.typ == TypeDevice {
			devices = append(devices, object.id)
		}

		below, err := s.objectsNaming(ctx, tx, object)
		if err != nil {
			return nil, nil, false, err
		}

		// One object naming as many dependents as the whole walk is allowed to
		// visit is the fleet-wide write. The read stopped at the limit, so how
		// many more there are is unknown and the whole recompile is the answer.
		if len(below) >= scopedCeiling {
			return nil, nil, true, nil
		}

		queue = append(queue, below...)
	}

	return devices, kinds, false, nil
}

// objectsNaming returns the objects holding a tuple whose subject is this one,
// whether the subject is written as a principal or as a userset. Both forms are
// wanted: a device group is named as a principal by the devices inside it, and
// a group is named as group:x#member by every role granted to it.
//
// It reads at most scopedCeiling rows, because its caller gives up at that
// number and the rows past it are read for nothing.
func (s *Store) objectsNaming(ctx context.Context, tx *sql.Tx, object Object) ([]Object, error) {
	rows, err := tx.QueryContext(
		ctx, s.q(selectDependentsSQL), object.typ.String(), object.id, scopedCeiling,
	)
	if err != nil {
		return nil, storeFailure("read the objects below one", err)
	}

	defer func() { _ = rows.Close() }() // the scan error below is the one worth reporting

	var out []Object

	for rows.Next() {
		var objectType, objectID string
		if err = rows.Scan(&objectType, &objectID); err != nil {
			return nil, storeFailure("scan an object below one", err)
		}

		// Through the constructor, exactly as a stored tuple is: a row that
		// reached the table another way is refused rather than compiled as
		// whatever it resembles.
		below, buildErr := NewObject(Type(objectType), objectID)
		if buildErr != nil {
			return nil, invalidStoredTuple.New("stored object is not in the vocabulary").Wrap(buildErr)
		}

		out = append(out, below)
	}

	if err = rows.Err(); err != nil {
		return nil, storeFailure("read the objects below one", err)
	}

	return out, nil
}

// scopedTuples reads the tuples one scoped recompile needs, and no others.
//
// It is the walk dependents takes, upwards. Compiling a device reads what is
// written on it, on every device it sits inside, on every group above it, on
// every org those name, and on every group named as a userset by any of those
// grants. Every one of those is an object, so collecting the whole set is a
// closure over the subjects of the tuples already collected. The compiler
// indexes tuples by object, so a set holding every object it will look up is
// enough for it to answer as it would with the whole table.
func (s *Store) scopedTuples(ctx context.Context, tx *sql.Tx, devices, kinds []string) ([]Tuple, error) {
	seen := make(map[Object]bool, len(devices)+len(kinds))
	queue := make([]Object, 0, len(devices)+len(kinds))

	for _, seed := range []struct {
		typ Type
		ids []string
	}{{typ: TypeDevice, ids: devices}, {typ: TypeResourceKind, ids: kinds}} {
		for _, id := range seed.ids {
			queue = append(queue, Object{typ: seed.typ, id: id})
		}
	}

	var out []Tuple

	for len(queue) > 0 {
		object := queue[0]
		queue = queue[1:]

		if seen[object] {
			continue
		}

		seen[object] = true

		written, err := queryTuples(ctx, tx, s.dialect, selectTuplesOnSQL, object.typ.String(), object.id)
		if err != nil {
			return nil, err
		}

		out = append(out, written...)

		for _, tuple := range written {
			if above, ok := above(tuple.subject); ok {
				queue = append(queue, above)
			}
		}
	}

	return out, nil
}

// above is the object a subject stands for when the compiler will read tuples
// written on it, and whether there is one. A principal is nobody's object and
// answers false; the four structural types are the graph's upward edges.
func above(subject Subject) (Object, bool) {
	switch subject.typ {
	case TypeDevice, TypeDeviceGroup, TypeOrg, TypeGroup:
		return Object{typ: subject.typ, id: subject.id}, true
	case TypeUser, TypeServiceAccount, TypeDeviceCredential, TypeNode, TypeSink, TypeHistory, TypeResourceKind:
		return Object{}, false
	default:
		return Object{}, false
	}
}

// directoryRows reads which of these ids the directory holds and what kind each
// one is, in one query. A device absent from the answer projects no row, which is
// what makes a grant on an unregistered device fail closed.
func (s *Store) directoryRows(
	ctx context.Context, tx *sql.Tx, devices []string,
) (map[string]directoryRow, error) {
	out := make(map[string]directoryRow, len(devices))

	if len(devices) == 0 {
		return out, nil
	}

	args := make([]any, 0, len(devices))
	for _, id := range devices {
		args = append(args, id)
	}

	query := selectDirectoryRowsPrefix + placeholders(len(devices)) + ")"

	rows, err := tx.QueryContext(ctx, s.q(query), args...)
	if err != nil {
		return nil, storeFailure("read the device directory", err)
	}

	defer func() { _ = rows.Close() }() // the scan error below is the one worth reporting

	for rows.Next() {
		var row directoryRow
		if err = rows.Scan(&row.id, &row.kind); err != nil {
			return nil, storeFailure("scan a device row", err)
		}

		out[row.id] = row
	}

	if err = rows.Err(); err != nil {
		return nil, storeFailure("read the device directory", err)
	}

	return out, nil
}

// writeScoped replaces the projected rows of these objects and leaves every
// other row alone. Each object is cleared and rewritten, so a principal who
// lost their access here loses the row rather than keeping a stale one.
func (s *Store) writeScoped(
	ctx context.Context, tx *sql.Tx, g *graph, targets scopedTargets,
) error {
	for _, id := range targets.devices {
		if err := s.writeScopedDevice(ctx, tx, g, targets, id); err != nil {
			return err
		}
	}

	compilable := configurableKinds()

	for _, kind := range targets.kinds {
		held, err := s.readSince(ctx, tx, selectResourceSinceOnSQL, int64(ActionViewResource.mask()), kind)
		if err != nil {
			return err
		}

		if _, err = tx.ExecContext(ctx, s.q(deleteResourceAccessSQL), kind); err != nil {
			return storeFailure("clear a resource kind's projection", err)
		}

		// The id space is the closed set the whole recompile walks, and never
		// the directory. A grant naming a kind outside it compiles to no row
		// there, so it compiles to no row here.
		if !slices.Contains(compilable, kind) {
			continue
		}

		if err = s.insertResourceAccess(ctx, tx, g.resourceRowsFor(kind), held, targets.epoch); err != nil {
			return err
		}
	}

	return nil
}

// writeScopedDevice replaces one device's rows in both device projections: the
// action masks and the dangerous verbs. Both are cleared and rewritten, so a
// principal who lost their access here loses the row rather than keeping a
// stale one.
func (s *Store) writeScopedDevice(
	ctx context.Context, tx *sql.Tx, g *graph, targets scopedTargets, id string,
) error {
	// Read before the delete, for the reason reproject reads before the
	// truncate: the generation a surviving sight began at is in the row that is
	// about to be replaced. The verb projection carries no such generation,
	// because a verb is not a sight and nothing subscribes to one.
	held, err := s.readSince(ctx, tx, selectDeviceSinceOnSQL, int64(ActionObserve.mask()), id)
	if err != nil {
		return err
	}

	if _, err = tx.ExecContext(ctx, s.q(deleteDeviceAccessSQL), id); err != nil {
		return storeFailure("clear a device's projection", err)
	}

	if _, err = tx.ExecContext(ctx, s.q(deleteDeviceVerbOnSQL), id); err != nil {
		return storeFailure("clear a device's verb projection", err)
	}

	// A target the directory no longer holds projects no row at all, which is
	// what a removal leaves behind: the delete above is the whole of it.
	row, registered := targets.directory[id]
	if !registered {
		return nil
	}

	if err = s.insertAccess(ctx, tx, g.rowsFor(row), held, targets.epoch); err != nil {
		return err
	}

	return s.insertVerbs(ctx, tx, g.verbRowsFor(row))
}

// placeholders renders n parameter markers for an IN list. Only markers are
// concatenated; no value ever reaches the query text.
func placeholders(n int) string {
	var b strings.Builder

	b.Grow(n * len("?, "))

	for i := range n {
		if i > 0 {
			b.WriteString(", ")
		}

		b.WriteString("?")
	}

	return b.String()
}
