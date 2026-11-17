package authz

import (
	"context"
	"database/sql"
	"errors"
	"slices"

	"github.com/prusa3d/prusa-connect/sdk/fault"
)

const (
	selectGrantsSQL = selectTuplesSQL + `
		WHERE object_type = ? AND object_id = ?
		ORDER BY relation, subject_type, subject_id, subject_relation`

	// The caller's standing is a join on the projection, so a device the caller
	// does not administer never leaves the database and no count can give it
	// away.
	selectSubjectGrantsSQL = `SELECT t.object_type, t.object_id, t.relation,
			t.subject_type, t.subject_id, t.subject_relation
		FROM authz_tuple t
		JOIN device_access a ON a.device_id = t.object_id
			AND a.subject_type = ? AND a.subject_id = ? AND (a.actions & ?) <> 0
		WHERE t.object_type = 'device'
			AND t.subject_type = ? AND t.subject_id = ? AND t.subject_relation = ?
		ORDER BY t.object_id, t.relation`

	// What one principal holds on one object, as the mask the projection
	// carries. The point check tests a bit; administering grants needs the
	// whole row, because the answer decides both whether the caller may write
	// here and what they are allowed to confer.
	heldDeviceActionsSQL = `SELECT actions FROM device_access
		WHERE subject_type = ? AND subject_id = ? AND device_id = ?`

	heldResourceActionsSQL = `SELECT actions FROM resource_access
		WHERE subject_type = ? AND subject_id = ? AND kind = ?`
)

// AdministerAction is the action a caller must hold on an object before they
// may read or change the grants written on it. There is no permission to
// administer permissions of its own: the vocabulary already names the level
// that owns an object, and it is that object type's manager action.
//
// It reports false for a type no action is defined on. org, group and
// device_group carry structure and grants but no action, so nothing can
// authorize a write on one and this surface refuses rather than inventing a
// check no projection can answer.
func AdministerAction(t Type) (Action, bool) {
	switch t {
	case TypeDevice:
		return ActionAdminister, true
	case TypeResourceKind:
		return ActionConfigureResource, true
	case TypeUser, TypeGroup, TypeServiceAccount, TypeDeviceCredential, TypeNode, TypeSink, TypeHistory,
		TypeOrg, TypeDeviceGroup:
		return "", false
	default:
		return "", false
	}
}

// Administrable reports whether this surface can decide who administers the
// grants written on an object of this type.
//
// It is true for the types AdministerAction names, and for an org. A device
// and a resource kind carry actions: the projection stores them, and
// administering one of those objects is holding its manager action. An org
// carries no action, because nobody performs a verb on an org. Administering
// an org is holding the admin role written on it. Neither case adds a separate
// permission to administer permissions.
//
// group and device_group stay out. A group has no owning level: member is its
// only relation. A device group has the whole role layer, but nothing in the
// product writes a device into one, so a device-group grant covers no device.
func Administrable(t Type) bool {
	if _, ok := AdministerAction(t); ok {
		return true
	}

	return t == TypeOrg
}

// Grants returns the tuples written on one object, for a caller who may
// administer it. It is the read half of the grant surface: the raw edges, in a
// stable order, rebuilt through the constructor like every other stored row.
//
// The permission is one point check on the object rather than a predicate on
// each row, because a grant is not addressed independently of the object it is
// written on: a caller who may administer the object may see every grant on it,
// and one who may not sees nothing and is told the same thing they would be
// told about an object that is not there.
func (s *Store) Grants(ctx context.Context, who Subject, object Object) ([]Tuple, error) {
	if err := s.mayAdministerObject(ctx, s.db, who, object); err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, s.q(selectGrantsSQL), object.typ.String(), object.id)
	if err != nil {
		return nil, storeFailure("read grants", err)
	}

	defer func() { _ = rows.Close() }() // the scan error below is the one worth reporting

	var out []Tuple

	for rows.Next() {
		var objectType, objectID, relation, subjectType, subjectID, subjectRelation string
		if err = rows.Scan(
			&objectType, &objectID, &relation, &subjectType, &subjectID, &subjectRelation,
		); err != nil {
			return nil, storeFailure("scan grant", err)
		}

		grant, buildErr := storedTuple(objectType, objectID, relation, subjectType, subjectID, subjectRelation)
		if buildErr != nil {
			return nil, buildErr
		}

		out = append(out, grant)
	}

	if err = rows.Err(); err != nil {
		return nil, storeFailure("read grants", err)
	}

	return out, nil
}

// SubjectGrants returns the tuples written on devices for one subject, narrowed
// to the devices the caller administers. It is the per-subject read of what
// Grants reads per object: raw edges, a block included, rather than the
// effective access SubjectDevices answers. A caller who administers nothing
// gets an empty list, the same answer as a subject who holds nothing.
func (s *Store) SubjectGrants(ctx context.Context, who, subject Subject) ([]Tuple, error) {
	if err := requireAdministering(ctx, Object{}); err != nil {
		return nil, err
	}

	mask, err := checkable(ctx, who, ActionAdminister)
	if err != nil {
		return nil, err
	}

	if subject.IsZero() {
		return nil, unknownType.New("a subject listing was asked for about an unconstructed subject")
	}

	return queryTuples(ctx, s.db, s.dialect, selectSubjectGrantsSQL,
		who.typ.String(), who.id, int64(mask),
		subject.typ.String(), subject.id, subject.rel.String(),
	)
}

// WriteGrant stores one tuple on behalf of a caller, and reports whether the
// graph did not already hold it. Three rules decide it, all read inside the
// transaction that writes:
//
//   - the caller holds the object type's manager action on the object, which is
//     what administering its grants is
//   - the grant confers no action the caller does not already hold there, so a
//     write cannot hand out more than the writer has
//   - the grant takes nothing the caller has no standing to take, which is
//     bound: neither from a principal who inherits more than they do, nor from
//     themselves when no other grant confers their access here
//
// The second is not implied by the first. A device's manager may not write its
// agent relation, because report_telemetry and fetch_job are in no role. A
// device credential is issued by whatever registers the device, and a grant
// API that could mint one would let a person's session forge that device's
// telemetry.
//
// The third is not implied by either. blocked confers nothing and removes the
// whole role layer, so the second rule passes every block ever written.
func (s *Store) WriteGrant(ctx context.Context, who Subject, grant Tuple) (bool, error) {
	if grant.IsZero() {
		return false, unknownType.New("write of an unconstructed tuple")
	}

	if err := requireAdministering(ctx, grant.object); err != nil {
		return false, err
	}

	var wrote bool

	err := s.mutate(ctx, func(ctx context.Context, tx *sql.Tx, moved *scope) (int64, error) {
		if err := s.authorize(ctx, tx, who, grant, grantWritten); err != nil {
			return 0, err
		}

		moved.moved(grant.object)

		result, err := tx.ExecContext(ctx, s.q(insertTupleSQL), tupleArgs(grant)...)
		if err != nil {
			return 0, storeFailure("insert grant", err)
		}

		written, err := result.RowsAffected()
		if err != nil {
			return 0, storeFailure("count inserted grants", err)
		}

		wrote = written > 0

		return written, nil
	})
	if err != nil {
		return false, err
	}

	return wrote, nil
}

// WithdrawGrant removes one tuple on behalf of a caller. It takes the same
// permission a write takes, and bound applies to it as it does to a write.
// Removing a tuple confers nothing. It can take access from a principal the
// caller does not outrank, and it can leave the object with nobody able to
// administer it. bound refuses both.
//
// Withdrawing a tuple the graph does not hold is not an error, so a client that
// retries a withdrawal it already made is told the same thing twice.
func (s *Store) WithdrawGrant(ctx context.Context, who Subject, grant Tuple) error {
	if grant.IsZero() {
		return unknownType.New("delete of an unconstructed tuple")
	}

	if err := requireAdministering(ctx, grant.object); err != nil {
		return err
	}

	return s.mutate(ctx, func(ctx context.Context, tx *sql.Tx, moved *scope) (int64, error) {
		if err := s.authorize(ctx, tx, who, grant, grantWithdrawn); err != nil {
			return 0, err
		}

		moved.moved(grant.object)

		result, err := tx.ExecContext(ctx, s.q(deleteTupleSQL), tupleArgs(grant)...)
		if err != nil {
			return 0, storeFailure("delete grant", err)
		}

		removed, err := result.RowsAffected()
		if err != nil {
			return 0, storeFailure("count deleted grants", err)
		}

		return removed, nil
	})
}

// authorize decides one mutation of the grant surface, whichever object type it
// names. It is everything WriteGrant and WithdrawGrant check, kept in one
// function so the two calls apply one policy.
//
// It has two arms, because the model has two shapes of object. An object
// carrying actions is decided against the projection it already has. An org
// carries none and is decided against the compiled graph. Each arm bounds both
// what the mutation confers and what it takes.
func (s *Store) authorize(
	ctx context.Context, tx *sql.Tx, who Subject, grant Tuple, kind mutation,
) error {
	if grant.object.typ == TypeOrg {
		return s.authorizeOnOrg(ctx, tx, who, grant, kind)
	}

	held, err := s.mayAdminister(ctx, tx, who, grant.object)
	if err != nil {
		return err
	}

	// Only a write adds. A withdrawal confers nothing, so only bound, which
	// reads what a mutation takes, applies to it.
	if kind == grantWritten {
		if s.orgAdminHandover && grant.relation == RelOrg {
			return orgEdgeNotWritable(grant.object)
		}

		if err = s.contains(ctx, tx, who, grant, held); err != nil {
			return err
		}
	}

	return s.bound(ctx, tx, who, grant, kind)
}

// contains refuses a write that hands out more than the caller holds. A grant
// can hand out actions, and it can hand out dangerous verbs.
//
// The actions are in the mask. The dangerous verbs are not: ownership hands
// out every dangerous verb of the device's kind, so a caller who holds the
// whole mask must also stand over the dangerous tier. See dangerous.go.
func (s *Store) contains(
	ctx context.Context, tx *sql.Tx, who Subject, grant Tuple, held actionSet,
) error {
	if missing, ok := escalates(grant, held); ok {
		return accessDenied.New(conferralRefusal).
			With(paramObject(grant.object), paramAction(missing))
	}

	if !impliesDangerousVerbs(grant.object.typ, grant.relation) {
		return nil
	}

	stands, reachErr := s.standsOverDangerousTier(ctx, tx, who, grant.object)
	if reachErr != nil {
		return reachErr
	}

	if !stands {
		return accessDenied.New(reachRefusal).
			With(paramObject(grant.object), paramRelation(grant.relation))
	}

	return nil
}

// authorizeOnOrg decides a mutation of the grants written on an org. Four
// rules, in the order a caller meets them:
//
//   - the caller holds admin on the org, which is what administering one is
//   - the mutation is not a write of admin, which is the one org grant this
//     surface refuses while the handover key is closed
//   - the caller still holds admin on the org afterwards, which is mayStandDown
//     read through the role rather than through a projected action
//   - on every object the org reaches, the mutation confers nothing the caller
//     does not already hold there and takes nothing from a principal they do
//     not outrank there
//
// The fourth is the one an org needs and a device does not. escalates bounds a
// device grant by what the caller holds on the device it is written on, which
// is the object the grant lands on. An org tuple is written on the org and
// lands on every device and every resource kind the org reaches, so the
// question has an answer per object and the widest of those is the one that
// counts. See mayConfer.
//
// It compares the compiler's answer on each reached object and does not
// restate what an org role means, for the reason bound gives: a rule that
// bounds a grant by restating what a grant confers is a second definition to
// keep in step. The cost is two compiles of every object the graph names,
// where bound compiles one object for a device grant. On the 400-printer
// fixture and the pure-Go SQLite driver, one write and one withdrawal through
// this surface is 141 ms against 5 ms for the same pair on a device
// (BenchmarkOrgGrantWriteAndWithdraw against
// BenchmarkGrantSurfaceWriteAndWithdraw). Writing a fleet-wide tuple is an
// administrator's act and not a request path, and it moves every device in the
// org, so the mutation takes the whole recompile either way.
func (s *Store) authorizeOnOrg(
	ctx context.Context, tx *sql.Tx, who Subject, grant Tuple, kind mutation,
) error {
	if err := asks(who); err != nil {
		return err
	}

	tuples, err := loadTuples(ctx, tx, s.dialect)
	if err != nil {
		return err
	}

	before := indexTuples(tuples)
	if !administersOrg(before, who, grant.object) {
		return deniedOnOrg(grant.object)
	}

	if kind == grantWritten && grant.relation == RelAdmin && !s.orgAdminHandover {
		return adminNotConferrable(grant.object)
	}

	after := indexTuples(applied(tuples, grant, kind))
	if !administersOrg(after, who, grant.object) {
		return standDown(grant.object)
	}

	for _, object := range objectsCarryingActions(tuples) {
		held, gained := before.accessOn(object), after.accessOn(object)

		if err = mayConfer(who, object, held, gained); err != nil {
			return err
		}

		if err = mayRemove(who, object, held, gained); err != nil {
			return err
		}
	}

	return nil
}

// mayAdministerObject is the read half's permission, for either shape of
// object. It answers the same question authorize answers first, and it is the
// whole of what a listing takes: administering an object is seeing every grant
// written on it.
func (s *Store) mayAdministerObject(
	ctx context.Context, q queryer, who Subject, object Object,
) error {
	if err := requireAdministering(ctx, object); err != nil {
		return err
	}

	if object.typ == TypeOrg {
		if err := asks(who); err != nil {
			return err
		}

		tuples, err := loadTuples(ctx, q, s.dialect)
		if err != nil {
			return err
		}

		if !administersOrg(indexTuples(tuples), who, object) {
			return deniedOnOrg(object)
		}

		return nil
	}

	_, err := s.mayAdminister(ctx, q, who, object)

	return err
}

// administersOrg reports whether this principal holds the admin role on the
// org, directly or through a group they are a member of.
//
// It reads the compiled graph rather than a projection, because there is no org
// projection to read: a projection stores actions, an org has none, and adding
// a table to store one role would be a third projection and a migration on two
// engines to hold what one map lookup already answers. The lookup is the same
// one deviceAccess makes for an org role, so a membership change reaches this
// the moment it reaches everything else.
func administersOrg(g *graph, who Subject, org Object) bool {
	target := principal{typ: who.typ, id: who.id}
	held := false

	for _, subject := range g.roles[roleKey{Object: TypeOrg, ID: org.id, Relation: RelAdmin}] {
		g.expand(subject, func(reached principal) {
			if reached == target {
				held = true
			}
		})
	}

	return held
}

// objectsCarryingActions is every object of the tuple set that the projection
// compiles: the devices and the resource kinds. It is where an org tuple lands,
// and it is derived from the tuples rather than from the directory so that a
// device the server does not know about yet is bounded the same way as one it
// does. A grant on such a device compiles to no row, but it compiles to
// standing the moment the device is registered, and the rule that bounds an org
// tuple has to have read it before then.
//
// The scan is of the object side alone. A device named only as the subject of a
// parent edge holds no tuples of its own, so nothing the org confers can reach
// it: the edge carries what its parent has, and its parent is on this list.
//
// Sorted, because a refusal names one object and one action, and two engines
// answering the same call must return the same bytes.
func objectsCarryingActions(tuples []Tuple) []Object {
	seen := make(map[Object]bool, len(tuples))
	out := make([]Object, 0, len(tuples))

	for _, tuple := range tuples {
		if tuple.object.typ != TypeDevice && tuple.object.typ != TypeResourceKind {
			continue
		}

		if seen[tuple.object] {
			continue
		}

		seen[tuple.object] = true

		out = append(out, tuple.object)
	}

	slices.SortFunc(out, func(a, b Object) int {
		return comparePrincipals(a.principal(), b.principal())
	})

	return out
}

// mayConfer refuses a mutation that hands any principal an action on this
// object that the caller does not hold on it.
//
// It is escalates read over the compiler instead of over the vocabulary table,
// which is what an object reached indirectly needs. conferred() answers what a
// relation gives on the object the tuple is written on; nothing in it can
// answer what an org role gives on one device of that org, because that
// depends on what else is written there.
//
// The state it refuses comes from a block. An org admin holds the manager mask
// everywhere the org reaches, and every org role confers inside it, so a
// fleet-wide grant is otherwise contained. A block deletes the administrator
// from one device's role layer, and lifting it takes the administer action the
// block took, so the administrator cannot undo it. Without this rule they
// could still write an org tuple handing somebody else standing on that
// device, which is the escalation the block was written to prevent.
//
// The measure is what the caller held before the mutation, so a write cannot be
// its own authority.
func mayConfer(who Subject, object Object, before, after map[principal]access) error {
	held := before[principal{typ: who.typ, id: who.id}].held

	beneficiaries := make([]principal, 0, len(after))
	for beneficiary := range after {
		beneficiaries = append(beneficiaries, beneficiary)
	}

	slices.SortFunc(beneficiaries, comparePrincipals)

	for _, beneficiary := range beneficiaries {
		gained := after[beneficiary].held &^ before[beneficiary].held
		if beyond := gained &^ held; beyond != 0 {
			return accessDenied.New(conferralRefusal).
				With(paramObject(object), paramAction(firstAction(beyond)))
		}
	}

	return nil
}

// mayAdminister refuses a caller who may not administer this object, and
// returns what they do hold on it for the containment rules to read.
//
// The action it tests is conferred by the manager level and by nothing below
// it, and every level contains the ones below, so a caller who passes holds the
// whole manager mask of that object type. Both containment rules read the
// returned set as though that is true. See conferred.
//
// A write calls it inside the writing transaction. Reading the projection from
// the pool instead would leave a window in which a grant withdrawn between the
// check and the write is still enough to write with.
func (s *Store) mayAdminister(
	ctx context.Context, q queryer, who Subject, object Object,
) (actionSet, error) {
	if err := requireAdministering(ctx, object); err != nil {
		return 0, err
	}

	action, ok := AdministerAction(object.typ)
	if !ok {
		return 0, notAdministrable(object)
	}

	if _, err := checkable(ctx, who, action); err != nil {
		return 0, err
	}

	held, err := s.heldActions(ctx, q, who, object)
	if err != nil {
		return 0, err
	}

	if held&action.mask() == 0 {
		return 0, denied(action, object)
	}

	return held, nil
}

// heldActions reads what one principal holds on one object, as the mask the
// projection carries. An object with no row for this principal holds nothing,
// which is the same answer as an object the directory does not have. So a
// refusal for an object that is not there is identical to a refusal for one
// the caller may not reach.
func (s *Store) heldActions(
	ctx context.Context, q queryer, who Subject, object Object,
) (actionSet, error) {
	query := heldDeviceActionsSQL
	if object.typ == TypeResourceKind {
		query = heldResourceActionsSQL
	}

	// Scanned into the width the mask is, so a row outside the action space,
	// which only a hand-edited or corrupted projection can hold, is refused by
	// the driver's range check rather than truncated into a set of actions
	// nobody granted.
	var held uint32

	err := q.QueryRowContext(
		ctx, s.q(query), who.typ.String(), who.id, object.id,
	).Scan(&held)

	switch {
	case errors.Is(err, sql.ErrNoRows):
		return 0, nil
	case err != nil:
		return 0, storeFailure("read held actions", err)
	default:
		return actionSet(held), nil
	}
}

// escalates reports whether this grant confers something the caller does not
// hold, and names the first action it would hand out that they do not have.
func escalates(grant Tuple, held actionSet) (Action, bool) {
	missing := conferred(grant.object.typ, grant.relation) &^ held
	if missing == 0 {
		return "", false
	}

	return firstAction(missing), true
}

// mutation says which way a grant surface call moves the tuple set.
type mutation uint8

const (
	grantWritten mutation = iota
	grantWithdrawn
)

// bound refuses a mutation that takes access the caller has no standing to
// take. escalates bounds what a grant adds; this bounds what one takes away,
// and the two are not the same rule: blocked confers nothing and removes the
// whole role layer, and a withdrawal confers nothing at all, so every
// subtraction passes the addition rule.
//
// Two rules read one compile of the object. mayRemove orders the caller against
// every other principal the mutation takes from; mayStandDown protects the
// caller from themselves.
//
// Both run on a write as well as on a withdrawal, because blocked is a write
// that subtracts. A rule that ran only on withdrawals would miss it.
//
// They compare the compiler's answer and not the projection's rows, because a
// rule that bounds a grant by reimplementing what a grant means is a second
// definition to keep in step. That costs one read of the whole tuple table and
// two compiles of one object, on top of the recompile every mutation pays, and
// it is the larger share of a scoped mutation. On the 400-printer fixture and
// the pure-Go SQLite driver, one mutation through this surface is 2.6 ms
// against 0.5 ms for the same mutation with no caller
// (BenchmarkGrantSurfaceWriteAndWithdraw against BenchmarkWriteAndRevokeGrant,
// each of which is two mutations).
func (s *Store) bound(
	ctx context.Context, tx *sql.Tx, who Subject, grant Tuple, kind mutation,
) error {
	tuples, err := loadTuples(ctx, tx, s.dialect)
	if err != nil {
		return err
	}

	before := indexTuples(tuples).accessOn(grant.object)
	after := indexTuples(applied(tuples, grant, kind)).accessOn(grant.object)

	if err = mayRemove(who, grant.object, before, after); err != nil {
		return err
	}

	return mayStandDown(who, grant.object, after)
}

// mayRemove refuses a mutation that takes access from a principal the caller
// does not outrank.
//
// The bound is what the caller did not write here. A grant written on the
// object is the caller's to manage, because administering the object is exactly
// what they were checked for; access flowing in from somewhere else is not,
// unless they hold as much of it themselves. So an org admin may block an org
// viewer from one printer, and a manager delegated one printer may not block
// the org admin who delegated it.
//
// The comparison is per source and not over one mask. device#parent is written
// on the object, so a manager delegated two devices may name one as the other's
// parent: conferred(device, parent) asks only for administer on the child, and the
// edge then confers the parent's mask on the child to every principal, the
// caller included. Against a single mask that write turns the caller's own
// written-here manager into inherited standing, and the next call blocks the
// org admin off a device of their own org, which nobody can undo because
// lifting a block takes the action the block removed.
//
// The two sources are ordered, and tenancy outranks the edge. A caller
// standing on what an org or a group confers may take what a parent edge
// confers, because tenancy is written above the object by whoever administers
// the tree. A caller standing only on what a parent confers may not take
// tenancy, because that edge is written on the object by whoever administers
// the object, which is the caller. Symmetric arms would close the escalation
// too, but they would break a case that must work: an administrator holding
// administer on a camera through the org could not hide it from a viewer of
// its printer, whose standing arrives down the edge.
//
// Held apart the other way round, a device reached only through a parent edge
// gives every principal the same empty inherited mask. That is every camera
// bound to a printer, because such a camera carries no org tuple, and one arm
// of the comparison would decide nothing. So the projection carries the
// parent's own split and not the parent's total. See access.
//
// It is reflexive, so it refuses nobody on their own account. What a caller may
// do to themselves is mayStandDown.
func mayRemove(who Subject, object Object, before, after map[principal]access) error {
	// Sorted, because the refusal names one action and two engines answering
	// the same call must return the same bytes.
	victims := make([]principal, 0, len(before))
	for victim := range before {
		victims = append(victims, victim)
	}

	slices.SortFunc(victims, comparePrincipals)

	// A caller is a principal here: mayAdminister has already refused a userset.
	protected := before[principal{typ: who.typ, id: who.id}]

	for _, victim := range victims {
		held := before[victim]
		if held.held&^after[victim].held == 0 {
			continue
		}

		for _, source := range [][2]actionSet{
			{held.inherited, protected.inherited},
			{held.fromParent, protected.inherited | protected.fromParent},
		} {
			if beyond := source[0] &^ source[1]; beyond != 0 {
				return accessDenied.
					New("a grant may not take an action from a principal the caller does not outrank").
					With(paramObject(object), paramAction(firstAction(beyond)))
			}
		}
	}

	return nil
}

// mayStandDown refuses a mutation that would leave the caller unable to
// administer the object, when no other grant confers that ability.
//
// It is one rule over three shapes, because it reads what the mutation
// produces and not which relation it names: withdrawing the grant the caller
// administers with, writing blocked on themselves, and severing the org their
// access arrives through all end in the same place. Nothing writes the first
// grant back. There is no API that writes a tuple without an existing grant on
// the object, RemoveDevice has no caller, and Bootstrap refuses to re-run, so
// the object keeps whatever grants it has and nobody can change them again.
//
// The measure is the whole standing and not the tuple: a caller who also
// reaches the object through an org keeps the action after their direct grant
// goes, so withdrawing a redundant grant is allowed. A block needs no special
// case, because it deletes the principal from the role layer whatever wrote
// it.
//
// A handover runs in the order that keeps this true at every step: grant the
// successor, then have the successor withdraw the departing grant.
func mayStandDown(who Subject, object Object, after map[principal]access) error {
	action, ok := AdministerAction(object.typ)
	if !ok {
		return notAdministrable(object)
	}

	if after[principal{typ: who.typ, id: who.id}].held&action.mask() != 0 {
		return nil
	}

	return standDown(object).With(paramAction(action))
}

// The two sentences this surface refuses with more than once, held as constants
// so a caller meeting one rule through two object types reads one wording. A
// difference between them would read as a difference between the rules.
const (
	conferralRefusal = "a grant may not confer an action the caller does not hold"

	standDownRefusal = "a grant may not remove the caller's own ability to administer the object " +
		"when no other grant confers it: grant another administrator first, then have them " +
		"withdraw this grant"
)

// standDown is the lockout refusal. It carries the object and nothing else,
// because an org has no action to name; mayStandDown adds the action for the
// object types that have one.
func standDown(object Object) *fault.Fault {
	return accessDenied.New(standDownRefusal).With(paramObject(object))
}

// deniedOnOrg refuses a caller who does not administer this org.
//
// An org has no directory, so an org nobody has written a tuple on has no admin
// either and reaches this by the same path: a refusal for an org that is not
// there and one for an org the caller may not touch are one answer, which is
// the property TestGrantRefusalsHideWhetherTheObjectExists holds for a device
// and TestOnlyAnOrgAdminAdministersAnOrg holds here.
func deniedOnOrg(object Object) error {
	return accessDenied.New("the subject holds no admin role on this org").With(paramObject(object))
}

// adminNotConferrable refuses a write of an org's admin role.
//
// It is the only org grant this surface refuses, and it is refused for what it
// does outside the org rather than inside it. Inside, admin is the manager
// level: it confers exactly what the caller writing it already holds everywhere
// the org reaches, so mayConfer would pass it. Outside, it raises what its
// holder may take. mayRemove bounds a caller by what they inherit on an object,
// whoever manages a device may write that device's org edge, and an org admin
// who attaches their own org to a device of another org then inherits the
// manager mask there and outranks that org's administrator. That is
// unreachable while nothing but Bootstrap can put a caller into an org's admin
// role, and this refusal keeps it unreachable.
//
// The refusal also blocks the handover that the lockout refusal recommends.
// WithOrgAdminHandover makes that configurable. With it open, this refusal
// does not run and orgEdgeNotWritable closes the escalation instead.
//
// Withdrawal is not refused. It takes standing rather than conferring it, and
// mayRemove already bounds what a withdrawal may take.
func adminNotConferrable(object Object) error {
	return accessDenied.New("the admin role on an org is not conferrable through this surface: it is " +
		"the only grant that raises what its holder may take from an administrator of another org").
		With(paramObject(object))
}

// orgEdgeNotWritable refuses a write of an object's org edge, which is the
// refusal a deployment takes in exchange for the handover.
//
// The edge is tenancy and not a grant: it says which org an object belongs to,
// and whoever manages the object may write it. That is harmless while no caller
// can be put into a second org's admin role through the surface. With the
// handover open, a caller who admins one org and manages one object of another
// can attach their own org to that object, inherit the manager mask there, and
// outrank the org administrator the object already had.
//
// Refusing the write closes it without narrowing the handover, because nothing
// legitimate writes this edge through the grant surface. RegisterDevice writes
// a device's tenancy in the transaction that registers it, and Bootstrap writes
// a resource kind's.
//
// Withdrawal is not refused. Removing an object from an org takes tenancy away
// rather than claiming it, and mayRemove bounds what that may take.
func orgEdgeNotWritable(object Object) error {
	return accessDenied.New("the org edge on this object is not writable through this surface while the " +
		"org handover is open: attaching an org confers that org's roles here").
		With(paramObject(object))
}

// applied returns the tuple set this mutation produces, so the rule compares
// the graph the caller asked for against the one that is there. A write of a
// tuple the graph already holds appears twice and compiles to the same answer,
// because indexing a tuple set is a union.
func applied(tuples []Tuple, grant Tuple, kind mutation) []Tuple {
	if kind == grantWritten {
		return append(slices.Clone(tuples), grant)
	}

	out := make([]Tuple, 0, len(tuples))

	for _, tuple := range tuples {
		if tuple != grant {
			out = append(out, tuple)
		}
	}

	return out
}

// firstAction names one action of a set, so a refusal says which action rather
// than which bits. Every bit of an actionSet belongs to an action, so the empty
// answer is unreachable, and a refusal that cannot name one is still a refusal.
func firstAction(set actionSet) Action {
	for _, action := range Actions() {
		if set&action.mask() != 0 {
			return action
		}
	}

	return ""
}

// conferred is what a grant of this relation gives its subject on this object.
//
// A role confers its level. A block confers nothing, because it removes the
// role layer rather than adding to it. Ownership confers ownerMask, which is
// inside the manager mask, so the caller who administers a device is by
// construction able to say who owns it and an owner is not. org, group and
// parent confer whatever the object they name holds, which the role layer
// bounds at manager: the subject of one of those is another object, and what
// flows through it is the grants written there.
//
// The escalation rule depends on this: every relation but agent confers inside
// the manager mask of the object type, and mayAdminister admits only a caller
// holding that whole mask. So a role or a structure grant is inside what the
// writer holds, and agent is the one case the check refuses.
// TestEveryRelationConfersWhatItsSubjectReaches holds both halves. If either
// is loosened, a session could mint a device credential.
//
// The object type is decided first and the relation second, so this function is
// its own guard. agent and owner are device relations and their masks are
// device masks; answering one of them for another object type would hand out a
// device action on an object no device action is defined on. relationSpecs
// declares neither relation anywhere but on a device, so nothing writes such a
// tuple. This function does not depend on that, because one row added to the
// vocabulary table must not be enough to hand out a device action.
func conferred(objectType Type, relation Relation) actionSet {
	if relation == RelBlocked {
		return 0
	}

	switch objectType {
	case TypeDevice:
		return conferredOnDevice(relation)
	case TypeResourceKind:
		return conferredOnKind(relation)
	case TypeUser, TypeGroup, TypeServiceAccount, TypeDeviceCredential, TypeNode, TypeSink, TypeHistory,
		TypeOrg, TypeDeviceGroup:
		return 0
	default:
		return 0
	}
}

// conferredOnDevice is conferred for a device, which is the only object type
// agent and owner are defined on.
func conferredOnDevice(relation Relation) actionSet {
	if relation == RelAgent {
		return agentMask
	}

	if relation == RelOwner {
		return ownerMask
	}

	return roleOrStructure(relation).deviceMask()
}

// conferredOnKind is conferred for a resource kind, which carries the role
// layer and org tenancy and nothing else.
func conferredOnKind(relation Relation) actionSet {
	if relation == RelAgent || relation == RelOwner {
		return 0
	}

	return roleOrStructure(relation).resourceMask()
}

// roleOrStructure is the level a relation carries: its own if it is a role, and
// manager otherwise, because org, group and parent confer whatever the object
// they name holds and the role layer bounds that at manager.
func roleOrStructure(relation Relation) level {
	if role, isRole := roleLevel(relation); isRole {
		return role
	}

	return levelManager
}

// notAdministrable refuses an object type whose grants nothing can authorize.
func notAdministrable(object Object) error {
	return unsupportedObject.New("no action is defined on that object type, so its grants cannot be administered").
		With(paramObject(object))
}
