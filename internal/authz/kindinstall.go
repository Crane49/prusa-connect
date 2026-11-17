package authz

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
)

// Installing the per-build half of the verb table.
//
// It happens once, at start, from the declarations compiled into this binary,
// and never at the first command. There is no install step and no runtime
// admission: a kind's verbs are checkable because somebody compiled them in.
// The question left at start is whether what was compiled in agrees with what
// this deployment has seen before. The pin answers it.
//
// The pin is keyed by (module path, kind, verb). The module path is in the key
// because renaming the kind does not change it. A kind that reappears from a
// different module path than first seen is refused. No two publishers share a
// kind, which is the refusal holdKind already makes at the directory.

const (
	selectPinsSQL = `SELECT module, kind, verb, class, markings FROM authz_declared_retired
		ORDER BY module, kind, verb`

	insertPinSQL = `INSERT INTO authz_declared_retired (module, kind, verb, class, markings, first_seen)
		VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`

	movePinSQL = `UPDATE authz_declared_retired SET module = ? WHERE module = ? AND kind = ?`

	selectAcceptancesSQL = `SELECT module, kind, verb, accepted_to FROM authz_declared_acceptance
		ORDER BY module, kind, verb`

	insertAcceptanceSQL = `INSERT INTO authz_declared_acceptance
		(module, kind, verb, accepted_to, accepted_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (module, kind, verb) DO UPDATE SET accepted_to = ?, accepted_at = ?`
)

// DeclaredVerb is one verb of a compiled-in declaration, as it reaches this
// package: the class it carries and the markings, with the marker that put
// each one there. The host has already computed its own markings by the time
// this arrives. The opaque_payload marking is read off the capability
// descriptor and not off the declaration's own `dangerous` field, so a
// declaration cannot avoid it.
type DeclaredVerb struct {
	Class    Action
	Markings map[Marking]Marker
}

// KindDeclaration is one compiled-in device kind: the module that declared it,
// the qualified kind name, and its verbs.
type KindDeclaration struct {
	// Module is the Go module path the declaration was compiled from. It is
	// part of the first-seen key. The host derives it from the build graph,
	// and the declaring module never writes it.
	Module      string
	Kind        DeviceKind
	DisplayName string
	Verbs       map[string]DeclaredVerb
}

// KindReport is what one install found: what applied, and what is held.
type KindReport struct {
	// Installed is every kind whose verbs are now in the table, sorted.
	Installed []DeviceKind
	// Refused is the kinds whose declaration failed validation, with the
	// reason. Their verbs classify nowhere and the boot is not stopped: a
	// refusal is that kind's health condition and never the server's.
	Refused map[DeviceKind]string
	// Held is the disagreements with the first-seen record that do not apply,
	// one line per verb, naming the old and the new. An operator reads them in
	// the inventory and accepts one by name through the grants surface.
	Held []string
}

// InstallKindDeclarations validates the compiled-in declarations, compares each
// against the first-seen record, writes the record for what is new, and puts
// the resulting table in force.
//
// It is called once, at start, after the blank imports have run and before the
// first request is served. A refusal is per kind: a declaration that fails
// validation refuses that kind and never the boot, because a build that will
// not start is a build nobody can read the refusal from.
func (s *Store) InstallKindDeclarations(
	ctx context.Context, declared ...KindDeclaration,
) (KindReport, error) {
	report := KindReport{Refused: map[DeviceKind]string{}}

	pins, err := s.pins(ctx)
	if err != nil {
		return report, err
	}

	accepted, err := s.acceptances(ctx)
	if err != nil {
		return report, err
	}

	next := generatedTable()

	slices.SortFunc(declared, func(a, b KindDeclaration) int {
		return strings.Compare(a.Kind.String(), b.Kind.String())
	})

	var writes []pinRow

	for _, declaration := range declared {
		verbs, held, refusal := resolveKind(declaration, pins, accepted)
		if refusal != "" {
			report.Refused[declaration.Kind] = refusal

			continue
		}

		if err = next.add(declaration, verbs); err != nil {
			report.Refused[declaration.Kind] = err.Error()

			continue
		}

		report.Installed = append(report.Installed, declaration.Kind)
		report.Held = append(report.Held, held...)

		for verb, entry := range verbs {
			writes = append(writes, pinRow{
				module: declaration.Module, kind: declaration.Kind, verb: verb, entry: entry,
			})
		}
	}

	if err = s.writePins(ctx, writes); err != nil {
		return report, err
	}

	setTable(next)

	return report, nil
}

// resolveKind is one declaration against the record. It returns the verbs as
// they are in force, the disagreements it is holding, and a refusal that stops
// this kind entering the table at all.
//
// It makes the refusals the SDK's own validator has not already made at the
// adapter's build: the class must be a member of the closed set and at or
// above operate, no verb may carry a read action, and a kind that reappears
// from a different module path than first seen is refused.
func resolveKind(
	declaration KindDeclaration, pins map[pinKey]verbEntry, accepted map[pinKey]string,
) (map[string]verbEntry, []string, string) {
	if declaration.Kind == "" {
		return nil, nil, "the declaration names no kind"
	}

	if declaration.Module == "" {
		return nil, nil, "the host derived no module path for the declaring module, so nothing can be pinned"
	}

	if owner, seen := publisherOf(pins, declaration.Kind); seen && owner != declaration.Module {
		return nil, nil, fmt.Sprintf(
			"this deployment first saw kind %s declared by %s and this build declares it from %s. "+
				"Two publishers do not share a kind. If this is a legitimate module rename, accept it "+
				"through POST /api/v1/grants/kinds/%s/acceptances",
			declaration.Kind, owner, declaration.Module, declaration.Kind)
	}

	if len(declaration.Verbs) == 0 {
		return nil, nil, "the declaration carries no verbs, so the kind authorizes nothing"
	}

	out := make(map[string]verbEntry, len(declaration.Verbs))

	var held []string

	for _, verb := range slices.Sorted(maps.Keys(declaration.Verbs)) {
		declaredVerb := declaration.Verbs[verb]

		if err := ValidateVerb(verb); err != nil {
			return nil, nil, verb + ": " + err.Error()
		}

		if refusal := checkClass(verb, declaredVerb.Class); refusal != "" {
			return nil, nil, refusal
		}

		entry := verbEntry{class: declaredVerb.Class, markings: map[Marking]Marker{}}
		maps.Copy(entry.markings, declaredVerb.Markings)

		key := pinKey{module: declaration.Module, kind: declaration.Kind, verb: verb}
		if pinned, seen := pins[key]; seen {
			entry, held = applyPin(key, entry, pinned, accepted, held)
		}

		out[verb] = entry
	}

	return out, held, ""
}

// checkClass holds the class a verb may carry: the floor is operate, and
// observe, view_audit and administer are not classes a verb may carry.
func checkClass(verb string, class Action) string {
	switch class {
	case ActionOperate, ActionConfigure:
		return ""
	case ActionObserve, ActionViewAudit:
		return verb + " declares " + class.String() + ", which is a read surface and not a verb class. " +
			"Answering any verb puts a command on a wire that carries one at a time, so the floor for " +
			"a verb is operate"
	case ActionAdminister:
		return verb + " declares administer, which no verb may carry: a declaration that could hand " +
			"out administration would be an adapter writing itself into the lattice"
	case ActionReportTelemetry, ActionFetchJob:
		return verb + " declares " + class.String() + ", which is reachable from the agent relation " +
			"alone and is not a class"
	case ActionViewResource, ActionOperateResource, ActionConfigureResource,
		ActionReadStorage, ActionWriteStorage, ActionAdmitDevice, ActionDevelopDevice:
		return verb + " declares " + class.String() + ", which is an action on this server's " +
			"configuration and not on a device"
	default:
		return verb + " declares " + class.String() + ", which is not one of operate, configure"
	}
}

// applyPin compares one declared verb against its first-seen record. A class
// raised or a marking added applies silently, because reach only narrows. A
// class lowered or a marking removed does not apply: the verb keeps what the
// record holds, and the disagreement is named until an operator accepts it.
func applyPin(
	key pinKey, declared, pinned verbEntry, accepted map[pinKey]string, held []string,
) (verbEntry, []string) {
	out := verbEntry{class: declared.class, markings: map[Marking]Marker{}}
	maps.Copy(out.markings, declared.markings)

	// A marking the record holds and this build does not is carried forward.
	// Only the marker clears a marking, and a rebuild is not the marker.
	for marking, marker := range pinned.markings {
		if _, still := out.markings[marking]; !still {
			out.markings[marking] = marker
			held = append(held, fmt.Sprintf(
				"%s on %s: this build removes the %s marking that %s put there. The marking stands "+
					"until an operator accepts the change by name",
				key.verb, key.kind, marking, marker))
		}
	}

	if declared.class == pinned.class {
		return out, held
	}

	if accepted[key] == string(declared.class) {
		return out, held
	}

	if lowers(declared.class, pinned.class) {
		out.class = pinned.class
		held = append(held, fmt.Sprintf(
			"%s on %s: this deployment first saw it as %s and this build declares %s. The recorded "+
				"class stands, because a rebuild that lowers a class widens every grant already "+
				"written against the higher one",
			key.verb, key.kind, pinned.class, declared.class))
	}

	return out, held
}

// lowers reports whether a class is weaker than the one the record holds. The
// order is the class ladder: operate below configure. A class that is not lower
// applies silently, because reach only narrows.
func lowers(declared, pinned Action) bool { return classRank(declared) < classRank(pinned) }

// classRank orders the classes a verb may carry. It is separate from the mask
// because a mask is a set and this is a ladder, and the ladder exists only here.
func classRank(class Action) int {
	switch class {
	case ActionOperate:
		return rankOperate
	case ActionConfigure:
		return rankConfigure
	case ActionObserve, ActionAdminister, ActionViewAudit, ActionReportTelemetry, ActionFetchJob,
		ActionViewResource, ActionOperateResource, ActionConfigureResource,
		ActionReadStorage, ActionWriteStorage, ActionAdmitDevice, ActionDevelopDevice:
		// Not classes. checkClass has already refused a verb carrying one, so
		// nothing ranked here reaches the ladder.
		return notAClass
	default:
		return notAClass
	}
}

// The rungs of the class ladder. It is separate from the mask because a mask is
// a set and this is an order, and the order exists only here.
const (
	notAClass = iota
	rankOperate
	rankConfigure
)

// pinKey addresses one first-seen record.
type pinKey struct {
	module string
	kind   DeviceKind
	verb   string
}

// pinRow is one record to write.
type pinRow struct {
	module string
	kind   DeviceKind
	verb   string
	entry  verbEntry
}

// publisherOf is the module path this deployment first saw declare a kind, and
// false for a kind it has never seen.
func publisherOf(pins map[pinKey]verbEntry, kind DeviceKind) (string, bool) {
	for key := range pins {
		if key.kind == kind {
			return key.module, true
		}
	}

	return "", false
}

// pins reads the first-seen record whole. It is bounded by the kinds compiled
// into every binary this deployment has ever run times the verbs each declared,
// and it is read once at start.
func (s *Store) pins(ctx context.Context) (map[pinKey]verbEntry, error) {
	rows, err := s.db.QueryContext(ctx, s.q(selectPinsSQL))
	if err != nil {
		return nil, storeFailure("read the first-seen verb record", err)
	}

	defer func() { _ = rows.Close() }() // the scan error below is the one worth reporting

	out := map[pinKey]verbEntry{}

	for rows.Next() {
		var module, kind, verb, class, markings string
		if err = rows.Scan(&module, &kind, &verb, &class, &markings); err != nil {
			return nil, storeFailure("scan a first-seen verb record", err)
		}

		entry := verbEntry{class: Action(class), markings: map[Marking]Marker{}}

		for marking := range strings.SplitSeq(markings, ",") {
			name, marker, found := strings.Cut(marking, ":")
			if !found {
				continue
			}

			entry.markings[Marking(name)] = Marker(marker)
		}

		out[pinKey{module: module, kind: DeviceKind(kind), verb: verb}] = entry
	}

	if err = rows.Err(); err != nil {
		return nil, storeFailure("read the first-seen verb record", err)
	}

	return out, nil
}

// acceptances reads what an operator has accepted, keyed the way the pin is.
func (s *Store) acceptances(ctx context.Context) (map[pinKey]string, error) {
	rows, err := s.db.QueryContext(ctx, s.q(selectAcceptancesSQL))
	if err != nil {
		return nil, storeFailure("read the accepted declaration changes", err)
	}

	defer func() { _ = rows.Close() }() // the scan error below is the one worth reporting

	out := map[pinKey]string{}

	for rows.Next() {
		var module, kind, verb, to string
		if err = rows.Scan(&module, &kind, &verb, &to); err != nil {
			return nil, storeFailure("scan an accepted declaration change", err)
		}

		out[pinKey{module: module, kind: DeviceKind(kind), verb: verb}] = to
	}

	if err = rows.Err(); err != nil {
		return nil, storeFailure("read the accepted declaration changes", err)
	}

	return out, nil
}

// writePins records what this build declared, for every verb the record does
// not already hold. It never updates a row: the record is what was first seen,
// and a record that followed each build would pin nothing.
func (s *Store) writePins(ctx context.Context, rows []pinRow) error {
	if len(rows) == 0 {
		return nil
	}

	now := time.Now().UTC()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return storeFailure("open the first-seen record write", err)
	}

	defer func() { _ = tx.Rollback() }() // committed below; this is the failure path

	for _, row := range rows {
		if _, err = tx.ExecContext(ctx, s.q(insertPinSQL),
			row.module, row.kind.String(), row.verb,
			row.entry.class.String(), renderMarkings(row.entry), now,
		); err != nil {
			return storeFailure("write a first-seen verb record", err)
		}
	}

	if err = tx.Commit(); err != nil {
		return storeFailure("commit the first-seen record write", err)
	}

	return nil
}

// renderMarkings encodes a verb's markings for the record: name:marker pairs in
// the closed set's order, comma separated, so two engines and two runs write one
// string for one set.
func renderMarkings(entry verbEntry) string {
	out := make([]string, 0, len(entry.markings))
	for _, marking := range entry.markingNames() {
		out = append(out, marking.String()+":"+entry.markings[marking].String())
	}

	return strings.Join(out, ",")
}

// AcceptDeclarationChange records an operator accepting one held change, or a
// module rename the install refused.
//
// A verb names the change to a class: the verb keeps the recorded class until
// this row exists, and applies the declared one from the next start. An empty
// verb is the module-path acceptance: the old record is retired onto the new
// key, with every first-seen class and marking carried over unchanged, so
// acceptance moves the publisher's name and never a pin.
func (s *Store) AcceptDeclarationChange(
	ctx context.Context, who Subject, module string, kind DeviceKind, verb string, to string,
) error {
	if err := s.mayAdministerOrg(ctx, who); err != nil {
		return err
	}

	if module == "" || kind == "" {
		return declarationRefused.New("an acceptance names the module and the kind it is about").
			With(paramName(module), paramDeviceKind(kind))
	}

	if verb != "" {
		if err := ValidateVerb(verb); err != nil {
			return err
		}
	}

	pins, err := s.pins(ctx)
	if err != nil {
		return err
	}

	if err = checkAcceptance(pins, module, kind, verb, to); err != nil {
		return err
	}

	now := time.Now().UTC()

	if verb == "" {
		return s.acceptModuleRename(ctx, module, kind, to, now)
	}

	if _, err = s.db.ExecContext(ctx, s.q(insertAcceptanceSQL),
		module, kind.String(), verb, to, now, to, now,
	); err != nil {
		return storeFailure("record an accepted declaration change", err)
	}

	return nil
}

// checkAcceptance refuses an acceptance of something this deployment has never
// recorded.
//
// Acceptances are read back keyed by (module, kind, verb), so a row whose key
// matches no pin is never looked up. An operator who mistyped a verb would see
// a success, and at the next start the disagreement would still be held with
// nothing saying why. An acceptance is about a record, so the record has to be
// there.
//
// It is checked against the first-seen record and not against the installed
// verb table. A module rename is refused at install because no two publishers
// share a kind, so the kind being renamed is in no table when somebody accepts
// the rename. The pin survives that refusal, and the pin is what an acceptance
// moves.
func checkAcceptance(pins map[pinKey]verbEntry, module string, kind DeviceKind, verb, to string) error {
	publisher, seen := publisherOf(pins, kind)
	if !seen {
		return declarationRefused.New(
			"this deployment has no first-seen record for that kind, so there is no declaration "+
				"change to accept. A kind is recorded the first time a build declares it").
			With(paramName(module), paramDeviceKind(kind))
	}

	if publisher != module {
		return declarationRefused.New(
			"this deployment recorded that kind under another module path, and an acceptance names "+
				"the module the record is under").
			With(paramName(publisher), paramDeviceKind(kind))
	}

	if verb == "" {
		return nil
	}

	if _, recorded := pins[pinKey{module: module, kind: kind, verb: verb}]; !recorded {
		return declarationRefused.New(
			"that kind has no first-seen record for that verb, so there is no class to accept a "+
				"change to. Check the verb against the kind's row in the authorization vocabulary").
			With(paramName(verb), paramDeviceKind(kind))
	}

	// A verb acceptance names the class the operator is agreeing to, and it is
	// compared at the next start against what the build declares. A value
	// outside the closed set can never equal one, so it is a row that could
	// only ever do nothing.
	if refusal := checkClass(verb, Action(to)); refusal != "" {
		return declarationRefused.New("an acceptance names the class it is accepting, and "+refusal).
			With(paramName(verb), paramDeviceKind(kind))
	}

	return nil
}

// AcceptedChange is one recorded acceptance, as the operator surface renders it.
type AcceptedChange struct {
	// Module is the module path the kind's record is under.
	Module string
	// Kind is the device kind the acceptance is about.
	Kind DeviceKind
	// Verb is the verb whose class change was accepted, and it is empty for a
	// module rename.
	Verb string
	// To is what was accepted: the class for a verb, and the module path the
	// kind moved from for a rename.
	To string
	// Class and Markings are what the first-seen record holds for this verb,
	// which is what the acceptance was written against. They are empty for a
	// rename, which is about the whole kind and no one verb.
	Class    string
	Markings string
}

// Acceptances is every recorded acceptance for one kind, with the first-seen
// record each was written against, in a stable order.
//
// An acceptance changes what happens at the next start and nothing before it.
// This is where an operator sees what has been accepted before the restart.
func (s *Store) Acceptances(ctx context.Context, who Subject, kind DeviceKind) ([]AcceptedChange, error) {
	if err := s.mayAdministerOrg(ctx, who); err != nil {
		return nil, err
	}

	accepted, err := s.acceptances(ctx)
	if err != nil {
		return nil, err
	}

	pins, err := s.pins(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]AcceptedChange, 0, len(accepted))

	for key, to := range accepted {
		if key.kind != kind {
			continue
		}

		change := AcceptedChange{Module: key.module, Kind: key.kind, Verb: key.verb, To: to}

		if pinned, recorded := pins[key]; recorded {
			change.Class = pinned.class.String()
			change.Markings = renderMarkings(pinned)
		}

		out = append(out, change)
	}

	slices.SortFunc(out, func(a, b AcceptedChange) int {
		if by := strings.Compare(a.Module, b.Module); by != 0 {
			return by
		}

		return strings.Compare(a.Verb, b.Verb)
	})

	return out, nil
}

// acceptModuleRename moves a kind's whole record onto the new module path. The
// rows carry their first-seen class and marking with them, so only the
// publisher's name moves.
func (s *Store) acceptModuleRename(
	ctx context.Context, from string, kind DeviceKind, to string, now time.Time,
) error {
	if to == "" {
		return declarationRefused.New(
			"a module-path acceptance names the module path the kind is moving to").
			With(paramName(from), paramDeviceKind(kind))
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return storeFailure("open the module rename", err)
	}

	defer func() { _ = tx.Rollback() }() // committed below; this is the failure path

	if _, err = tx.ExecContext(ctx, s.q(movePinSQL), to, from, kind.String()); err != nil {
		return storeFailure("move the first-seen record onto the new module path", err)
	}

	if _, err = tx.ExecContext(ctx, s.q(insertAcceptanceSQL),
		to, kind.String(), "", from, now, from, now,
	); err != nil {
		return storeFailure("record the accepted module rename", err)
	}

	if err = tx.Commit(); err != nil {
		return storeFailure("commit the module rename", err)
	}

	return nil
}

// mayAdministerOrg refuses a caller who does not administer this server's
// configuration. An acceptance moves what a verb means for every device of a
// kind, so it is the org's decision and not one device's.
func (s *Store) mayAdministerOrg(ctx context.Context, who Subject) error {
	if who.IsZero() {
		return unknownType.New("an acceptance was made by an unconstructed subject")
	}

	// The object is the driver kind's configuration surface, because that is
	// where a device kind's declaration comes from: a driver is what declares
	// one, and administering the driver configuration is administering which
	// drivers this server runs.
	object, err := NewObject(TypeResourceKind, ref.KindDriver.String())
	if err == nil {
		err = requireAdministering(ctx, object)
	}

	if err != nil {
		return err
	}

	allowed, err := s.Can(ctx, who, ActionConfigureResource, object)
	if err != nil {
		return err
	}

	if !allowed {
		return accessDenied.New(
			"accepting a declaration change moves what a verb means on every device of that kind, "+
				"so it takes the grant that administers this server's driver configuration").
			With(paramObject(object), paramAction(ActionConfigureResource))
	}

	return nil
}

// add puts one validated declaration into the table under construction,
// refusing a kind the generated half already carries and a verb another kind's
// module already owns.
func (t *verbTable) add(declaration KindDeclaration, verbs map[string]verbEntry) error {
	if _, taken := t.byKind[declaration.Kind]; taken {
		return fmt.Errorf(
			"%w: this build already carries a device kind called %s", ErrKindTaken, declaration.Kind)
	}

	t.byKind[declaration.Kind] = verbs

	rows := make([]VerbRow, 0, len(verbs))

	for _, verb := range slices.Sorted(maps.Keys(verbs)) {
		t.union[verb] = true
		rows = append(rows, VerbRow{
			Verb: verb, Class: verbs[verb].class, Markings: markingRows(verbs[verb]),
		})
	}

	t.kinds = append(t.kinds, KindVerbs{
		Module:      declaration.Module,
		Kind:        declaration.Kind,
		DisplayName: declaration.DisplayName,
		Verbs:       rows,
	})

	return nil
}

// ErrKindTaken reports a declared kind whose name this build already carries.
// It is an error and not a replacement: a declaration that silently overwrote
// the printer's verbs is how an adapter takes over the core's own vocabulary.
var ErrKindTaken = errors.New("authz: device kind already declared")
