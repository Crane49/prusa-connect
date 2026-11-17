package authz

import "github.com/prusa3d/prusa-connect/sdk/vocabulary"

// What a published authorization reference declares about itself. The content
// version moves when an action, a type, a relation or a permitted shape
// changes. The format version moves when the shape of an entry changes, so a
// consumer can tell a reshaped artefact from a reworded one.
const (
	VocabularyName = "authorization"
	// A retired entry takes a major version. An entry added with none retired
	// takes a minor. A name leaves a released artefact only after a
	// deprecation cycle.
	VocabularyVersion = "4.2.0"
	VocabularyFormat  = "1.0"
)

// ActionDoc is one action as a reader of the published reference sees it. The
// prose is declared; Domain and ConferredBy are derived from the code that
// enforces them, so a published claim about who reaches an action cannot drift
// from the check that decides it.
type ActionDoc struct {
	Action      Action     `json:"action"`
	Domain      Type       `json:"domain"`
	Summary     string     `json:"summary"`
	Description string     `json:"description"`
	ConferredBy []Relation `json:"conferred_by"`
	Version     string     `json:"version"`
	// Deprecated marks an action that is going away, and is nil for one that is
	// staying. It is removed only after a release published this marker for it,
	// and the compatibility gate refuses any other removal.
	Deprecated *vocabulary.Deprecation `json:"deprecated,omitempty"`
}

// TypeDoc is one type of the vocabulary. Subject and Object are derived from
// the relation specs rather than declared, so the two halves of the vocabulary
// cannot disagree about which side of a tuple a type may stand on.
//
// Subject is wider than "is a principal": org and device_group stand on the
// subject side of a structure edge without anything being granted to them.
type TypeDoc struct {
	Type        Type   `json:"type"`
	Summary     string `json:"summary"`
	Description string `json:"description"`
	Subject     bool   `json:"subject"`
	Object      bool   `json:"object"`
	Version     string `json:"version"`
	// Deprecated marks a type that is going away, and is nil for one that is
	// staying. It is removed only after a release published this marker for it,
	// and the compatibility gate refuses any other removal.
	Deprecated *vocabulary.Deprecation `json:"deprecated,omitempty"`
}

// RelationDoc is one edge name. Role is derived: a role relation carries a
// level and confers actions, and a structure relation carries what the object
// it names holds.
type RelationDoc struct {
	Relation    Relation `json:"relation"`
	Summary     string   `json:"summary"`
	Description string   `json:"description"`
	Role        bool     `json:"role"`
	Version     string   `json:"version"`
	// Deprecated marks a relation that is going away, and is nil for one that is
	// staying. It is removed only after a release published this marker for it,
	// and the compatibility gate refuses any other removal.
	Deprecated *vocabulary.Deprecation `json:"deprecated,omitempty"`
}

// ShapeDoc is one permitted edge: the four columns authz_relation_shape holds
// and authz_tuple's composite foreign key names. An empty SubjectRelation is a
// principal named directly, a set one is a userset.
type ShapeDoc struct {
	ObjectType      Type     `json:"object_type"`
	Relation        Relation `json:"relation"`
	SubjectType     Type     `json:"subject_type"`
	SubjectRelation Relation `json:"subject_relation,omitempty"`
}

// actionProse is the descriptor table for the action half, a static composite
// literal and not a map assembled at start-up. It is in Actions() order, and
// TestEveryActionIsDescribed holds it there.
var actionProse = []struct {
	action      Action
	summary     string
	description string
	deprecated  *vocabulary.Deprecation
}{
	{
		action:  ActionObserve,
		summary: "Read a device's live and recorded state.",
		description: "Covers the telemetry stream, the current job, the file listing and every read of a " +
			"device's condition. It is the floor of the device role layer: nothing below it reads " +
			"anything, and every higher role contains it. No verb may carry it, because answering a " +
			"verb writes to a transport that carries one command at a time.",
	},
	{
		action:  ActionOperate,
		summary: "Send a verb of this class to a device: act on the machine in its normal duty.",
		description: "Pause, resume, home, set a target, stop a job, put a file on the machine, and " +
			"every other verb the device's kind declares at this class. It is the floor a verb may " +
			"carry: there is no read class for a verb. The kind's declaration decides which verbs " +
			"exist; this action decides whether the caller may send one of them at all.",
	},
	{
		action:  ActionConfigure,
		summary: "Send a verb of this class to a device: change what the machine is, not the work on it.",
		description: "Firmware, the token a printer reports under, the name it answers to on the " +
			"network. It enters above operator because this server can undo none of them. A verb of " +
			"this class is very often marked irreversible as well, which takes it out of every level " +
			"and into a grant that names the verb.",
	},
	{
		action:  ActionAdminister,
		summary: "Administer the grants written on a device.",
		description: "The top of the device role layer, and the one action no verb may ever carry: a " +
			"driver that could declare a verb of this class would be writing itself into the lattice. " +
			"It is what a caller must hold in full before they can write another grant.",
	},
	{
		action:  ActionViewAudit,
		summary: "Read who authorized which command on a device.",
		description: "The audit-class events a durable command record carries. It is conferred at " +
			"manager and by nothing lower: an operator is a device viewer, and conferring audit sight " +
			"at operator would let every device viewer learn which operator sent which command.",
	},
	{
		action:  ActionReportTelemetry,
		summary: "Submit telemetry as the device itself.",
		description: "Reachable only from the agent relation, which a device credential holds on its own " +
			"device. It is deliberately outside every role mask, so a fleet-wide operator grant is " +
			"not permission to forge a device's readings.",
	},
	{
		action:  ActionFetchJob,
		summary: "Collect queued work as the device itself.",
		description: "The other half of the device credential, reachable only from the agent relation. " +
			"Together with report_telemetry it is what keeps a device credential from becoming an " +
			"operator credential.",
	},
	{
		action:  ActionViewResource,
		summary: "Read the configuration of one resource kind.",
		description: "Covers listing instances of the kind and reading each one's declaration and status. " +
			"Settings an implementation marks secret are redacted before this action sees them.",
	},
	{
		action:  ActionOperateResource,
		summary: "Act on a running instance without rewriting its settings.",
		description: "Restart a failed sink, drain a queue, retry a connection. It is the resource-side " +
			"equivalent of operate. The instance changes state and its declaration does not.",
	},
	{
		action:  ActionConfigureResource,
		summary: "Rewrite the settings block of a resource kind.",
		description: "Create, edit and delete instances of the kind, including the settings a connection " +
			"string lives in. It is the top of the resource role layer and is disjoint from every " +
			"device action: a fleet-wide device grant is not permission to rewrite this server's " +
			"storage configuration.",
	},
	{
		action:  ActionReadStorage,
		summary: "Read what is inside an instance of the kind.",
		description: "Browse the entries of a storage instance and download a file's bytes. It is about " +
			"contents and never about the declaration: view_resource says the instance exists and what " +
			"it is configured as, and this says what is in it. Seeing that a storage backend is there " +
			"and reading the files somebody put in it are not the same permission.",
	},
	{
		action:  ActionWriteStorage,
		summary: "Change what is inside an instance of the kind.",
		description: "Upload a file, create a folder, rename and delete. It is the write half of the " +
			"contents surface and stops there: it is not configure_resource, so a caller who may drop a " +
			"file into a storage instance may not repoint that instance at another directory, delete " +
			"the instance, or administer the grants written on the kind.",
	},
	{
		action:  ActionAdmitDevice,
		summary: "Decide which devices are let onto this server.",
		description: "List the devices waiting at a connector's registration table, approve one so " +
			"its next poll is answered with the credential it dials with, and turn one away. It is " +
			"performed on the driver resource kind, because a registration table belongs to a " +
			"connector. It is deliberately not configure_resource: rewriting a connector's settings " +
			"is changing what this server dials, and admitting a machine is deciding who joins the " +
			"fleet, so a site can hand a technician the second without the first.",
	},
	{
		action:  ActionDevelopDevice,
		summary: "Reach the developer surface of this server.",
		description: "Open the page at /dev, read which printer dialects this build carries and which " +
			"paths this origin answers that the API document does not publish, and read the log of " +
			"commands this process sent to one device. It is performed on the driver resource kind, " +
			"because a connector is an instance of that kind and the command log is what went through " +
			"one. It is deliberately not view_resource: reading that a connector is declared is part " +
			"of seeing the configuration, and the developer surface says what this binary is made of " +
			"and what it sent to a machine.",
	},
}

// typeProse is the descriptor table for the type half, in Types() order.
var typeProse = []struct {
	typ         Type
	summary     string
	description string
	deprecated  *vocabulary.Deprecation
}{
	{
		typ:     TypeUser,
		summary: "One person, as an identity provider resolved them.",
		description: "A user holds grants directly and through the groups they are a member of. The id is " +
			"whatever the configured identity provider produced, so two providers naming the same " +
			"person produce two subjects.",
	},
	{
		typ:     TypeGroup,
		summary: "A named set of people whose grants its members inherit.",
		description: "A group is written on the subject side of a grant as a userset, so granting a group " +
			"operator on a device group is one tuple however many people are in it. Service accounts " +
			"are deliberately not members: a robot never inherits a team's access.",
	},
	{
		typ:     TypeServiceAccount,
		summary: "A non-human caller: a script, an integration or a job runner.",
		description: "A service account holds grants written on it directly and never through a group or " +
			"an org role, so a fleet-wide widening cannot reach one by accident. Every grant it holds " +
			"was written for it on purpose.",
	},
	{
		typ:     TypeDeviceCredential,
		summary: "A device's own credential, and not the device.",
		description: "A device credential is the subject of the agent relation on exactly one device, " +
			"and of nothing else. It is the only type that reaches report_telemetry and fetch_job, and " +
			"it reaches them on one device. The device it speaks for is the device type, which is " +
			"the object a grant is written on.",
	},
	{
		typ:     TypeNode,
		summary: "A federated peer site acting on this one's devices.",
		description: "A node is written on the subject side of a device or device-group grant like a " +
			"principal, so what a peer may do here is the same graph question as what a person may do, " +
			"and it is answered locally rather than trusted from the wire.",
	},
	{
		typ:     TypeSink,
		summary: "One configured event sink, as a holder of grants.",
		description: "A sink is the instance an operator declared under sink:, reading the event bus as " +
			"itself rather than as a service account of the same name. Nothing issues a credential for " +
			"one and no caller can present it: it exists only while this server is supervising that " +
			"instance. It is the one non-human type an org role accepts, and only at viewer, so " +
			"org:local#viewer@sink:audit is the single tuple that feeds a sink the whole fleet " +
			"including a printer admitted tomorrow.",
	},
	{
		typ:     TypeHistory,
		summary: "One configured history provider, as a holder of grants.",
		description: "A history provider is the instance an operator declared under history:, reading " +
			"the event bus as itself and storing what it is granted. Nothing issues a credential for " +
			"one and no caller can present it. Like a sink it is accepted at org viewer and no " +
			"higher, and that single tuple is what feeds it a printer admitted tomorrow: a provider " +
			"holding hand-written per-origin grants stores nothing for the origins they miss, and a " +
			"store with a hole in it is discovered when somebody needs the rows.",
	},
	{
		typ:     TypeOrg,
		summary: "The tenant everything else hangs under.",
		description: "An org carries the RBAC half of the model: admin, operator and viewer on the org " +
			"reach every device in it at manager, operator and viewer level. It is the object side " +
			"only, so nothing is ever granted to an org.",
	},
	{
		typ:     TypeDeviceGroup,
		summary: "A tree of devices that grants are inherited down.",
		description: "A device group stands on both sides: grants are written on it, and a device names " +
			"its group so a grant there reaches every device under it. parent nests one group inside " +
			"another, and the depth the resolver walks is bounded.",
	},
	{
		typ:     TypeDevice,
		summary: "One physical device.",
		description: "The object every device action is defined on. A device names its org and its " +
			"group, so a grant written at either level reaches it without a tuple per device. Its kind " +
			"says what type of device it is: this build registers printers and cameras. A device may " +
			"also name another device as its parent, which is how a camera is reached by whoever may " +
			"reach the printer it watches.",
	},
	{
		typ:     TypeResourceKind,
		summary: "The configuration surface of one resource kind.",
		description: "Its ids are exactly the configurable resource kinds. A grant here governs every " +
			"instance of that kind, because a resource instance is a line of this server's " +
			"configuration and not a device somebody is given access to.",
	},
}

// relationProse is the descriptor table for the relation half, in Relations()
// order.
var relationProse = []struct {
	relation    Relation
	summary     string
	description string
	deprecated  *vocabulary.Deprecation
}{
	{
		relation:    RelMember,
		summary:     "Membership of a group.",
		description: "The subject of a member edge inherits every grant written on the group as a userset.",
	},
	{
		relation: RelParent,
		summary:  "One object nested inside another of its own type.",
		description: "Grants on the parent reach everything beneath it. It nests one device group inside another, and " +
			"it binds one device to another, which is a camera and the printer it watches. The walk is bounded, so a " +
			"cycle a write created cannot become an unbounded resolve.",
	},
	{
		relation:    RelOrg,
		summary:     "The tenant an object belongs to.",
		description: "Structure, not a role. It is what makes an org admin reach a device without a tuple per device.",
	},
	{
		relation: RelGroup,
		summary:  "The device group a device sits in.",
		description: "Structure, not a role. Moving a device between groups is one edge and changes every grant that " +
			"reaches it.",
	},
	{
		relation: RelAgent,
		summary:  "A device's own credential, on that device.",
		description: "The only relation that confers outside the role hierarchy, and the only one whose conferral a caller " +
			"holding manager does not already hold. Writing one is refused unless the writer holds it too, which is " +
			"what stops a session minting a device credential.",
	},
	{
		relation: RelBlocked,
		summary:  "A deny that covers every action on the object.",
		description: "It confers nothing and removes the role layer rather than adding to it, so it wins over any grant " +
			"reaching the same subject from anywhere.",
	},
	{
		relation: RelViewer,
		summary:  "The read-only role.",
		description: "Confers observe on a device. Confers view_resource and read_storage on a resource kind. Every " +
			"higher role contains it.",
	},
	{
		relation: RelOperator,
		summary:  "The day-to-day role.",
		description: "Contains viewer and adds operate on a device. Adds operate_resource and write_storage on a " +
			"resource kind. These actions change what a device or an instance is doing, and what is inside one, " +
			"without changing what it is.",
	},
	{
		relation: RelManager,
		summary:  "The administering role.",
		description: "Contains operator and adds configure, administer and view_audit on a device. Adds " +
			"configure_resource, admit_device and develop_device on a resource kind. A caller must hold this whole " +
			"set before they may write a grant on the same object.",
	},
	{
		relation: RelAdmin,
		summary:  "The org-wide manager role.",
		description: "Exists only on org, and reaches every device in the org at manager level. It is the RBAC half of the " +
			"model.",
	},
	{
		relation: RelOwner,
		summary:  "Whose machine this is.",
		description: "Not a role, and it carries no level. It adds observe, operate and configure to whatever the " +
			"role layer already gives its subject. It also reaches every dangerous verb of the owned device's kind. " +
			"So somebody who owns a device and holds only operator on it may flash it, and an operator who owns " +
			"nothing may not. It stops short of administer, so an owner cannot hand ownership on. Whoever " +
			"administers the device writes this edge. It stops short of view_audit too. It is written on the " +
			"device, so it is inherited from nothing and a block on the same device removes it.",
	},
}

// Relations returns the closed set of edge names, in declaration order.
func Relations() []Relation {
	return []Relation{
		RelMember, RelParent, RelOrg, RelGroup, RelAgent, RelBlocked,
		RelViewer, RelOperator, RelManager, RelAdmin, RelOwner,
	}
}

// ActionDocs returns every action with the object type it is defined on and the
// relations that confer it there. It is the one accessor the published page and
// the API endpoint both read, so the two cannot answer differently.
func ActionDocs() []ActionDoc {
	out := make([]ActionDoc, 0, len(actionProse))

	for _, entry := range actionProse {
		domain, _ := entry.action.domain()

		out = append(out, ActionDoc{
			Action:      entry.action,
			Domain:      domain,
			Summary:     entry.summary,
			Description: entry.description,
			ConferredBy: confersOn(domain, entry.action),
			Version:     VocabularyVersion,
			Deprecated:  entry.deprecated,
		})
	}

	return out
}

// TypeDocs returns every type with the sides of a tuple it may stand on.
func TypeDocs() []TypeDoc {
	out := make([]TypeDoc, 0, len(typeProse))

	for _, entry := range typeProse {
		out = append(out, TypeDoc{
			Type:        entry.typ,
			Summary:     entry.summary,
			Description: entry.description,
			Subject:     standsAsSubject(entry.typ),
			Object:      standsAsObject(entry.typ),
			Version:     VocabularyVersion,
			Deprecated:  entry.deprecated,
		})
	}

	return out
}

// RelationDocs returns every relation, saying which are the role layer.
func RelationDocs() []RelationDoc {
	out := make([]RelationDoc, 0, len(relationProse))

	for _, entry := range relationProse {
		_, isRole := roleLevel(entry.relation)

		out = append(out, RelationDoc{
			Relation:    entry.relation,
			Summary:     entry.summary,
			Description: entry.description,
			Role:        isRole,
			Version:     VocabularyVersion,
			Deprecated:  entry.deprecated,
		})
	}

	return out
}

// Shapes returns every permitted edge, in the order the vocabulary declares
// them. It is the grammar a tuple write is held to, projected into the rows the
// database's composite foreign key points at.
func Shapes() []ShapeDoc {
	expanded := expandShapes(relationSpecs)

	out := make([]ShapeDoc, 0, len(expanded))
	for _, shape := range expanded {
		out = append(out, ShapeDoc(shape))
	}

	return out
}

// confersOn is which relations, written on an object of this type, hand out
// this action. It reads conferred(), the same function the write path uses to
// bound an escalation, so the published answer is the enforced one.
//
// It names only relations the vocabulary declares on that object type. A role
// held further up reaches the object through a structure edge instead: org and
// group confer what the object they name holds, which is what the relation
// descriptors say and what a conferral list cannot.
func confersOn(object Type, action Action) []Relation {
	out := []Relation{}

	mask := action.mask()
	if mask == 0 {
		return out
	}

	for _, relation := range Relations() {
		if _, declared := lookupRelation(object, relation); !declared {
			continue
		}

		if conferred(object, relation)&mask != 0 {
			out = append(out, relation)
		}
	}

	return out
}

// standsAsObject reports whether grants are written on this type.
func standsAsObject(t Type) bool {
	for _, spec := range relationSpecs {
		if spec.Object == t {
			return true
		}
	}

	return false
}

// standsAsSubject reports whether this type appears on the subject side of any
// permitted edge.
func standsAsSubject(t Type) bool {
	for _, spec := range relationSpecs {
		for _, subject := range spec.Subjects {
			if subject.Type == t {
				return true
			}
		}
	}

	return false
}
