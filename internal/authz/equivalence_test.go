package authz

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
)

// oracle answers the same question the store answers, by searching the tuples
// on every call instead of reading a projection compiled from them. It is a
// second implementation on purpose: a disagreement between the two is exactly
// the defect class this design exists to prevent, and a projection that goes
// wrong silently is worth more to catch than one that goes wrong loudly.
//
// It shares no table with the implementation. The relation names, the role
// ordering, the action-to-role mapping and the agent actions below are
// transcribed from the package doc by hand, as strings, so that widening a mask
// in vocabulary.go cannot widen the oracle with it. That independence is the
// only property that makes this file worth having.
type oracle struct {
	tuples    []Tuple
	printers  map[string]bool
	resources map[string]bool
}

// oracleLevel is the oracle's own role ordering, unrelated to the level type
// the implementation uses.
type oracleLevel int

const (
	oracleNone oracleLevel = iota
	oracleViewer
	oracleOperator
	oracleManager
)

// The vocabulary as the package doc states it, keyed by plain strings so that
// renaming a constant cannot quietly re-point this table.
var (
	oracleDeviceRoles = map[string]oracleLevel{
		"viewer":   oracleViewer,
		"operator": oracleOperator,
		"manager":  oracleManager,
	}

	oracleOrgRoles = map[string]oracleLevel{
		"viewer":   oracleViewer,
		"operator": oracleOperator,
		"admin":    oracleManager,
	}

	// "observe viewer, or the device's own agent; operate operator; configure,
	// administer and view_audit manager."
	oracleActionRoles = map[string]oracleLevel{
		"observe":    oracleViewer,
		"operate":    oracleOperator,
		"configure":  oracleManager,
		"administer": oracleManager,
		"view_audit": oracleManager,
	}

	// "owner is not a role and has no level. It adds observe, operate and
	// configure to whatever the role layer already gave that principal, and
	// stops short of administer and of view_audit." The dangerous tier is not
	// here because it is not a mask: ownership reaches it through the verb
	// projection, which this oracle does not model.
	oracleOwnerActions = map[string]bool{
		"observe":   true,
		"operate":   true,
		"configure": true,
	}

	// "report_telemetry, fetch_job agent only", and observe, which an agent
	// also reaches.
	oracleAgentActions = map[string]bool{
		"observe":          true,
		"report_telemetry": true,
		"fetch_job":        true,
	}

	// "view_resource viewer; operate_resource operator; configure_resource
	// manager; read_storage viewer; write_storage operator", on a resource kind
	// and never on an instance.
	oracleResourceActionRoles = map[string]oracleLevel{
		"view_resource":      oracleViewer,
		"operate_resource":   oracleOperator,
		"configure_resource": oracleManager,
		"read_storage":       oracleViewer,
		"write_storage":      oracleOperator,
		"admit_device":       oracleManager,
		"develop_device":     oracleManager,
	}

	// The two action domains, transcribed. Their union is held to Actions() by
	// TestOracleCoversTheVocabulary, and the sweeping tests use them so a new
	// verb is swept against the object type it is defined on.
	oraclePrinterActions = []string{
		"observe", "operate", "configure", "administer", "view_audit",
		"report_telemetry", "fetch_job",
	}

	oracleResourceActions = []string{
		"view_resource", "operate_resource", "configure_resource",
		"read_storage", "write_storage", "admit_device", "develop_device",
	}

	// Every action the package doc lists. A new action that reaches neither
	// table is caught by TestOracleCoversTheVocabulary rather than passing as
	// a silent deny.
	oracleActions = slices.Concat(oraclePrinterActions, oracleResourceActions)
)

// printerActions and resourceActions are the two sweeps, as the typed values
// the store takes.
func printerActions() []Action { return asActions(oraclePrinterActions) }

func resourceActions() []Action { return asActions(oracleResourceActions) }

func asActions(names []string) []Action {
	out := make([]Action, 0, len(names))
	for _, name := range names {
		out = append(out, Action(name))
	}

	return out
}

func newOracle(tb testing.TB, store *Store) oracle {
	tb.Helper()

	stored, err := store.Tuples(tb.Context())
	require.NoError(tb, err)

	printers := make(map[string]bool, len(fixtureDirectory))
	for _, row := range fixtureDirectory {
		printers[row.ID] = true
	}

	registered := fixtureResources(tb)

	resources := make(map[string]bool, len(registered))
	for _, instance := range registered {
		resources[instance.String()] = true
	}

	return oracle{tuples: stored, printers: printers, resources: resources}
}

// canResource is the resource half, walked from the raw tuples: the highest
// role written on the kind or reached through the org that kind names, minus a
// block, and then only for an instance the directory holds.
func (o oracle) canResource(who Subject, action Action, instance ref.Ref) bool {
	need, ok := oracleResourceActionRoles[string(action)]
	if !ok {
		return false
	}

	if !o.resources[instance.String()] {
		return false
	}

	kind := instance.Kind().String()

	if o.isBlockedOn("resource_kind", kind, who) {
		return false
	}

	return o.resourceLevel(who, kind) >= need
}

func (o oracle) resourceLevel(who Subject, kind string) oracleLevel {
	best := o.roleOn("resource_kind", kind, who, oracleDeviceRoles)

	for _, org := range o.linked(t2{"resource_kind", kind}, "org") {
		best = max(best, o.roleOn("org", org, who, oracleOrgRoles))
	}

	return best
}

func (o oracle) can(who Subject, action Action, printer string) bool {
	verb := string(action)

	if !o.printers[printer] {
		return false
	}

	if oracleAgentActions[verb] && o.isAgent(who, printer) {
		return true
	}

	need, needsRole := oracleActionRoles[verb]
	if !needsRole {
		return false
	}

	return o.reaches(who, verb, need, printer, map[string]bool{})
}

// reaches is the device arm, walked down device#parent as well as up the group
// and org trees. A device may sit inside another device, which is ADR-20260826-5yj
// rule 15 and today is a camera and the printer it watches, and whoever reaches
// the printer reaches the camera at the mask they reached the printer with.
//
// Three things the edge does not carry, each transcribed from the package doc
// rather than from the compiler. A block on the child cuts the whole walk, so
// one camera can be hidden from somebody who may see its printer. Nothing flows
// upwards: the walk only ever reads the devices a device names as its parents.
// And a device credential does not cross, because a device reaches the device
// it is the agent of and nothing else.
//
// The visited set is per top-level call, so a parent cycle converges. Sharing
// it across the parents of one device is safe because a false answer is only
// returned once every path out of that device has been walked and answered
// false too.
func (o oracle) reaches(who Subject, verb string, need oracleLevel, printer string, seen map[string]bool) bool {
	if seen[printer] {
		return false
	}

	seen[printer] = true

	if o.isBlocked(who, printer) {
		return false
	}

	// Ownership adds rather than raising, so it is its own arm and not a level
	// this walk compares against. A block above already refused it.
	if oracleOwnerActions[verb] && o.isOwner(who, printer) {
		return true
	}

	if o.deviceLevel(who, printer) >= need {
		return true
	}

	if string(who.typ) == "device_credential" {
		return false
	}

	for _, parent := range o.linked(t2{"device", printer}, "parent") {
		if o.reaches(who, verb, need, parent, seen) {
			return true
		}
	}

	return false
}

// isOwner reports whether this principal is named as an owner of the device,
// directly or through a group written as a userset.
func (o oracle) isOwner(who Subject, printer string) bool {
	for _, t := range o.tuples {
		if o.names(t, "device", printer, "owner") && o.holds(t.subject, who) {
			return true
		}
	}

	return false
}

func (o oracle) isAgent(who Subject, printer string) bool {
	for _, t := range o.tuples {
		if o.names(t, "device", printer, "agent") &&
			string(t.subject.typ) == string(who.typ) && t.subject.id == who.id {
			return true
		}
	}

	return false
}

func (o oracle) isBlocked(who Subject, printer string) bool {
	return o.isBlockedOn("device", printer, who)
}

func (o oracle) isBlockedOn(objectType, id string, who Subject) bool {
	for _, t := range o.tuples {
		if o.names(t, objectType, id, "blocked") && o.holds(t.subject, who) {
			return true
		}
	}

	return false
}

func (o oracle) deviceLevel(who Subject, printer string) oracleLevel {
	best := o.roleOn("device", printer, who, oracleDeviceRoles)

	for _, group := range o.linked(t2{"device", printer}, "group") {
		best = max(best, o.groupLevel(group, who, map[string]bool{}))
	}

	for _, org := range o.orgsOf(printer) {
		best = max(best, o.roleOn("org", org, who, oracleOrgRoles))
	}

	return best
}

// orgsOf is the org of the printer, plus the org of any group above it.
func (o oracle) orgsOf(printer string) []string {
	orgs := o.linked(t2{"device", printer}, "org")

	for _, group := range o.linked(t2{"device", printer}, "group") {
		for _, ancestor := range o.chain(group, map[string]bool{}) {
			orgs = append(orgs, o.linked(t2{"device_group", ancestor}, "org")...)
		}
	}

	return orgs
}

func (o oracle) groupLevel(group string, who Subject, seen map[string]bool) oracleLevel {
	if seen[group] {
		return oracleNone
	}

	seen[group] = true

	best := o.roleOn("device_group", group, who, oracleDeviceRoles)

	for _, parent := range o.linked(t2{"device_group", group}, "parent") {
		best = max(best, o.groupLevel(parent, who, seen))
	}

	return best
}

// chain is a printer group and every ancestor above it.
func (o oracle) chain(group string, seen map[string]bool) []string {
	if seen[group] {
		return nil
	}

	seen[group] = true

	out := []string{group}
	for _, parent := range o.linked(t2{"device_group", group}, "parent") {
		out = append(out, o.chain(parent, seen)...)
	}

	return out
}

func (o oracle) roleOn(objectType, id string, who Subject, roles map[string]oracleLevel) oracleLevel {
	best := oracleNone

	for _, t := range o.tuples {
		level, isRole := roles[string(t.relation)]
		if !isRole || !o.names(t, objectType, id, string(t.relation)) {
			continue
		}

		if o.holds(t.subject, who) && level > best {
			best = level
		}
	}

	return best
}

// holds reports whether the subject of a tuple covers this principal, following
// a userset into the groups it names. Only group#member is a userset here, as
// the package doc says; anything else covers nobody.
func (o oracle) holds(granted, who Subject) bool {
	if !granted.IsUserset() {
		return string(granted.typ) == string(who.typ) && granted.id == who.id
	}

	if string(granted.typ) != "group" || string(granted.rel) != "member" {
		return false
	}

	return o.memberOf(granted.id, who, map[string]bool{})
}

func (o oracle) memberOf(group string, who Subject, seen map[string]bool) bool {
	if seen[group] {
		return false
	}

	seen[group] = true

	for _, t := range o.tuples {
		if !o.names(t, "group", group, "member") {
			continue
		}

		if t.subject.IsUserset() {
			if string(t.subject.typ) == "group" && string(t.subject.rel) == "member" &&
				o.memberOf(t.subject.id, who, seen) {
				return true
			}

			continue
		}

		if string(t.subject.typ) == string(who.typ) && t.subject.id == who.id {
			return true
		}
	}

	return false
}

// t2 is an object named the way the oracle names one: two strings.
type t2 struct {
	Type string
	ID   string
}

// linked returns the ids this object points at through a structural relation.
func (o oracle) linked(object t2, relation string) []string {
	var out []string

	for _, t := range o.tuples {
		if o.names(t, object.Type, object.ID, relation) {
			out = append(out, t.subject.id)
		}
	}

	return out
}

func (o oracle) names(t Tuple, objectType, id, relation string) bool {
	return string(t.object.typ) == objectType && t.object.id == id && string(t.relation) == relation
}

// TestOracleCoversTheVocabulary fails when an action is added to the
// implementation and not to the transcription above, so the oracle cannot go
// quietly blind to a new verb.
func TestOracleCoversTheVocabulary(t *testing.T) {
	t.Parallel()

	implemented := make([]string, 0, len(Actions()))
	for _, action := range Actions() {
		implemented = append(implemented, string(action))
	}

	transcribed := slices.Clone(oracleActions)
	slices.Sort(implemented)
	slices.Sort(transcribed)
	require.Equal(t, implemented, transcribed, "the oracle's action list is transcribed from the package doc by hand")

	for _, verb := range oraclePrinterActions {
		_, role := oracleActionRoles[verb]
		require.Truef(t, role || oracleAgentActions[verb], "%s reaches neither table", verb)
	}

	for _, verb := range oracleResourceActions {
		require.Containsf(t, oracleResourceActionRoles, verb, "%s reaches no role", verb)
	}

	// The two domains partition the vocabulary. An action in both, or in
	// neither, would leave one sweep testing a verb the other also owns.
	for _, action := range Actions() {
		domain, ok := action.domain()
		require.Truef(t, ok, "%s has no object type", action)

		onPrinter := slices.Contains(oraclePrinterActions, string(action))
		require.Equalf(t, domain == TypeDevice, onPrinter, "%s is swept against the wrong type", action)
	}
}

// TestPointCheckMatchesTheGraphWalk sweeps every principal, action and printer
// of the fixture and holds the projection to what a walk of the raw tuples
// says.
func TestPointCheckMatchesTheGraphWalk(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)
		reference := newOracle(t, store)

		for _, name := range fixturePrincipals {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				who := subjectOf(t, name)

				for _, action := range printerActions() {
					for _, id := range append(directoryIDs(), "p-hidden") {
						got, err := store.Can(t.Context(), who, action, deviceOf(t, id))
						require.NoError(t, err)
						require.Equalf(t, reference.can(who, action, id), got,
							"%s %s on device:%s", name, action, id)
					}
				}
			})
		}
	})
}

// TestListMatchesThePointCheck is the invariant the two enforcement surfaces
// exist to keep: for every principal and every action, the set the list returns
// equals the set the point check permits.
func TestListMatchesThePointCheck(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)

		for _, name := range fixturePrincipals {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				who := subjectOf(t, name)

				for _, action := range printerActions() {
					var permitted []string

					for _, id := range directoryIDs() {
						allowed, err := store.Can(t.Context(), who, action, deviceOf(t, id))
						require.NoError(t, err)

						if allowed {
							permitted = append(permitted, id)
						}
					}

					rows, err := store.ListDevices(t.Context(), who, action, Page{Limit: maxPageLimit})
					require.NoError(t, err)

					listed := ids(rows)
					slices.Sort(permitted)
					slices.Sort(listed)
					require.Equalf(t, permitted, listed, "%s %s", name, action)

					total, err := store.CountDevices(t.Context(), who, action)
					require.NoError(t, err)
					require.Equalf(t, len(permitted), total, "%s %s count", name, action)
				}
			})
		}
	})
}

// TestListMatchesTheGraphWalkAfterRevocation runs the same invariant over a
// graph that has been edited, because a projection is at its most suspect right
// after a write.
func TestListMatchesTheGraphWalkAfterRevocation(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)

		require.NoError(t, store.Delete(t.Context(),
			mustTuple(t, "device_group:floor-one#operator@group:maintenance#member"),
			mustTuple(t, "org:acme#viewer@user:olga"),
		))
		require.NoError(t, store.Write(t.Context(),
			mustTuple(t, "device:p-gamma#manager@user:dana"),
		))

		reference := newOracle(t, store)

		for _, name := range fixturePrincipals {
			who := subjectOf(t, name)

			for _, action := range printerActions() {
				var permitted []string

				for _, id := range directoryIDs() {
					if reference.can(who, action, id) {
						permitted = append(permitted, id)
					}
				}

				rows, err := store.ListDevices(t.Context(), who, action, Page{Limit: maxPageLimit})
				require.NoError(t, err)

				listed := ids(rows)
				slices.Sort(permitted)
				slices.Sort(listed)
				require.Equalf(t, permitted, listed, "%s %s", name, action)
			}
		}
	})
}

// TestVisibleMatchesTheGraphWalk holds the third enforcement surface to the
// same walk the point check and the list are held to. Visible builds a
// subscription's routing table, so an origin it carries and the walk refuses is
// an event stream showing a device nobody granted, and an origin it leaves out
// is a device the reader never learns exists.
func TestVisibleMatchesTheGraphWalk(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)
		reference := newOracle(t, store)

		for _, name := range fixturePrincipals {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				who := subjectOf(t, name)

				seen, err := store.Visible(t.Context(), who)
				require.NoError(t, err)

				devices := []string{}

				for _, id := range append(directoryIDs(), "p-hidden") {
					if reference.can(who, ActionObserve, id) {
						devices = append(devices, id)
					}
				}

				listed := make([]string, 0, len(seen.Devices))
				for _, device := range seen.Devices {
					listed = append(listed, device.ID)
				}

				slices.Sort(devices)
				slices.Sort(listed)
				require.Equalf(t, devices, listed, "%s observes", name)

				resources := []string{}

				for _, instance := range resourceUniverse(t) {
					if reference.canResource(who, ActionViewResource, instance) {
						resources = append(resources, instance.String())
					}
				}

				held := make([]string, 0, len(seen.Resources))
				for _, instance := range seen.Resources {
					held = append(held, instance.Ref.String())
				}

				slices.Sort(resources)
				slices.Sort(held)
				require.Equalf(t, resources, held, "%s watches", name)
			})
		}
	})
}

// TestVisibleMatchesTheGraphWalkAfterRevocation runs the same invariant over a
// graph that has been edited. A projection is at its most suspect right after a
// write, and a removed grant that an open stream keeps is the failure the epoch
// beside this answer exists to stop.
func TestVisibleMatchesTheGraphWalkAfterRevocation(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)

		require.NoError(t, store.Delete(t.Context(),
			mustTuple(t, "device_group:floor-one#operator@group:maintenance#member"),
			mustTuple(t, "org:acme#viewer@user:olga"),
		))
		require.NoError(t, store.Write(t.Context(),
			mustTuple(t, "device:p-gamma#manager@user:dana"),
		))

		reference := newOracle(t, store)

		for _, name := range fixturePrincipals {
			who := subjectOf(t, name)

			seen, err := store.Visible(t.Context(), who)
			require.NoError(t, err)

			devices := []string{}

			for _, id := range directoryIDs() {
				if reference.can(who, ActionObserve, id) {
					devices = append(devices, id)
				}
			}

			listed := make([]string, 0, len(seen.Devices))
			for _, device := range seen.Devices {
				listed = append(listed, device.ID)
			}

			slices.Sort(devices)
			slices.Sort(listed)
			require.Equalf(t, devices, listed, "%s observes", name)
		}
	})
}
