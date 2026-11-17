package authz

import (
	"cmp"
	"slices"
)

// principal is a subject that can hold access: never a userset, always the
// thing a credential resolves to. It carries the type as well as the id,
// because two objects of different types may share an id and must never share
// a grant.
type principal struct {
	typ Type
	id  string
}

// principal returns the object seen as a holder of grants, which is what the
// membership closure is keyed by.
func (o Object) principal() principal { return principal(o) }

// accessRow is one row of the device projection: what this principal may do on
// this device, after groups, nesting, org roles and blocks have been resolved.
type accessRow struct {
	Subject principal
	Device  string
	Actions actionSet
}

// resourceAccessRow is one row of the resource projection: what this principal
// may do to the configuration of one resource kind. It is keyed by kind and not
// by instance, because a grant on a resource kind is what the vocabulary
// carries. See the package doc for why.
type resourceAccessRow struct {
	Subject principal
	Kind    string
	Actions actionSet
}

// projection is everything one compile of the graph produces. Both halves come
// out of one walk, so a write cannot leave the two tables at different
// generations of the same tuple set.
type projection struct {
	Devices   []accessRow
	Resources []resourceAccessRow
	// Verbs is the dangerous tier compiled the same way: who reaches which verb
	// on which device. It is a table of its own and not a mask, because a verb
	// is not a bit.
	Verbs []verbRow
}

// verbRow is one row of the verb projection: this principal reaches this verb
// on this device. It carries no mask and no condition, because a dangerous verb
// is reached or it is not.
type verbRow struct {
	Subject principal
	Device  string
	Verb    string
}

// roleGrant pairs a role relation with the level it confers. The two tables
// below are the whole role layer, and the only place the order of the roles is
// written down.
type roleGrant struct {
	Relation Relation
	Level    level
}

var (
	// directRoles is the role layer as it is written on an object directly: on
	// a device, on a device group, and on a resource kind.
	directRoles = []roleGrant{
		{Relation: RelViewer, Level: levelViewer},
		{Relation: RelOperator, Level: levelOperator},
		{Relation: RelManager, Level: levelManager},
	}

	// orgRoles is the role layer as the org carries it, weakest first. The order
	// matters: OrgRoleConferring walks this list and answers the first role
	// whose level reaches the action, which is the smallest role that confers
	// it. Reversing it would hand out the admin role where the viewer role was
	// enough.
	//
	// orgRoleOrder in orgrole.go is the same relations strongest first, because
	// a listing describes a person who holds two by the stronger. Neither order
	// may be derived from the other.
	orgRoles = []roleGrant{
		{Relation: RelViewer, Level: levelViewer},
		{Relation: RelOperator, Level: levelOperator},
		{Relation: RelAdmin, Level: levelManager},
	}
)

// roleKey addresses the subjects holding one relation on one object.
type roleKey struct {
	Object   Type
	ID       string
	Relation Relation
}

// origin says where the grant behind a level is written: on the object itself,
// or on a group or an org above it. The projection sums the two, and the grant
// surface keeps them apart, because a caller who administers an object can
// write back what is written there and cannot write back what flows in from
// above. See mayRemove.
type origin uint8

const (
	writtenHere origin = iota
	writtenAbove
)

// standing is the level one principal reaches on one object, by origin.
type standing struct {
	here  level
	above level
}

// top is the level the projection carries: the highest by any path.
func (s standing) top() level { return max(s.here, s.above) }

// raise lifts one half of a standing, keeping the higher of the two.
func (s standing) raise(lvl level, from origin) standing {
	if from == writtenAbove {
		s.above = max(s.above, lvl)

		return s
	}

	s.here = max(s.here, lvl)

	return s
}

// access is what one principal holds on one object, and where each part of it
// comes from. held is the mask the projection stores; the other three say where
// it was written, because mayRemove orders a caller against a victim by source
// and not by mask.
//
//   - writtenHere is what is written on this object itself. mayRemove never
//     reads it: administering the object is what the caller was checked for, so
//     what is written here is already theirs. It is carried because a parent
//     edge takes it down onto the child, where it is no longer written here.
//   - inherited is what flows down the group and org trees, over this object
//     and over every device above it. That is tenancy, written by whoever
//     administers the tree rather than by whoever administers the object.
//   - fromParent is what another device confers through device#parent: the
//     part written on that parent, and the part its own parents conferred on
//     it. Anybody holding administer on the child may write that edge, so this is
//     the one source a caller can point at themselves.
//
// The parent's own split is kept. Folding the parent's total into one mask on
// the child loses which part of it tenancy conferred. On a device reached only
// through a parent edge, a delegate and the org administrator could then not
// be told apart. Every camera bound to a printer is such a device, because
// cameraSettings.placement writes no org tuple on one. See mayRemove.
type access struct {
	held        actionSet
	writtenHere actionSet
	inherited   actionSet
	fromParent  actionSet
}

// reaches reports whether this access carries anything at all.
func (a access) reaches() bool { return a.held != 0 }

// graph is the tuple table indexed for the compiler. It is rebuilt from
// scratch on every write, so it holds no state between calls. Every map is
// keyed by the whole object, type included: an id on its own is not an
// identity here.
type graph struct {
	roles        map[roleKey][]Subject
	groupMembers map[principal][]Subject
	blocked      map[string][]Subject
	owners       map[string][]Subject
	agents       map[string][]string
	kindBlocked  map[string][]Subject
	kindOrg      map[string][]string
	deviceGroup  map[string][]string
	deviceParent map[string][]string
	deviceOrg    map[string][]string
	groupParent  map[string][]string
	groupOrg     map[string][]string

	// verbs is the dangerous tier: the per-verb rows indexed by the device
	// they are written on, and the directory's kinds. It is beside the tuple
	// index and not inside it, because a verb is not a bit in a projected
	// mask.
	verbs verbIndex

	expanded  map[principal]map[principal]bool
	ancestors map[string][]string

	// reached memoizes one device's resolved access, so a device with two
	// parents is walked once rather than once per path below it. Only a walk
	// that met no cycle is stored: a result truncated by the visiting set is
	// the answer for that chain and not the answer for the device.
	reached map[string]map[principal]access
}

// project compiles the tuples and the verb grants into the projections in one
// walk. A principal reaching a device with at least one action gets a device
// row; a principal reaching a resource kind gets a resource row. Devices absent
// from the directory produce no rows: a grant on a device the server does not
// know about fails closed. Kinds are the closed set and not a directory read,
// so a grant naming a kind outside it fails closed the same way.
//
// devices carries the kind as well as the id, because the verb table is keyed
// by device kind.
func project(tuples []Tuple, devices []directoryRow, kinds []string, verbs verbIndex) projection {
	g := indexTuples(tuples)
	g.verbs = verbs

	out := projection{
		Devices:   make([]accessRow, 0, len(devices)),
		Resources: make([]resourceAccessRow, 0, len(kinds)),
		Verbs:     make([]verbRow, 0, len(verbs.grants)),
	}

	for _, row := range devices {
		out.Devices = append(out.Devices, g.rowsFor(row)...)
		out.Verbs = append(out.Verbs, g.verbRowsFor(row)...)
	}

	for _, kind := range kinds {
		out.Resources = append(out.Resources, g.resourceRowsFor(kind)...)
	}

	slices.SortFunc(out.Devices, func(a, b accessRow) int {
		if c := comparePrincipals(a.Subject, b.Subject); c != 0 {
			return c
		}

		return cmp.Compare(a.Device, b.Device)
	})

	slices.SortFunc(out.Resources, func(a, b resourceAccessRow) int {
		if c := comparePrincipals(a.Subject, b.Subject); c != 0 {
			return c
		}

		return cmp.Compare(a.Kind, b.Kind)
	})

	return out
}

// comparePrincipals orders two holders of grants, type before id, so the two
// projections are written in one deterministic order.
func comparePrincipals(a, b principal) int {
	if c := cmp.Compare(a.typ, b.typ); c != 0 {
		return c
	}

	return cmp.Compare(a.id, b.id)
}

func indexTuples(tuples []Tuple) *graph {
	g := &graph{
		roles:        make(map[roleKey][]Subject),
		groupMembers: make(map[principal][]Subject),
		blocked:      make(map[string][]Subject),
		owners:       make(map[string][]Subject),
		agents:       make(map[string][]string),
		deviceGroup:  make(map[string][]string),
		deviceParent: make(map[string][]string),
		deviceOrg:    make(map[string][]string),
		groupParent:  make(map[string][]string),
		groupOrg:     make(map[string][]string),
		kindBlocked:  make(map[string][]Subject),
		kindOrg:      make(map[string][]string),
		ancestors:    make(map[string][]string),
		reached:      make(map[string]map[principal]access),
	}

	for _, t := range tuples {
		g.add(t)
	}

	return g
}

func (g *graph) add(t Tuple) {
	object, relation, subject := t.object, t.relation, t.subject

	switch {
	case object.typ == TypeGroup && relation == RelMember:
		key := object.principal()
		g.groupMembers[key] = append(g.groupMembers[key], subject)
	case object.typ == TypeDevice && relation == RelBlocked:
		g.blocked[object.id] = append(g.blocked[object.id], subject)
	case object.typ == TypeDevice && relation == RelOwner:
		g.owners[object.id] = append(g.owners[object.id], subject)
	case object.typ == TypeDevice && relation == RelAgent:
		g.agents[object.id] = append(g.agents[object.id], subject.id)
	case object.typ == TypeDevice && relation == RelGroup:
		g.deviceGroup[object.id] = append(g.deviceGroup[object.id], subject.id)
	case object.typ == TypeDevice && relation == RelParent:
		g.deviceParent[object.id] = append(g.deviceParent[object.id], subject.id)
	case object.typ == TypeDevice && relation == RelOrg:
		g.deviceOrg[object.id] = append(g.deviceOrg[object.id], subject.id)
	case object.typ == TypeDeviceGroup && relation == RelParent:
		g.groupParent[object.id] = append(g.groupParent[object.id], subject.id)
	case object.typ == TypeDeviceGroup && relation == RelOrg:
		g.groupOrg[object.id] = append(g.groupOrg[object.id], subject.id)
	case object.typ == TypeResourceKind && relation == RelBlocked:
		g.kindBlocked[object.id] = append(g.kindBlocked[object.id], subject)
	case object.typ == TypeResourceKind && relation == RelOrg:
		g.kindOrg[object.id] = append(g.kindOrg[object.id], subject.id)
	default:
		if _, isRole := roleLevel(relation); isRole {
			key := roleKey{Object: object.typ, ID: object.id, Relation: relation}
			g.roles[key] = append(g.roles[key], subject)
		}
	}
}

// rowsFor resolves one device: the highest role each principal reaches by any
// path, minus the blocks, plus ownership and the device's own agent.
func (g *graph) rowsFor(device directoryRow) []accessRow {
	reached := g.deviceAccess(device)

	out := make([]accessRow, 0, len(reached))

	for who, held := range reached {
		if !held.reaches() {
			continue
		}

		out = append(out, accessRow{Subject: who, Device: device.id, Actions: held.held})
	}

	return out
}

// deviceAccess is what every principal holds on one device, and how much of it
// is written above the device. rowsFor keeps the first half. The grant surface
// compares both, so the projection and the rule that bounds a grant read one
// compiler.
//
// A device may sit inside another device, as a camera sits inside the printer
// it watches. So this is the device's own access plus what its parents confer,
// and inheritFrom adds the second half.
func (g *graph) deviceAccess(device directoryRow) map[principal]access {
	out, _ := g.reachDevice(device, map[string]bool{})

	return out
}

// reachDevice is deviceAccess with the chain it is already walking, so a parent
// cycle stops at the device it started from. A cycle is a configuration error.
// This compiler runs inside the transaction every grant write takes, so an
// unbounded walk here would stop the whole server and not one request. It
// reports whether the answer was cut short by that guard.
//
// Cost is one local resolve per device, per compile. Nothing bounds how many
// parents a device may name: anybody holding administer on the child may write
// one tuple per pair. The walk is over paths and not over devices, so layers
// of two devices that each name both devices above them double the work per
// layer. Without the memo, forty devices arranged that way take seconds, on a
// walk that runs on every start and inside every grant write.
//
// The memo is keyed by device and skipped for a truncated answer. A result
// that stopped at the visiting set is the answer for that chain and would be
// wrong for a device outside the cycle.
func (g *graph) reachDevice(device directoryRow, visiting map[string]bool) (map[principal]access, bool) {
	if visiting[device.id] {
		return nil, true
	}

	if cached, ok := g.reached[device.id]; ok {
		return cached, false
	}

	visiting[device.id] = true
	defer delete(visiting, device.id)

	out, denied := g.localDeviceAccess(device)

	var truncated bool

	for _, parent := range g.deviceParent[device.id] {
		// The parent's kind comes from the directory and not from the edge. A
		// verb means what the adapter of that machine declared it to mean,
		// whatever watches it.
		above, cut := g.reachDevice(g.deviceRow(parent), visiting)
		truncated = truncated || cut

		g.inheritFrom(out, denied, above)
	}

	if !truncated {
		g.reached[device.id] = out
	}

	return out, truncated
}

// deviceRow is one directory row the compiler already holds, or the id alone
// for a device the directory does not carry. A device absent from the directory
// projects no row of its own, and its kind is empty, so a verb grant written
// on it confers nothing. A grant on a device the server does not know about
// fails closed, as project says for a tuple.
func (g *graph) deviceRow(id string) directoryRow {
	return directoryRow{id: id, kind: g.verbs.kinds[id]}
}

// inheritFrom adds what a parent device confers to what is written on the child.
//
// What flows down is the mask the parent resolved. A caller who may watch a
// printer may watch its camera, whichever of the parent's own paths gave them
// that. A second definition of what reaching a printer means would drift from
// the first.
//
// It arrives as something other than writtenHere, so the camera's own manager
// may not write it back. A group grant is under the same rule.
//
// A block written on the child cuts it. That is how one camera is hidden from
// somebody who may see the printer.
//
// The parent's own split is carried and not flattened. What an org or a group
// confers on the parent is tenancy one hop further down and stays inherited on
// the child. What is written on the parent, and what the parent's own parents
// confer, is fromParent. With a single mask for both, a delegate written
// manager on a printer and the administrator of that printer's org would be
// identical on the printer's camera, where neither holds anything else. The
// source split exists to stop that escalation.
//
// Ownership is in neither inherited nor fromParent. ownerMask is not standing:
// it is an addition to the role layer, written on a device and removable by
// whoever administers that device. Carrying it in held alone keeps it that way
// one hop down, so a manager of the camera can withdraw it as a manager of the
// printer can.
//
// A device credential is skipped. A device reaches the device it is the agent
// of and no other, and a credential can hold nothing else on a device, so
// skipping the type is skipping the agent grant.
func (g *graph) inheritFrom(out map[principal]access, denied map[principal]bool, parent map[principal]access) {
	for who, held := range parent {
		if !held.reaches() || denied[who] || who.typ == TypeDeviceCredential {
			continue
		}

		row := out[who]
		row.held |= held.held
		row.inherited |= held.inherited
		row.fromParent |= held.fromParent | held.writtenHere
		out[who] = row
	}
}

// localDeviceAccess is what is written on one device and above it in the group
// and org trees, with the principals its own block denies. It is deviceAccess
// without the parent chain, which is the part reachDevice recurses over.
func (g *graph) localDeviceAccess(device directoryRow) (map[principal]access, map[principal]bool) {
	levels := make(map[principal]standing)

	for _, role := range directRoles {
		g.grant(levels, roleKey{Object: TypeDevice, ID: device.id, Relation: role.Relation},
			role.Level, writtenHere)
	}

	g.grantFromGroups(levels, device.id)

	for _, org := range g.orgsOf(device.id) {
		for _, role := range orgRoles {
			g.grant(levels, roleKey{Object: TypeOrg, ID: org, Relation: role.Relation},
				role.Level, writtenAbove)
		}
	}

	// One deny at the role layer, so it covers every action and cannot be
	// forgotten when an action is added. A device credential is not denied
	// here: it holds no role, and a compromised one is revoked by deleting its
	// agent tuple.
	//
	// The denied set is kept as well as applied. Ownership is added after the
	// levels are resolved, and a deny that reached the role layer alone would
	// leave a blocked owner still commanding the machine.
	denied := make(map[principal]bool)

	for _, subject := range g.blocked[device.id] {
		g.expand(subject, func(who principal) {
			denied[who] = true

			delete(levels, who)
		})
	}

	out := make(map[principal]access, len(levels)+len(g.agents[device.id]))
	for who, reached := range levels {
		out[who] = access{
			held:        reached.top().deviceMask(),
			writtenHere: reached.here.deviceMask(),
			inherited:   reached.above.deviceMask(),
		}
	}

	// Ownership adds to whatever role the subject reached, and is not a level
	// of its own. An owner holding operator on their own machine reaches
	// configure, and an operator who owns nothing does not. The tuple is
	// written on the device, so it inherits nothing and the device's manager
	// may withdraw it.
	for _, subject := range g.owners[device.id] {
		g.expand(subject, func(who principal) {
			if denied[who] {
				return
			}

			held := out[who]
			held.held |= ownerMask
			out[who] = held
		})
	}

	// A history provider reads audit beside what its role gave it. It is
	// granted one org#viewer tuple at first sight, and viewer confers observe
	// alone, so without this the audit filter would pass every provider
	// nothing, with no error. It is added to a provider that already reaches
	// the device and never on its own: a provider with no grant still reads
	// nothing.
	for who, held := range out {
		if who.typ != TypeHistory || held.held&maskObserve == 0 || denied[who] {
			continue
		}

		held.held |= historyMask
		out[who] = held
	}

	// A device credential reaches the device it is the agent of and no other,
	// and reaches it through no role. Its tuple is written on the device, so
	// it inherits nothing and the device's manager may withdraw it.
	for _, credential := range g.agents[device.id] {
		who := principal{typ: TypeDeviceCredential, id: credential}
		held := out[who]
		held.held |= agentMask
		out[who] = held
	}

	return out, denied
}

// verbRowsFor resolves the dangerous tier for one device: who reaches which
// verb on it, expanded to principals the way the mask projection is expanded.
//
// It has two sources, and both are per device. A row somebody wrote names the
// subject, the device and the verb. Ownership implies one row per dangerous
// verb of the device's own kind, computed and not stored. That is how an owner
// reaches raw gcode and the irreversible verbs on their own machine without
// any of them occupying a bit.
//
// It does not descend a parent edge. A verb belongs to a kind, and the camera
// inside a printer has its own kind, so a printer's dangerous verb reaching
// its camera would name a verb the camera does not have.
//
// It drops a verb the device's kind no longer declares. A rebuild that removed
// a verb leaves its rows in place and without effect. An operator still sees
// them, and deletes them or waits for the verb to come back.
//
// A block cuts both sources, for the reason a block cuts the role layer: a deny
// that reached some grants and not others would be forgotten for the rest.
func (g *graph) verbRowsFor(device directoryRow) []verbRow {
	_, denied := g.localDeviceAccess(device)

	kind := DeviceKind(device.kind)
	held := map[principal]map[string]bool{}

	add := func(who principal, verb string) {
		if denied[who] {
			return
		}

		if !Dangerous(kind, verb) {
			return
		}

		if held[who] == nil {
			held[who] = map[string]bool{}
		}

		held[who][verb] = true
	}

	for _, grant := range g.verbs.grants[device.id] {
		g.expand(grant.subject, func(who principal) { add(who, grant.verb) })
	}

	for _, subject := range g.owners[device.id] {
		g.expand(subject, func(who principal) {
			for _, verb := range DangerousVerbsOf(kind) {
				add(who, verb)
			}
		})
	}

	out := make([]verbRow, 0, len(held))

	for who, verbs := range held {
		for _, verb := range sortedVerbs(verbs) {
			out = append(out, verbRow{Subject: who, Device: device.id, Verb: verb})
		}
	}

	slices.SortFunc(out, func(a, b verbRow) int {
		if c := comparePrincipals(a.Subject, b.Subject); c != 0 {
			return c
		}

		return cmp.Compare(a.Verb, b.Verb)
	})

	return out
}

// resourceRowsFor resolves one resource kind: the highest role each principal
// reaches on it, minus the blocks. There is no hierarchy above a kind and no
// agent below it. A kind is a flat, closed, node-local object, so it needs
// neither the group chain nor a directory.
func (g *graph) resourceRowsFor(kind string) []resourceAccessRow {
	reached := g.kindAccess(kind)

	out := make([]resourceAccessRow, 0, len(reached))

	for who, held := range reached {
		if !held.reaches() {
			continue
		}

		out = append(out, resourceAccessRow{Subject: who, Kind: kind, Actions: held.held})
	}

	return out
}

// kindAccess is the resource half of deviceAccess.
func (g *graph) kindAccess(kind string) map[principal]access {
	levels := make(map[principal]standing)

	for _, role := range directRoles {
		g.grant(levels, roleKey{Object: TypeResourceKind, ID: kind, Relation: role.Relation},
			role.Level, writtenHere)
	}

	for _, org := range g.kindOrg[kind] {
		for _, role := range orgRoles {
			g.grant(levels, roleKey{Object: TypeOrg, ID: org, Relation: role.Relation},
				role.Level, writtenAbove)
		}
	}

	// The same one deny at the role layer as a device carries, so an action
	// added to the resource surface cannot be the one somebody forgot.
	for _, subject := range g.kindBlocked[kind] {
		g.expand(subject, func(who principal) { delete(levels, who) })
	}

	out := make(map[principal]access, len(levels))
	for who, reached := range levels {
		out[who] = access{
			held:        reached.top().resourceMask(),
			writtenHere: reached.here.resourceMask(),
			inherited:   reached.above.resourceMask(),
		}
	}

	return out
}

// accessOn is the compiler asked about one object and not the whole fleet.
// It answers nothing for a type the projection does not carry, which is every
// type no action is defined on.
func (g *graph) accessOn(object Object) map[principal]access {
	switch object.typ {
	case TypeDevice:
		return g.deviceAccess(g.deviceRow(object.id))
	case TypeResourceKind:
		return g.kindAccess(object.id)
	case TypeUser, TypeGroup, TypeServiceAccount, TypeDeviceCredential, TypeNode, TypeSink, TypeHistory,
		TypeOrg, TypeDeviceGroup:
		return nil
	default:
		return nil
	}
}

// orgsOf returns the orgs whose global roles reach this device: the one it
// names itself, and the one named by any device group above it. Tenancy
// written on a group is tenancy, so an org admin reaches the devices inside it
// without a second tuple per device.
func (g *graph) orgsOf(device string) []string {
	seen := make(map[string]bool)

	var out []string

	appendOrg := func(org string) {
		if seen[org] {
			return
		}

		seen[org] = true

		out = append(out, org)
	}

	for _, org := range g.deviceOrg[device] {
		appendOrg(org)
	}

	for _, group := range g.deviceGroup[device] {
		for _, ancestor := range g.groupChain(group) {
			for _, org := range g.groupOrg[ancestor] {
				appendOrg(org)
			}
		}
	}

	return out
}

// grant raises every principal named by that relation to at least lvl, in the
// half of their standing this grant is written in.
func (g *graph) grant(levels map[principal]standing, key roleKey, lvl level, from origin) {
	for _, subject := range g.roles[key] {
		g.expand(subject, func(who principal) {
			levels[who] = levels[who].raise(lvl, from)
		})
	}
}

// grantFromGroups raises every principal holding a role on any group above this
// device. Group roles are read off the device's own group chain, so this is
// also where the chain is walked.
func (g *graph) grantFromGroups(levels map[principal]standing, device string) {
	for _, group := range g.deviceGroup[device] {
		for _, ancestor := range g.groupChain(group) {
			for _, role := range directRoles {
				g.grant(levels, roleKey{Object: TypeDeviceGroup, ID: ancestor, Relation: role.Relation},
					role.Level, writtenAbove)
			}
		}
	}
}

// expand resolves a subject to principals and calls visit for each. The only
// userset the vocabulary has is group#member; any other shape resolves to
// nobody rather than to whatever else shares its id, so a row that reached the
// table without passing NewTuple cannot borrow a group's membership.
func (g *graph) expand(subject Subject, visit func(principal)) {
	if !subject.IsUserset() {
		visit(principal{typ: subject.typ, id: subject.id})

		return
	}

	if subject.typ != TypeGroup || subject.rel != RelMember {
		return
	}

	for who := range g.membership()[principal{typ: subject.typ, id: subject.id}] {
		visit(who)
	}
}

// membership is the transitive membership of every group, computed once by
// fixpoint. A membership cycle is a configuration error; iterating to a fixed
// point converges on one instead of recursing into it.
func (g *graph) membership() map[principal]map[principal]bool {
	if g.expanded != nil {
		return g.expanded
	}

	closure := make(map[principal]map[principal]bool, len(g.groupMembers))
	for group := range g.groupMembers {
		closure[group] = make(map[principal]bool)
	}

	for changed := true; changed; {
		changed = false

		for group, subjects := range g.groupMembers {
			for _, subject := range subjects {
				if reach(closure[group], closure, subject) {
					changed = true
				}
			}
		}
	}

	g.expanded = closure

	return closure
}

// groupChain returns a device group and every ancestor above it, so a grant on
// a floor is inherited by the cells inside it.
func (g *graph) groupChain(group string) []string {
	if cached, ok := g.ancestors[group]; ok {
		return cached
	}

	seen := map[string]bool{}
	queue := []string{group}

	var out []string

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		if seen[current] {
			continue
		}

		seen[current] = true

		out = append(out, current)
		queue = append(queue, g.groupParent[current]...)
	}

	g.ancestors[group] = out

	return out
}

// reach adds what subject contributes to set, reporting whether anything was
// new. A userset that is not group#member contributes nobody, for the reason
// expand gives.
func reach(set map[principal]bool, closure map[principal]map[principal]bool, subject Subject) bool {
	if !subject.IsUserset() {
		who := principal{typ: subject.typ, id: subject.id}
		if set[who] {
			return false
		}

		set[who] = true

		return true
	}

	if subject.typ != TypeGroup || subject.rel != RelMember {
		return false
	}

	changed := false

	for who := range closure[principal{typ: subject.typ, id: subject.id}] {
		if set[who] {
			continue
		}

		set[who] = true
		changed = true
	}

	return changed
}
