package authz

// Type is the closed set of types a tuple names, on either side. A new type is
// a change to this file. The authorization graph is never pluggable
// (AGENTS.md).
//
// The relation vocabulary and the class set below stay closed, because placing
// a name into the lattice decides who can escalate. The verbs a device accepts
// are open: a driver declares its own verbs and each carries a class from the
// closed set. The verb table is in verbtable.go and it is not the graph.
type Type string

// The types. Principals hold grants; org, device_group and device are the
// objects grants are written on. group and device_group appear on both sides.
const (
	TypeUser           Type = "user"
	TypeGroup          Type = "group"
	TypeServiceAccount Type = "service_account"
	// TypeDeviceCredential is what a device authenticates as, and not the
	// device. The device is TypeDevice, which is the object a grant is
	// written on. gosec reads this name as a secret. It names a type and holds
	// nothing.
	TypeDeviceCredential Type = "device_credential" //nolint:gosec // a type name, not a credential
	TypeNode             Type = "node"
	// TypeSink is one configured event sink, as a holder of grants. It is the
	// instance an operator declared under `sink:` and never the kind. The kind
	// is TypeResourceKind, which is the configuration surface. This is the
	// principal the running instance reads events as.
	//
	// It is separate from TypeServiceAccount. With one type for both,
	// `sink: {audit: ...}` and `service_account:audit` would be one principal
	// holding one set of grants. The sink would read the fleet with whatever
	// that service account was granted, and a service-account credential of
	// that name would hold every grant written to feed the sink.
	//
	// Nothing can present it. internal/app's graphPrincipals stamps
	// authz.TypeUser onto every id a credential resolves to, so the type is
	// reachable only from the composition root, which builds it from the
	// instance the resource host is supervising.
	TypeSink Type = "sink"
	// TypeHistory is one configured history provider, as a holder of grants.
	// It is the instance an operator declared under `history:` and never the
	// kind. It is separate from TypeSink for the reason TypeSink is separate
	// from TypeServiceAccount: one type for both would make a sink and a
	// history store of the same name one principal holding one set of grants.
	//
	// A provider with no grant stores nothing and reports no error. This type
	// and the org-viewer tuple written for it prevent that.
	TypeHistory     Type = "history"
	TypeOrg         Type = "org"
	TypeDeviceGroup Type = "device_group"
	TypeDevice      Type = "device"
	// TypeResourceKind is the configuration surface of one resource kind, and
	// its ids are exactly ref.ConfigurableKinds. A grant here governs every
	// instance of that kind, because a resource instance is a line of this
	// server's configuration and not a device somebody is given access to.
	TypeResourceKind Type = "resource_kind"
)

// Relation is the closed set of edge names.
type Relation string

// The relations. viewer, operator and manager are the role layer; parent, org,
// group and agent are structure; blocked is the deny that covers every action;
// owner says whose machine it is and adds to whatever role the subject holds.
const (
	RelMember   Relation = "member"
	RelParent   Relation = "parent"
	RelOrg      Relation = "org"
	RelGroup    Relation = "group"
	RelAgent    Relation = "agent"
	RelBlocked  Relation = "blocked"
	RelViewer   Relation = "viewer"
	RelOperator Relation = "operator"
	RelManager  Relation = "manager"
	// RelAdmin exists only on org. An org admin reaches every device in the
	// org at manager level, which is the RBAC half of the model.
	RelAdmin Relation = "admin"
	// RelOwner is whose machine this is. It is not a role and carries no level.
	// It adds ownerMask to whatever the role layer already gives its subject.
	// An owner who holds operator on their own device reaches configure, and
	// an operator who owns nothing does not.
	RelOwner Relation = "owner"
)

// Action is what a caller performs. Handlers check an action and never a role,
// so the role layer can change without touching call sites. Every action
// belongs to exactly one object type, which domain reports.
//
// A driver declares its verbs, and each verb carries exactly one class:
// operate or configure. The class set is closed, because placing a name into
// the lattice decides who can escalate, and the core makes that decision. The
// verb table is open, and the verb table is not the graph.
type Action string

// The device actions. report_telemetry and fetch_job are reachable only from
// the agent relation, so a device credential cannot become an operator
// credential.
const (
	// ActionObserve is reading telemetry and state, and subscribing to them. It
	// sits beside the classes and no verb may carry it.
	//
	// Answering even a pure read puts a command on a wire that carries one
	// command at a time. The verb table classes send_info as a write for that
	// reason. So every verb is a write to the transport, whatever it does to
	// the device, and observation is a different surface with a different
	// object. No verb can carry observe, so a destructive verb cannot be
	// classed as a read. The floor for any verb is operate, where stop_print
	// already sits.
	ActionObserve Action = "observe"
	// ActionOperate is acting on the machine in its normal duty. It is the
	// floor a verb may carry.
	ActionOperate Action = "operate"
	// ActionConfigure changes what the machine is, and not the work on it:
	// the firmware it runs, the token it reports under, the name it answers to.
	ActionConfigure Action = "configure"
	// ActionAdminister administers the grants on the object. It sits beside
	// the classes, and no verb may carry it: a declaration that could hand out
	// administration would be an adapter writing itself into the lattice.
	ActionAdminister Action = "administer"
	// ActionViewAudit is reading audit-class events: who authorized which
	// command on this device.
	//
	// It is conferred at manager and by nothing lower. An operator is a device
	// viewer, so conferring audit sight at operator would let every device
	// viewer learn which operator sent which command. The `authorized` frame
	// is gated on this action.
	ActionViewAudit Action = "view_audit"

	ActionReportTelemetry Action = "report_telemetry"
	ActionFetchJob        Action = "fetch_job"
)

// The two raw gcode verbs, named so RawGCodeVerbs and the generated half cannot
// spell them differently. They are written out here and not imported from
// sdk/connectorapi, because this package is the authorization graph and
// imports nothing of the printer plane.
// TestTheRawGcodeVerbNamesMatchTheDriver holds the two copies together.
const (
	verbSendGCode  = "core.command.send_gcode"
	verbForceGCode = "core.command.force_gcode"
)

// The resource actions, performed on a resource kind. They are disjoint from
// the device actions: a fleet-wide device grant is not permission to rewrite
// the server's storage configuration. The two masks share no bit, so neither
// can be reached by asking the wrong table.
const (
	ActionViewResource      Action = "view_resource"
	ActionOperateResource   Action = "operate_resource"
	ActionConfigureResource Action = "configure_resource"
)

// The storage contents actions, performed on a resource kind like the resource
// actions above and separate from them. The resource actions are about an
// instance's declaration: what it is, where it points, whether it is running.
// These are about what is inside one. Seeing that a storage instance exists is
// not reading the files in it. Being allowed to drop a file into one is not
// being allowed to repoint it at another directory or delete it. The
// declaration actions cannot answer either question without handing out far
// more than was asked for.
const (
	ActionReadStorage  Action = "read_storage"
	ActionWriteStorage Action = "write_storage"
)

// ActionAdmitDevice is deciding which devices are let onto this server: the
// listing of what is waiting, and the approval or rejection of one.
//
// It is performed on the driver resource kind, because a registration table
// belongs to a connector and a connector is an instance of that kind. It is
// separate from configure_resource because the two are different permissions.
// Rewriting a connector's settings block changes what this server dials.
// Admitting a machine decides who joins the fleet. A site that wants a
// technician who can plug machines in without being able to repoint the
// connector needs them apart.
//
// It is conferred at manager on the resource role layer, so an org admin holds
// it from the first start.
const ActionAdmitDevice Action = "admit_device"

// ActionDevelopDevice is reaching the developer surface: the page at /dev, the
// connector and route table this build carries, and the log of commands this
// process sent to one device.
//
// It is performed on the driver resource kind, for the reason admit_device is.
// A connector is an instance of that kind, and the command log is what this
// server sent through one. There is no device to name: the connector table
// belongs to the build and not to any machine.
//
// It is separate from view_resource, which a reader of a connector's
// declaration holds. Reading that a connector is declared is part of seeing
// this server's configuration. The developer surface says which dialects were
// compiled in, which paths this origin answers that the API document does not
// publish, and what was sent to a machine and what came back.
//
// It is conferred at manager on the resource role layer, so an org admin holds
// it from the first start.
const ActionDevelopDevice Action = "develop_device"

// actionSet is the bitmask a projected row carries. Both enforcement surfaces
// test it with the same predicate, so neither can drift from the other.
type actionSet uint32

const (
	maskObserve actionSet = 1 << iota
	maskOperate
	maskConfigure
	maskAdminister
	maskViewAudit
	maskReportTelemetry
	maskFetchJob
	maskViewResource
	maskOperateResource
	maskConfigureResource
	maskReadStorage
	maskWriteStorage
	maskAdmitDevice
	maskDevelopDevice
	// A new bit is appended and never inserted. A projected row stores this set
	// as one integer, so a bit that moves reinterprets every row already
	// written as a set of actions nobody granted.
)

// agentMask is what a device's own credential reaches on that device. It
// shares no bit with operate or administer.
const agentMask = maskObserve | maskReportTelemetry | maskFetchJob

// historyMask is what a history provider reaches on a device beyond the role it
// was granted: audit sight.
//
// A provider must read audit-class events without holding administer on
// anything. The bootstrap writes it one org#viewer tuple. The tuple is on the
// org, so it covers every device this server has and every one it admits
// later. Viewer confers observe alone, so without this mask the audit filter
// would pass a provider that consumes the audit class nothing, with no error,
// and the gap would be found only when somebody needs the rows.
//
// It is added to what the role layer gave the provider, as the agent relation
// adds its mask. A provider is not a manager of anything.
const historyMask = maskViewAudit

// ownerMask is what owning a device reaches on it: every class a caller may
// hold on a machine, short of administering its grants.
//
// It stops short of administer. Whoever administers the device says who owns
// it, and the owner cannot hand ownership on. It stops short of view_audit for
// a different reason. Audit sight is a read surface and not a class a command
// carries. It stays at manager, so that holding a machine does not make
// somebody the reader of who else touched it.
//
// The dangerous tier is not in it, because the tier is not a mask. Ownership
// reaches every dangerous verb of the owned device's kind. That is computed
// into the verb projection and not stored, so raw gcode and the irreversible
// verbs reach an owner without occupying a bit here.
//
// It occupies no bit of its own. Every action it names is already assigned.
const ownerMask = maskObserve | maskOperate | maskConfigure

// level orders the role layer. It exists only inside the projection; nothing
// outside this package names a role.
type level uint8

const (
	levelNone level = iota
	levelViewer
	levelOperator
	levelManager
)

// subjectShape is one permitted subject for a relation. An empty Relation means
// a principal named directly, a set Relation means a userset (Zanzibar §2.1).
type subjectShape struct {
	Type     Type
	Relation Relation
}

// relationSpec declares one relation of the vocabulary and every subject shape
// it accepts. It is a slice rather than a map literal so the whole vocabulary
// reads in one place, grouped by the object the relation is written on.
type relationSpec struct {
	Object   Type
	Relation Relation
	Subjects []subjectShape
}

// grantable is every principal shape a role or a block may name on a device or
// a device group. Service accounts and nodes are here and deliberately absent
// from org roles and group membership: neither can become fleet-wide without an
// explicit grant per device or per group.
var grantable = []subjectShape{
	{Type: TypeUser},
	{Type: TypeGroup, Relation: RelMember},
	{Type: TypeServiceAccount},
	{Type: TypeNode},
	{Type: TypeSink},
	{Type: TypeHistory},
}

// orgWatchers is what org#viewer accepts: everyone members covers, a sink and a
// history provider. Viewer is the only org role those two may hold. The other
// org roles accept members.
//
// A sink is here and a service account is not. A service account is kept out
// because a credential must not become fleet-wide by accident: a credential is
// presented at the door, can be copied, and outlives whoever issued it. A sink
// is presented nowhere. Nothing issues one, internal/app's graphPrincipals
// keys every authenticated caller under user, and the principal exists only
// while the resource host is supervising the instance an operator declared.
// Writing org:local#viewer@sink:audit is an administrator saying the sink
// watches the fleet. It is the only tuple that covers a device admitted later,
// because internal/app's register writes device#org for every device it admits
// and places it in no group.
//
// It stops at viewer. A sink reads: the hub asks observe of it and no other
// action. So org:local#admin@sink:audit would confer nothing a sink uses, and
// would stand in the graph saying this webhook may reflash every machine in
// the building. The vocabulary refuses that tuple.
var orgWatchers = []subjectShape{
	{Type: TypeUser},
	{Type: TypeGroup, Relation: RelMember},
	{Type: TypeSink},
	// A history provider, for the same reason and with the same stop at
	// viewer. One tuple on the org covers a device admitted later. Grants
	// written by hand per device miss the next device, and the provider then
	// stores no audit records for it.
	{Type: TypeHistory},
}

// members is what may hold a group membership. A service account is not a
// person and never inherits a team's grants.
var members = []subjectShape{
	{Type: TypeUser},
	{Type: TypeGroup, Relation: RelMember},
}

// relationSpecs is the vocabulary. Reconcile projects it into
// authz_relation_shape, which authz_tuple's foreign key points at, so this is
// the one copy the write path and the database are both held to.
var relationSpecs = []relationSpec{
	{Object: TypeGroup, Relation: RelMember, Subjects: members},

	{Object: TypeOrg, Relation: RelAdmin, Subjects: members},
	{Object: TypeOrg, Relation: RelOperator, Subjects: members},
	{Object: TypeOrg, Relation: RelViewer, Subjects: orgWatchers},

	{Object: TypeDeviceGroup, Relation: RelParent, Subjects: []subjectShape{{Type: TypeDeviceGroup}}},
	{Object: TypeDeviceGroup, Relation: RelOrg, Subjects: []subjectShape{{Type: TypeOrg}}},
	{Object: TypeDeviceGroup, Relation: RelManager, Subjects: grantable},
	{Object: TypeDeviceGroup, Relation: RelOperator, Subjects: grantable},
	{Object: TypeDeviceGroup, Relation: RelViewer, Subjects: grantable},

	// A device inside another device: a camera and the printer it watches.
	// It is the same relation device_group#parent carries and it works the
	// same way, so a camera is reached by whoever may reach its printer
	// without a tuple per viewer.
	{Object: TypeDevice, Relation: RelParent, Subjects: []subjectShape{{Type: TypeDevice}}},
	{Object: TypeDevice, Relation: RelOrg, Subjects: []subjectShape{{Type: TypeOrg}}},
	{Object: TypeDevice, Relation: RelGroup, Subjects: []subjectShape{{Type: TypeDeviceGroup}}},
	{Object: TypeDevice, Relation: RelAgent, Subjects: []subjectShape{{Type: TypeDeviceCredential}}},
	{Object: TypeDevice, Relation: RelBlocked, Subjects: grantable},
	{Object: TypeDevice, Relation: RelManager, Subjects: grantable},
	{Object: TypeDevice, Relation: RelOperator, Subjects: grantable},
	{Object: TypeDevice, Relation: RelViewer, Subjects: grantable},
	{Object: TypeDevice, Relation: RelOwner, Subjects: grantable},

	{Object: TypeResourceKind, Relation: RelOrg, Subjects: []subjectShape{{Type: TypeOrg}}},
	{Object: TypeResourceKind, Relation: RelBlocked, Subjects: grantable},
	{Object: TypeResourceKind, Relation: RelManager, Subjects: grantable},
	{Object: TypeResourceKind, Relation: RelOperator, Subjects: grantable},
	{Object: TypeResourceKind, Relation: RelViewer, Subjects: grantable},
}

// relations indexes relationSpecs for the write path.
var relations = indexRelations()

// relationShape is one permitted edge, as the four columns authz_relation_shape
// holds and authz_tuple's composite foreign key names. An empty SubjectRelation
// is a principal, spelled the same way here as it is in a tuple row, so one key
// covers principals and usersets with no special case.
type relationShape struct {
	ObjectType      Type
	Relation        Relation
	SubjectType     Type
	SubjectRelation Relation
}

// expandShapes projects relation specs into the rows the database holds the
// tuple table to. relationSpecs lists permitted shapes and not names, so the
// constraint is composite: a legal name in an illegal position is the
// fail-open case.
func expandShapes(specs []relationSpec) []relationShape {
	out := make([]relationShape, 0, len(specs)*len(grantable))

	for _, spec := range specs {
		for _, subject := range spec.Subjects {
			out = append(out, relationShape{
				ObjectType:      spec.Object,
				Relation:        spec.Relation,
				SubjectType:     subject.Type,
				SubjectRelation: subject.Relation,
			})
		}
	}

	return out
}

// String renders the shape the way a refusal spells it.
func (s relationShape) String() string {
	return s.ObjectType.String() + relationSeparator + s.Relation.String() +
		"@" + s.SubjectType.String() + relationSeparator + s.SubjectRelation.String()
}

// args is the shape as query parameters, in the column order of the composite
// key. One function so the insert, the delete and the count cannot disagree
// about which column comes second.
func (s relationShape) args() []any {
	return []any{
		s.ObjectType.String(), s.Relation.String(),
		s.SubjectType.String(), s.SubjectRelation.String(),
	}
}

// Types returns the closed set, in declaration order.
func Types() []Type {
	return []Type{
		TypeUser, TypeGroup, TypeServiceAccount, TypeDeviceCredential,
		TypeNode, TypeSink, TypeHistory, TypeOrg, TypeDeviceGroup, TypeDevice, TypeResourceKind,
	}
}

// Actions returns the closed set, in declaration order.
func Actions() []Action {
	return []Action{
		ActionObserve, ActionOperate, ActionConfigure, ActionAdminister, ActionViewAudit,
		ActionReportTelemetry, ActionFetchJob,
		ActionViewResource, ActionOperateResource, ActionConfigureResource,
		ActionReadStorage, ActionWriteStorage, ActionAdmitDevice, ActionDevelopDevice,
	}
}

// Valid reports whether t is a member of the closed set.
func (t Type) Valid() bool {
	switch t {
	case TypeUser, TypeGroup, TypeServiceAccount, TypeDeviceCredential,
		TypeNode, TypeSink, TypeHistory, TypeOrg, TypeDeviceGroup, TypeDevice, TypeResourceKind:
		return true
	default:
		return false
	}
}

// String returns the type as it is written in a tuple and stored in a column.
func (t Type) String() string { return string(t) }

// String returns the relation as it is written in a tuple.
func (r Relation) String() string { return string(r) }

// Valid reports whether a is a member of the closed set.
func (a Action) Valid() bool { return a.mask() != 0 }

// String returns the action as it is written in a policy and a log line.
func (a Action) String() string { return string(a) }

// ActionNames renders an action set for the wire, and renders the empty set as
// an empty array rather than as null. A client that iterates the member without
// a null check is then correct on every object.
func ActionNames(held []Action) []string {
	out := make([]string, 0, len(held))
	for _, action := range held {
		out = append(out, action.String())
	}

	return out
}

// mask returns the bit this action occupies in a projected row, zero for an
// action outside the closed set.
func (a Action) mask() actionSet {
	switch a {
	case ActionObserve:
		return maskObserve
	case ActionOperate:
		return maskOperate
	case ActionConfigure:
		return maskConfigure
	case ActionAdminister:
		return maskAdminister
	case ActionViewAudit:
		return maskViewAudit
	case ActionReportTelemetry:
		return maskReportTelemetry
	case ActionFetchJob:
		return maskFetchJob
	case ActionViewResource:
		return maskViewResource
	case ActionOperateResource:
		return maskOperateResource
	case ActionConfigureResource:
		return maskConfigureResource
	case ActionReadStorage:
		return maskReadStorage
	case ActionWriteStorage:
		return maskWriteStorage
	case ActionAdmitDevice:
		return maskAdmitDevice
	case ActionDevelopDevice:
		return maskDevelopDevice
	default:
		return 0
	}
}

// domain returns the object type this action is defined on, reporting false for
// an action outside the closed set. With it, a device action checked on a
// resource object is refused, where a plain lookup would only miss.
func (a Action) domain() (Type, bool) {
	switch a {
	case ActionObserve, ActionOperate, ActionConfigure, ActionAdminister, ActionViewAudit,
		ActionReportTelemetry, ActionFetchJob:
		return TypeDevice, true
	case ActionViewResource, ActionOperateResource, ActionConfigureResource,
		ActionReadStorage, ActionWriteStorage, ActionAdmitDevice, ActionDevelopDevice:
		return TypeResourceKind, true
	default:
		return "", false
	}
}

// deviceMask returns what a role reaches on a device. Each level contains the
// one below it.
func (l level) deviceMask() actionSet {
	switch l {
	case levelViewer:
		return maskObserve
	case levelOperator:
		return levelViewer.deviceMask() | maskOperate
	case levelManager:
		// configure enters here and not at operator. It changes the machine
		// and not the job on it, and an operator is somebody who runs prints
		// on a fleet they do not own.
		//
		// view_audit enters here and nowhere lower. An operator is a device
		// viewer, so conferring audit sight at operator would hand every
		// device viewer the record of which operator sent which command.
		//
		// No level confers the dangerous tier. A dangerous verb is not a bit,
		// so there is nothing for a level to carry, and reaching one takes a
		// row naming the verb.
		return levelOperator.deviceMask() | maskConfigure | maskAdminister | maskViewAudit
	case levelNone:
		return 0
	default:
		return 0
	}
}

// resourceMask returns what a role reaches on a resource kind. The same levels
// carry it, so an operator who may restart a failed sink is the same grant
// shape as an operator who may cancel a job, and only a manager rewrites the
// settings block a connection string lives in.
//
// The contents actions enter below the manager level. A viewer of the kind is
// already trusted to read its declaration, and putting a file there is
// day-to-day work. Requiring manager for either would push a deployment into
// handing out manager, which widens the settings surface far more than the
// file surface.
func (l level) resourceMask() actionSet {
	switch l {
	case levelViewer:
		return maskViewResource | maskReadStorage
	case levelOperator:
		return levelViewer.resourceMask() | maskOperateResource | maskWriteStorage
	case levelManager:
		// admit_device enters at manager beside configure_resource. Deciding
		// who joins the fleet is an administrator's act. The action is
		// separate from configure_resource so that a deployment can hand it
		// out without handing out the settings block.
		//
		// develop_device enters here on the same terms. The developer surface
		// reports what this build carries and what it sent to a machine. The
		// action is separate so a site can hand somebody that surface without
		// the settings block.
		return levelOperator.resourceMask() | maskConfigureResource | maskAdmitDevice | maskDevelopDevice
	case levelNone:
		return 0
	default:
		return 0
	}
}

// roleLevel maps a role relation to its level, reporting false for a relation
// that is structure rather than a role.
func roleLevel(r Relation) (level, bool) {
	switch r {
	case RelViewer:
		return levelViewer, true
	case RelOperator:
		return levelOperator, true
	case RelManager, RelAdmin:
		return levelManager, true
	case RelMember, RelParent, RelOrg, RelGroup, RelAgent, RelBlocked, RelOwner:
		return levelNone, false
	default:
		return levelNone, false
	}
}

func indexRelations() map[Type]map[Relation][]subjectShape {
	out := make(map[Type]map[Relation][]subjectShape, len(relationSpecs))
	for _, spec := range relationSpecs {
		byRelation, ok := out[spec.Object]
		if !ok {
			byRelation = make(map[Relation][]subjectShape)
			out[spec.Object] = byRelation
		}

		byRelation[spec.Relation] = spec.Subjects
	}

	return out
}

// lookupRelation returns the subject shapes a relation accepts on an object of
// this type, reporting false for a pair outside the vocabulary.
func lookupRelation(object Type, relation Relation) ([]subjectShape, bool) {
	byRelation, ok := relations[object]
	if !ok {
		return nil, false
	}

	shapes, ok := byRelation[relation]

	return shapes, ok
}
