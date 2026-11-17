package authz

import (
	"context"
	"database/sql"
	"errors"
	"slices"

	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// The dangerous tier holds the verbs that no class, role or level confers. A
// subject reaches one only through a row that names the subject, the device
// and the verb.
//
// It is the only place a verb name appears in a grant. Everywhere else a grant
// names a class, and a class is an ordinary tuple. The tier exists because a
// level is too coarse for these verbs: keeping one at manager would hand it to
// every manager of every device, and a row names exactly who may do what.
//
// The verb stays on the row by name. An operator reads a name and never a
// number, and a revocation is a deleted row.

const (
	// The dangerous-tier rows, with the verb by name.
	selectVerbGrantsSQL = `SELECT object_type, object_id, verb,
		subject_type, subject_id, subject_relation
		FROM authz_declared_grant`

	selectVerbGrantsOnSQL = selectVerbGrantsSQL + `
		WHERE object_type = ? AND object_id = ?
		ORDER BY verb, subject_type, subject_id, subject_relation`

	insertVerbGrantSQL = `INSERT INTO authz_declared_grant
		(object_type, object_id, verb, subject_type, subject_id, subject_relation)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT DO NOTHING`

	deleteVerbGrantSQL = `DELETE FROM authz_declared_grant
		WHERE object_type = ? AND object_id = ? AND verb = ?
		  AND subject_type = ? AND subject_id = ? AND subject_relation = ?`

	// The projected tier: who reaches which verb on which device, compiled the
	// way device_access is compiled and read the way device_access is read.
	selectDeviceVerbSQL = `SELECT 1 FROM device_verb
		WHERE subject_type = ? AND subject_id = ? AND device_id = ? AND verb = ?`

	selectDeviceVerbsOfSQL = `SELECT device_id, verb FROM device_verb
		WHERE subject_type = ? AND subject_id = ?
		ORDER BY device_id, verb`
)

// directoryRow is one device the projection compiles over: the id and the kind.
//
// The verb table is keyed by kind, so a compile that knew an id and not its
// kind could not say which verbs exist on it.
type directoryRow struct {
	id   string
	kind string
}

// VerbGrant is one row of the dangerous tier: the device, the verb by name and
// the subject.
//
// The fields are unexported and validated at construction, exactly as Tuple's
// are, so a grant on an object type that spans device kinds cannot be written by
// a struct literal.
type VerbGrant struct {
	object  Object
	verb    string
	subject Subject
}

// NewVerbGrant validates the whole row.
//
// The object is a device and nothing else. An org, a group and a device group
// span device kinds, and a verb belongs to exactly one kind, so one row on one
// of those three would name a verb that exists on some of the devices under it
// and on none of the others. Every other type carries no verb at all.
//
// The subject is one of the shapes a role may name, so a dangerous verb and a
// role are held by the same kinds of principal. A userset is accepted for the
// reason the role layer accepts one: group#member is the recursion the model
// rests on.
func NewVerbGrant(object Object, verb string, subject Subject) (VerbGrant, error) {
	if object.IsZero() || subject.IsZero() {
		return VerbGrant{}, unknownType.New("verb grant has an unconstructed object or subject")
	}

	if object.typ != TypeDevice {
		return VerbGrant{}, declaredGrantRefused.New(
			"a verb is granted on a device and on nothing else. An org, a group and a device group "+
				"span device kinds, and a verb belongs to one kind, so one row would name a verb the "+
				"devices under it do not all have").
			With(paramObject(object), paramType(object.typ))
	}

	if err := ValidateVerb(verb); err != nil {
		return VerbGrant{}, err
	}

	if !permits(grantable, subject) {
		return VerbGrant{}, invalidSubject.New(
			"a verb is granted to the same subject shapes a role is").
			With(paramSubject(subject), paramName(verb))
	}

	return VerbGrant{object: object, verb: verb, subject: subject}, nil
}

// Object returns the device the grant is written on.
func (g VerbGrant) Object() Object { return g.object }

// Verb returns the verb by name, which is what the row stores and what an
// operator reads.
func (g VerbGrant) Verb() string { return g.verb }

// Subject returns who the grant is written for.
func (g VerbGrant) Subject() Subject { return g.subject }

// IsZero reports whether g was never constructed.
func (g VerbGrant) IsZero() bool { return g == VerbGrant{} }

// String renders the grant as an operator reads it in a refusal or an audit
// line: object#verb@subject, the notation a tuple already uses.
func (g VerbGrant) String() string {
	return g.object.String() + relationSeparator + g.verb + "@" + g.subject.String()
}

// args renders the row in the column order every statement above names.
func (g VerbGrant) args() []any {
	return []any{
		g.object.typ.String(), g.object.id, g.verb,
		g.subject.typ.String(), g.subject.id, g.subject.rel.String(),
	}
}

// key renders the primary key, which is every column: two grants of one verb to
// one subject on one device are one row.
func (g VerbGrant) key() []any { return g.args() }

// WriteVerbGrant writes one dangerous-tier row on behalf of a caller.
//
// The caller must administer the device and must already reach the verb on it.
// Nobody may confer a verb they do not hold, and no level stands in for one.
//
// Owning a device implies every dangerous verb of its kind. That is computed
// and not stored, so the owner of a machine is who hands its dangerous verbs
// out. A device with no owner fact has nobody who can, and the tier stays
// closed for it.
func (s *Store) WriteVerbGrant(ctx context.Context, who Subject, grant VerbGrant) (bool, error) {
	if grant.IsZero() {
		return false, unknownType.New("write of an unconstructed verb grant")
	}

	if err := requireAdministering(ctx, grant.object); err != nil {
		return false, err
	}

	var wrote bool

	err := s.mutate(ctx, func(ctx context.Context, tx *sql.Tx, moved *scope) (int64, error) {
		stands, err := s.verbGrantStanding(ctx, tx, who, grant.object)
		if err != nil {
			return 0, err
		}

		kind, err := s.kindOfDevice(ctx, tx, grant.object.id)
		if err != nil {
			return 0, err
		}

		if err = s.grantableVerb(kind, grant); err != nil {
			return 0, err
		}

		reaches, err := s.reachesVerb(ctx, tx, who, grant.object.id, grant.verb)
		if err != nil {
			return 0, err
		}

		// Standing over the tier counts as reaching the verb, which is how the
		// first row gets written: the owner of a machine reaches every dangerous
		// verb of its kind, and so does an admin of an org the machine is in. A
		// caller who only administers the device has to hold the verb itself.
		if !reaches && !stands {
			return 0, accessDenied.New(dangerousTierRefusal).
				With(paramObject(grant.object), paramName(grant.verb))
		}

		moved.moved(grant.object)

		result, err := tx.ExecContext(ctx, s.q(insertVerbGrantSQL), grant.args()...)
		if err != nil {
			return 0, storeFailure("insert verb grant", err)
		}

		written, err := result.RowsAffected()
		if err != nil {
			return 0, storeFailure("count inserted verb grants", err)
		}

		wrote = written > 0

		return written, nil
	})
	if err != nil {
		return false, err
	}

	return wrote, nil
}

// grantableVerb refuses a row for a verb this device's kind does not carry, and
// for a verb of that kind nothing marked dangerous.
//
// A row for a verb the kind does not declare would confer nothing, so it is
// refused and not stored. CanVerb makes the same check at read time, for a
// verb the kind stopped declaring after the row was written.
//
// A verb that is not dangerous is refused too. Its class already confers it,
// and a row would be a second grant path for the same permission.
func (s *Store) grantableVerb(kind string, grant VerbGrant) error {
	if !Declared(DeviceKind(kind), grant.verb) {
		return declaredGrantRefused.New(
			"this device's kind declares no such verb, so the row would confer nothing").
			With(paramObject(grant.object), paramName(grant.verb),
				fault.StringParam(paramNameKind, kind))
	}

	if !Dangerous(DeviceKind(kind), grant.verb) {
		return declaredGrantRefused.New(
			"that verb is not in the dangerous tier, so its class already confers it. Grant the class "+
				"as an ordinary role instead: a second grant path for one verb is two revocation "+
				"stories for one permission").
			With(paramObject(grant.object), paramName(grant.verb),
				fault.StringParam(paramNameKind, kind))
	}

	return nil
}

// kindOfDevice reads the kind the directory registered an id under, refusing an
// id the directory does not hold. A grant on a device this server does not know
// about has no kind to read a verb under, so it is a refusal rather than a
// stored row that compiles to nothing.
func (s *Store) kindOfDevice(ctx context.Context, q queryer, id string) (string, error) {
	var kind string

	err := q.QueryRowContext(ctx, s.q(selectDeviceKindSQL), id).Scan(&kind)

	switch {
	case errors.Is(err, sql.ErrNoRows):
		return "", declaredGrantRefused.New(
			"this server has no device under that id, so there is no kind to read the verb under").
			With(paramID(id))
	case err != nil:
		return "", storeFailure("read the device kind", err)
	}

	if kind == "" {
		kind = DeviceKindPrinter.String()
	}

	return kind, nil
}

// DeviceKind returns the kind the directory registered an id under, and printer
// for an id the directory does not hold.
//
// A caller uses it to classify a verb, and a verb classified under the wrong
// kind is refused rather than answered, so the fallback is the safe direction:
// an unregistered id classifies against the printer's own table and is then
// refused by the reach check, which cannot see a device the directory has never
// heard of.
func (s *Store) DeviceKind(ctx context.Context, id string) (DeviceKind, error) {
	var kind string

	err := s.db.QueryRowContext(ctx, s.q(selectDeviceKindSQL), id).Scan(&kind)

	switch {
	case errors.Is(err, sql.ErrNoRows):
		return DeviceKindPrinter, nil
	case err != nil:
		return "", storeFailure("read the device kind", err)
	}

	return DeviceKind(kind).Or(DeviceKindPrinter), nil
}

// WithdrawVerbGrant removes one dangerous-tier row on behalf of a caller.
//
// A revocation is a deleted row, and the recompile this mutation takes produces
// a verb projection without it. Withdrawing a grant the graph does not hold is
// not an error, so a client that retries a withdrawal it already made is told
// the same thing twice.
//
// It takes the same standing a write takes, so whoever could hand a verb out
// can take it back. Without that an owner who bootstrapped the tier on their
// own machine would need somebody else to undo it.
func (s *Store) WithdrawVerbGrant(ctx context.Context, who Subject, grant VerbGrant) error {
	if grant.IsZero() {
		return unknownType.New("delete of an unconstructed verb grant")
	}

	if err := requireAdministering(ctx, grant.object); err != nil {
		return err
	}

	return s.mutate(ctx, func(ctx context.Context, tx *sql.Tx, moved *scope) (int64, error) {
		if _, err := s.verbGrantStanding(ctx, tx, who, grant.object); err != nil {
			return 0, err
		}

		moved.moved(grant.object)

		result, err := tx.ExecContext(ctx, s.q(deleteVerbGrantSQL), grant.key()...)
		if err != nil {
			return 0, storeFailure("delete verb grant", err)
		}

		removed, err := result.RowsAffected()
		if err != nil {
			return 0, storeFailure("count deleted verb grants", err)
		}

		return removed, nil
	})
}

// VerbGrants returns the dangerous-tier rows written on one object, for a caller
// who may administer it. It is the read half, and it takes the permission Grants
// takes for the reason Grants gives: a grant is not addressed independently of
// the object it is written on.
//
// It reads the written rows and never the projection, so a row whose verb the
// kind no longer declares is still listed. An operator deletes it or waits for
// the verb to come back, and either way they can see it.
func (s *Store) VerbGrants(ctx context.Context, who Subject, object Object) ([]VerbGrant, error) {
	if err := s.mayAdministerObject(ctx, s.db, who, object); err != nil {
		return nil, err
	}

	return s.verbGrants(ctx, s.db, selectVerbGrantsOnSQL, object.typ.String(), object.id)
}

// CanVerb reports whether a subject reaches one dangerous verb on one device.
//
// It fails closed twice. A verb this device's kind no longer declares is
// refused before the row is read, so a build that removed a verb leaves its
// rows without effect. And an absent projected row is a refusal.
//
// The caller reaches this only for a verb the class check already passed, so a
// verb outside the tier answers true here: the class carried it, and asking the
// tier for a verb no marking put there would refuse everything.
func (s *Store) CanVerb(ctx context.Context, who Subject, device Object, verb string) (bool, error) {
	if who.IsZero() || device.IsZero() {
		return false, unknownType.New("verb check with an unconstructed subject or object")
	}

	if device.typ != TypeDevice {
		return false, unsupportedObject.New("a verb is performed on a device").
			With(paramObject(device), paramName(verb))
	}

	kind, err := s.kindOfDevice(ctx, s.db, device.id)
	if err != nil {
		return false, err
	}

	if !Declared(DeviceKind(kind), verb) {
		return false, nil
	}

	if !Dangerous(DeviceKind(kind), verb) {
		return true, nil
	}

	// The dangerous tier is administering the machine, so a credential whose
	// scopes stop short of administer never reaches it.
	if !ceilingFrom(ctx).administers() {
		return false, nil
	}

	var held int

	err = s.db.QueryRowContext(ctx, s.q(selectDeviceVerbSQL),
		who.typ.String(), who.id, device.id, verb).Scan(&held)

	switch {
	case errors.Is(err, sql.ErrNoRows):
		return false, nil
	case err != nil:
		return false, storeFailure("read the verb projection", err)
	}

	return true, nil
}

// reachesVerb is CanVerb inside a transaction, without the classification
// checks the caller has already made.
func (s *Store) reachesVerb(ctx context.Context, q queryer, who Subject, device, verb string) (bool, error) {
	var held int

	err := q.QueryRowContext(ctx, s.q(selectDeviceVerbSQL),
		who.typ.String(), who.id, device, verb).Scan(&held)

	switch {
	case errors.Is(err, sql.ErrNoRows):
		return false, nil
	case err != nil:
		return false, storeFailure("read the verb projection", err)
	}

	return true, nil
}

// VerbsOfSubject is every dangerous verb one subject reaches, by device, read
// from the projection the point check reads. The grants-by-subject route
// answers with these and with the subject's classes.
func (s *Store) VerbsOfSubject(ctx context.Context, who Subject) (map[string][]string, error) {
	if who.IsZero() {
		return nil, unknownType.New("read of an unconstructed subject's verbs")
	}

	if !ceilingFrom(ctx).administers() {
		return map[string][]string{}, nil
	}

	rows, err := s.db.QueryContext(ctx, s.q(selectDeviceVerbsOfSQL), who.typ.String(), who.id)
	if err != nil {
		return nil, storeFailure("read a subject's verbs", err)
	}

	defer func() { _ = rows.Close() }() // the scan error below is the one worth reporting

	out := map[string][]string{}

	for rows.Next() {
		var device, verb string
		if err = rows.Scan(&device, &verb); err != nil {
			return nil, storeFailure("scan a subject's verb", err)
		}

		out[device] = append(out[device], verb)
	}

	if err = rows.Err(); err != nil {
		return nil, storeFailure("read a subject's verbs", err)
	}

	return out, nil
}

// verbGrants reads rows and rebuilds every one through the constructor, exactly
// as a stored tuple is rebuilt. A row arriving from a restored backup or a hand
// edit is refused rather than compiled as whatever it resembles.
func (s *Store) verbGrants(
	ctx context.Context, q queryer, query string, args ...any,
) ([]VerbGrant, error) {
	rows, err := q.QueryContext(ctx, s.q(query), args...)
	if err != nil {
		return nil, storeFailure("read verb grants", err)
	}

	defer func() { _ = rows.Close() }() // the scan error below is the one worth reporting

	var out []VerbGrant

	for rows.Next() {
		var objectType, objectID, verb, subjectType, subjectID, subjectRelation string
		if err = rows.Scan(
			&objectType, &objectID, &verb, &subjectType, &subjectID, &subjectRelation,
		); err != nil {
			return nil, storeFailure("scan a verb grant", err)
		}

		object, buildErr := NewObject(Type(objectType), objectID)
		if buildErr != nil {
			return nil, invalidStoredTuple.New("a stored verb grant names an object outside the vocabulary").
				Wrap(buildErr)
		}

		subject, buildErr := storedSubject(subjectType, subjectID, subjectRelation)
		if buildErr != nil {
			return nil, buildErr
		}

		grant, buildErr := NewVerbGrant(object, verb, subject)
		if buildErr != nil {
			return nil, invalidStoredTuple.New("a stored verb grant is not one this vocabulary allows").
				Wrap(buildErr)
		}

		out = append(out, grant)
	}

	if err = rows.Err(); err != nil {
		return nil, storeFailure("read verb grants", err)
	}

	return out, nil
}

// verbIndex is the dangerous tier as the compiler reads it: the rows keyed by
// the device they are written on, and the directory's kinds.
//
// The kinds come from the same read the ids come from, because a compile that
// knew an id and not its kind could not tell a dangerous verb from an ordinary
// one, and the scoped and whole recompiles would then disagree on the same
// database.
type verbIndex struct {
	grants map[string][]VerbGrant
	kinds  map[string]string
}

// loadVerbIndex reads the dangerous tier for one compile.
func (s *Store) loadVerbIndex(
	ctx context.Context, q queryer, kinds map[string]string, scoped []string,
) (verbIndex, error) {
	grants, err := s.verbGrantsFor(ctx, q, scoped)
	if err != nil {
		return verbIndex{}, err
	}

	out := verbIndex{grants: make(map[string][]VerbGrant, len(grants)), kinds: kinds}
	for _, grant := range grants {
		out.grants[grant.object.id] = append(out.grants[grant.object.id], grant)
	}

	return out, nil
}

// verbGrantsFor reads the rows one compile needs. A nil scope reads every row,
// which is the whole recompile; a scoped recompile names the devices it is
// rewriting, and that list is bounded by scopedCeiling.
func (s *Store) verbGrantsFor(ctx context.Context, q queryer, scoped []string) ([]VerbGrant, error) {
	if scoped == nil {
		return s.verbGrants(ctx, q, selectVerbGrantsSQL)
	}

	if len(scoped) == 0 {
		return nil, nil
	}

	query := selectVerbGrantsSQL + `
		WHERE object_type = 'device' AND object_id IN (` + placeholders(len(scoped)) + `)`

	args := make([]any, 0, len(scoped))
	for _, id := range scoped {
		args = append(args, id)
	}

	return s.verbGrants(ctx, q, query, args...)
}

// kindsOf is the directory as the compiler reads it: id to kind. It is the one
// shape both recompiles hand the verb index, so a scoped compile and a whole one
// cannot read the kind two ways.
func kindsOf(rows []directoryRow) map[string]string {
	out := make(map[string]string, len(rows))
	for _, row := range rows {
		out[row.id] = row.kind
	}

	return out
}

// sortedVerbs is a stable rendering of a verb set, so two engines write one
// projection in one order and a golden file does not move between runs.
func sortedVerbs(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for verb := range set {
		out = append(out, verb)
	}

	slices.Sort(out)

	return out
}

// SubjectDevices answers what one subject holds, device by device, for a caller
// who may administer those devices.
//
// It reads the projection the point check reads, so the answer cannot disagree
// with what the subject would actually be allowed to do. It is narrowed to the
// devices the *caller* administers: asking what somebody else holds is a read of
// their permissions, and reading a person's permissions on a machine is
// something you do because you administer that machine.
//
// The verbs are not in it. They are the caller's second read, because a verb
// depends on the device's kind and on the dangerous rows, and joining the two
// in one statement would put the verb table inside a query.
func (s *Store) SubjectDevices(ctx context.Context, who, subject Subject) ([]DeviceRow, error) {
	if err := requireAdministering(ctx, Object{}); err != nil {
		return nil, err
	}

	if who.IsZero() || subject.IsZero() {
		return nil, unknownType.New("a subject listing was asked for by or about an unconstructed subject")
	}

	administered, err := s.ListDevices(ctx, who, ActionAdminister, Page{Limit: maxSubjectDevices})
	if err != nil {
		return nil, err
	}

	held, err := s.ListDevices(ctx, subject, ActionObserve, Page{Limit: maxSubjectDevices})
	if err != nil {
		return nil, err
	}

	visible := make(map[string]bool, len(administered))
	for _, row := range administered {
		visible[row.ID] = true
	}

	out := make([]DeviceRow, 0, len(held))

	for _, row := range held {
		if visible[row.ID] {
			out = append(out, row)
		}
	}

	return out, nil
}

// maxSubjectDevices bounds the two listings this joins. It is the page bound the
// device listing already publishes: an answer about one person is read by a
// person, and a fleet larger than this is read a page at a time through the
// listing that pages.
const maxSubjectDevices = 200

// MaskOf is the action set these actions carry, for a caller rendering what a
// subject reaches. It is exported because the verb rendering needs it and the
// mask type is this package's own.
func MaskOf(actions []Action) uint32 {
	var out actionSet
	for _, action := range actions {
		out |= action.mask()
	}

	return uint32(out)
}

// VerbsReached is every verb of one kind a caller holding these classes may
// send, the dangerous ones only where a row or ownership conferred them.
//
// It is what makes a grant readable per kind. "Operator on a shelf" means
// nothing until the answer says which of the shelf's verbs it reaches, and
// this is that list. A dangerous verb needs its class as well as
// its row, because the command path checks the class first and the row after
// it, never instead of it.
func VerbsReached(kind DeviceKind, held uint32, dangerous map[string]bool) []string {
	verbs := table().byKind[kind.Or(DeviceKindPrinter)]

	out := make([]string, 0, len(verbs))

	for verb, entry := range verbs {
		if actionSet(held)&entry.class.mask() == 0 {
			continue
		}

		if entry.dangerous() && !dangerous[verb] {
			continue
		}

		out = append(out, verb)
	}

	slices.Sort(out)

	return out
}
