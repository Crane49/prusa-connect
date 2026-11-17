// Package authz is the authorization graph.
//
// The model is Zanzibar relation tuples: a tuple is object#relation@subject,
// and the subject may itself be object#relation. That makes groups, nested
// groups and hierarchy inheritance one mechanism. Users, groups, service
// accounts, devices, sinks and peer nodes are subjects in the same graph, so
// "this integration controls devices 1 to 3 and no others" is three tuples
// and not a second permission system.
//
// The type and relation vocabularies are closed and owned here. There is no
// registration hook for either, and a new type or relation is a change to
// vocabulary.go, which declares them and carries the reason for each one. A
// relation vocabulary that extensions could add to would need a different
// engine than this graph, so it is closed on purpose.
//
// The permission vocabulary is not closed, and it is open in a narrow place. A
// driver declares its own verbs, and each verb carries a class from a closed
// set the core owns: operate or configure. The core never learns what a verb
// means. It checks that the verb is declared for the device's kind, that its
// class is in the closed set, and that the caller's grant carries that class.
// Grants name classes, and a class is an ordinary tuple in the one grant
// system this package has.
//
// So the *class* set is a change to vocabulary.go and stays one: placing a name
// into the lattice is the escalation decision, and that decision is the core's.
// The open part is the verb table, which is not the graph. It has two halves,
// both in verbtable.go: the printer's own verbs, a static Go table keyed to
// the device kind printer, and the verbs of every kind a declaration compiled
// into this binary carries. Totality holds over the union, there is no default
// arm, and a (kind, verb) pair nothing declared yields the zero Action.
//
// The single exception is the dangerous tier: a verb no class, role or level
// confers, reached only by a row naming the subject, the device and the verb.
// That row is authz_declared_grant, beside authz_tuple and never inside it, and
// it is compiled into a projection of its own by the same graph walk. See
// verbgrant.go. A verb is in the tier because a hand marked it: the host
// computes opaque_payload from the capability descriptor whatever the
// declaration says, and an adapter may raise its own bar and never lower one.
//
// # Resolution
//
// Roles are ordered manager > operator > viewer. A device's effective role for
// a principal is the highest of its direct grant, the grants on every
// device_group above it, and the org role. An org role reaches a device through
// the device's own org tuple or through the org of any group above it: tenancy
// written on a group is tenancy, so an org admin needs no tuple per device.
//
// A blocked tuple removes the role layer entirely, so one deny covers every
// action and cannot be forgotten when an action is added.
//
// owner is not a role and has no level. It says whose machine a device is, and
// it adds observe, operate and configure to whatever the role layer already
// gave that principal, plus every dangerous verb of the device's kind. So
// somebody who owns a device and holds only operator on it may flash it and may
// send raw gcode to it, and an operator who owns nothing may do neither. It is
// written on the device, so it is inherited from nothing, a block on the same
// device removes it, and it stops short of administer: whoever administers the
// device says who owns it, and the owner cannot hand ownership on. It reaches a device
// inside the one it is written on, because that is the mask the parent
// resolved, and a block on that child removes it there too.
//
// A handler checks an action and never a role, which lets the role layer change
// without touching a call site. This is the mapping the oracle in
// equivalence_test.go transcribes by hand, so it is stated here and not read
// out of vocabulary.go:
//
//	observe           viewer, or the device's own agent
//	operate           operator
//	configure         manager
//	administer        manager
//	view_audit        manager
//	report_telemetry  agent only
//	fetch_job         agent only
//
// What owning a device adds on top is below.
//
// That table is the role layer and it is not the whole of what a caller may
// send. A verb some hand marked is conferred by none of those levels: reaching
// one takes a row naming it, or ownership of the machine, which implies every
// dangerous verb of the owned device's kind, computed and not stored.
//
// A history provider reads audit beside whatever its role gave it, which is the
// one other addition outside the role layer. It is granted one org#viewer tuple
// at first sight, and viewer confers observe alone, so without the addition the
// durable-record filter would starve every provider silently.
//
// On a resource kind: view_resource and read_storage at viewer,
// operate_resource and write_storage at operator, configure_resource,
// admit_device and develop_device at manager. admit_device is deciding which
// devices are let onto this server, held on the driver kind because a
// registration table belongs to a connector. develop_device is reaching the
// developer surface, held on the same kind for the same reason: a connector is
// an instance of it and the command log is what went through one.
//
// An action belongs to exactly one object type, and the two sets of bits are
// disjoint. Asking a device action of a resource kind is refused rather than
// answered no, because the lookup would miss for a reason that reads like a
// denial.
//
// # A device inside a device
//
// A device may name another device as its parent, for example a camera and the
// printer it watches. The child is a device row like any other, and the edge
// adds inheritance the way a group grant does: a principal who reaches the
// printer reaches its camera at the same mask.
//
// The printer's own sources are carried down rather than flattened. What an org
// or a group confers on the printer is still tenancy on the camera, and what is
// written on the printer arrives as a third thing. They are held apart because
// whoever administers the camera writes the edge and does not write tenancy.
// Tenancy outranks the edge, which is what lets an administrator hide a camera
// from a viewer of its printer, and stops a delegate blocking that
// administrator off one.
//
// Two things do not flow down the edge. A device credential does not: a device
// reaches the device it is the agent of and nothing else. A block written on
// the child does not flow up, so it hides one camera from somebody who may
// still see the printer.
//
// The walk stops at a device it is already visiting, so a parent cycle
// converges rather than recursing. A cycle is a configuration error, and this
// compiler runs inside the transaction every grant write takes.
//
// # Why a resource grant is written on the kind and not on the instance
//
// A device is a machine somebody is given access to, four hundred at a time. A
// resource instance is a line of this server's configuration, so the question
// the API asks is "may you configure this server's storage" and not "may you
// touch this backend". Three things make the kind the right object:
//
//   - creating an instance names no instance, so whatever authorizes a create
//     is written on the kind whatever else the model has
//   - a per-instance boundary that create defeats is not a boundary. Whoever
//     may create a storage instance can create one pointing at the bucket
//     another instance holds
//   - the row count is principals times the closed kind set, so this projection
//     does not grow with the configuration the way device_access grows with the
//     fleet
//
// What varies per instance is only whether the instance exists. authz_resource
// is that directory and the point check joins it, so an instance the server
// does not have refuses exactly as one the caller may not see does.
//
// The id space of resource_kind is ref.ConfigurableKinds and nothing else.
// ref.Kind also names a printer and a node, because the event envelope, the
// health key and the authorization key are one vocabulary. A grant on
// resource_kind:printer is storable and compiles to no row, which fails closed
// the same way a grant on an unregistered device does.
//
// # Resource instances do not federate
//
// A device identity is a fleet-unique ULID and is addressable from anywhere in
// the tree. A resource instance id is operator-chosen, node scoped and
// DNS-1123, and two sites both have a storage:local. An event.Origin carries
// no site token and is both the hub's channel key and the authorization key,
// so a parent granted a child's storage:local would attach to its own.
//
// So there is no intersective ceiling over resource grants and nothing to
// attenuate downward: an instance exists at exactly one node and its grants are
// written there. R5 is unaffected.
//
// # Enforcement
//
// One relation definition drives the point check, the list and the event
// stream filter. Writing a tuple recompiles the denormalized projections
// inside the same transaction, and Can, CanResource, ListDevices,
// ListResources and Visible then read those tables with the same predicate.
//
// Visible is the event-stream filter, and it is here and not in the transport
// because the epoch has to be read before the rows and not after. Stamped
// after, the answer describes a graph newer than the rows it carries and a
// revocation is never noticed. A revocation is noticed only because of that
// order, so it is not left to each caller.
//
// A list is resolved inside the query. Fetching rows and filtering them in Go
// leaks through pagination, counts and offsets even when the rows it returns
// are right. The order is a case-folded sort key written with the directory
// row, because ordering by free text orders by the database collation and puts
// different rows on the same page on the two engines.
//
// The vocabulary is enforced in three places reading one definition. NewTuple
// refuses an illegal edge, the database refuses a tuple whose shape is not a
// row of authz_relation_shape, and every row read back is rebuilt through
// NewTuple. A row arriving from a restored backup or an import is refused
// rather than resolved as whatever it resembles.
//
// The shapes are rows and not a CHECK because the value set changes with the
// code. Reconcile writes relationSpecs into the lookup tables at startup, so a
// new relation is an INSERT on both engines instead of DDL that SQLite cannot
// express without rebuilding the table. Reconcile refuses to start when a shape
// has left the vocabulary while tuples still name it.
//
// On SQLite a foreign key binds only when the connection asked for it, so
// Migrate refuses a handle whose foreign_keys pragma is off. Migrate also
// records a checksum per applied migration and refuses a file that has moved,
// been renamed or been deleted, so a migration is immutable once it has run and
// a change to one goes in a new file. The ledger carries its own version,
// because it is the one table no migration can alter.
//
// Credentials, token issuance and federation are not here. Neither is what a
// principal may see of a camera's media, which is a device-scoped question: a
// grant on resource_kind:camera reaches that kind's configuration and its
// lifecycle events, and nothing a camera shows.
package authz
