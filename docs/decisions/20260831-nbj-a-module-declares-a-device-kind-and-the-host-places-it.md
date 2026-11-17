---
title: "ADR-20260831-nbj: a module declares a device kind and the host places it"
description: "A third party contributes a driver or a device class with no core edit: a declared device kind enters the directory as a qualified id, the module places its own verbs on the core device actions, no permission is declared on the device object, and every device event takes the origin kind device."
date: 2026-08-31
status: accepted
amends: [ADR-20260826-5yj]
tags:
  - adr
  - devices
  - extensibility
  - modules
  - authorization
---
# ADR-20260831-nbj: a module declares a device kind and the host places it

- status: accepted, and every gate is owed
- date: 2026-08-31

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Amends [ADR-20260826-5yj](20260826-5yj-a-device-is-the-object-and-a-printer-is-the-type.md) rules 8 and 12, and its step 5 | Rules 8 and 17 are amended by [ADR-20260831-c7b](20260831-c7b-an-adapter-declares-the-states-and-the-permissions-and-the-host-places-them.md), which carries the state set into a kind declaration and opens the device object to a declared permission | Builds on [ADR-20260828-xdp](20260828-xdp-a-contributed-resource-kind-is-a-grantable-object.md), [ADR-20260827-rqc](20260827-rqc-an-extension-declares-a-permission.md), [ADR-20260825-dee](20260825-dee-the-capability-vocabulary.md) | Reads with [ADR-20260827-wmv](20260827-wmv-a-module-is-mounted-and-an-adapter-is-constructed.md), [ADR-20260827-rwq](20260827-rwq-a-module-keeps-its-data-out-of-the-host-schema.md), [ADR-20260827-7jn](20260827-7jn-the-plugin-wire-is-a-specification.md)

Rules 1, 4, 12, 17 and 19 state the requirement. The rest follow from them.

## the requirement, and the rule that blocks it

The requirement is one thing, and it is not a preference. Both our internal
use and the community must be able to contribute any adapter, for a Voron, a
Bambu Lab or a home-made CNC, without touching the core.

[ADR-20260826-5yj](20260826-5yj-a-device-is-the-object-and-a-printer-is-the-type.md) rule 8
closed the device kind set and gave it to the core. That was the right rule
while every device class was ours to ship. It is the rule that now blocks the
requirement, twice over.

**It publishes what must stay private.**
`docs/vocabularies/resource-kinds.json` already carries `printer` and `camera`
as rows. `cmd/generate-resource-kinds` writes that file and the page beside it
from `ref.KindDocs()`, a Go table the binary links. A device class can be
private to whoever builds it. Minting a core kind for one writes
its name, its summary and its capability floor into two artefacts the open
source core publishes. There is no flag that holds one row back.

**It prices every community class at one of our releases.** A kind minted in
`ref.Kind` is a compile-time table only a core release changes. A person with a
home made CNC then waits on our release cycle for the class to exist. That is
the camera precedent's exact failure to scale, and it is what "avoid touching
the core just for this" refuses.

This record opens the device directory to a kind an installed module declares,
in the shape [ADR-20260828-xdp](20260828-xdp-a-contributed-resource-kind-is-a-grantable-object.md)
opened the resource projection.

## rules

### the requirement

1. Let a third party contribute a driver or a device class with no core edit.
   It is the bar every other rule here is measured against.
2. Read rule 1 as barring a per-vendor and a per-class core edit, and not the
   mechanism. Building the seam once in the core is what rule 1 asks for.
   Adding a row to a core table for each new vendor is what it refuses.
3. Serve two tiers separately, because the first is deliverable sooner and
   unblocks more people. A community printer dialect is a new driver for the
   kind `printer`, which already exists, so it needs nothing from this record
   past rules 12 to 16. A new device class needs the whole of it. The two are
   sequenced on
   the old backlog.
   Sooner is not the same as small. An audit on 2026-08-31 found four things
   blocking the printer tier, not the two a scoping note assumed, and that
   epic names them. Today the documented path for an outside printer author is
   a blank import in `internal/app/app.go` and a `replace` in the root
   `go.mod`. That is a fork of the binary, and rule 1 refuses it.

### the kind

4. Accept a device kind an installed module declares as a qualified id,
   `<namespace>/<name>`, and place it in the device directory beside the core
   kinds. This amends ADR-20260826-5yj rules 8 and 12.
5. Keep `ref.Kinds()` a compile-time table. A contributed device kind is a
   string in the directory row, on the wire, in a list filter and in the
   admission check. It reaches none of the switches that read `ref.Kind`.
   [ADR-20260825-bhe](20260825-bhe-every-vocabulary-is-a-gated-published-resource.md) rule 4
   and ADR-20260827-wmv rule 8 stand unchanged. That is what keeps this record's cost
   to one id space.
6. Read the contributed half of the kind set from the set the host installed,
   and never from a running module. That is ADR-20260828-xdp rule 2 applied to the
   device directory, over the same overlay record ADR-20260827-rwq rule 10 writes.
   `validDeviceRow` at `internal/authz/store.go:341` then accepts the same set
   before the port binds that the install mutation wrote.
7. Derive the namespace from the declaring module and refuse a module that
   passes one in. That is ADR-20260827-rqc rule 16's derivation,
   `extension.NamespaceOf`, reused with no change.
8. Carry three things in the declaration and no others: a display name, a
   capability floor, and a verb list. The display name is what a program shows
   for a kind it has no icon for. ADR-20260826-5yj rule 13 already accepted that cost
   for a federated peer's unknown kind.
   - **Two more things by
     [ADR-20260831-c7b](20260831-c7b-an-adapter-declares-the-states-and-the-permissions-and-the-host-places-them.md)
     rules 5 and 6.** A declaration also carries the state set, each state with
     a display name, and the transitions between them.
     `internal/kernel/devicestate` is where the host places both.
9. Check the capability floor at admission against the driver's `Baseline()`,
   and refuse the device otherwise. A kind nothing checks is a label, and the
   row that owes this check is on
   the old backlog.
10. Keep the directory's kind conflict check as it stands. `holdKind` at
    `internal/authz/store.go:364` already refuses a registration that changes
    what an id names, so a contributed kind cannot capture an existing device's
    grants.
11. Bound the contributed device kind space per namespace and per deployment,
    and refuse the install that passes either bound. ADR-20260828-xdp rule 5 set 8 and
    16 for resource kinds against a measurement. A device kind adds no
    projected row, so its bound is about a person reading a fleet and not about
    time, and the number is owed to the epic.

### the verbs

12. Let a module place its own verbs onto the core device actions.
13. Namespace a module verb under the declaring module, the way ADR-20260825-dee rules
    1 and 26 already namespace a capability key. A verb keeps the capability
    vocabulary's grammar and takes no second one.
14. Place a module verb on `send_command` at the weakest, and refuse a
    placement on `send_gcode` or `provision`. A dialect escape and a
    provisioning act are not something an install grants by declaring them.
    - **One name in that denylist is retired.**
      [ADR-20260831-c7b](20260831-c7b-an-adapter-declares-the-states-and-the-permissions-and-the-host-places-them.md)
      rule 17 moves `send_gcode` off the core action vocabulary and onto the
      xBuddy adapter's own declaration. The dialect-free action keeps the bit,
      so the placement this rule refuses still has a target to name. What the
      two rules together leave undecided is on the open items below.
    - **Amended by [ADR-20260901-b2z](20260901-b2z-driver-declared-permissions.md) rules 4 and
      18.** The names this rule uses cease to exist. The floor is the `operate`
      class, nothing declares `administer` or `view_audit` on a verb, and
      nothing clears a host marking. So an install still cannot grant itself a
      dialect escape or a provisioning act.
15. Hold the module verbs in a per-install half of the verb-action table,
    beside the generated half, on ADR-20260828-xdp rule 8's split.
16. Refuse a verb that names no action at install, and keep the command path
    fail-closed. `authz.ActionForVerb` at `internal/authz/vocabulary.go:482`
    returns the zero action for an unknown verb and the command is refused
    before the driver is reached. That behaviour does not move. What moves is
    when the refusal is read: at install, where a person sees it, instead of at
    the first command.

### the permissions

17. Declare no permission on the `device` object yet. A contributed device
    class is governed by the core device actions, and adapters and modules may
    extend them later.
    [ADR-20260827-rqc](20260827-rqc-an-extension-declares-a-permission.md) rule 14 stands
    unchanged.
    - **Opened by [ADR-20260831-c7b](20260831-c7b-an-adapter-declares-the-states-and-the-permissions-and-the-host-places-them.md).** An adapter declares a permission on the
      device object, and rules 10 to 13 there place it.
18. Keep the per-kind permission reachable and build nothing for it. The verb
    list of rule 12 maps onto actions and never onto names, so opening rule 14
    later adds a second column to that list and rewrites no stored grant.
    Earlier research's per-kind mask is the shape that would take it.
    - **The shape is taken by [ADR-20260831-c7b](20260831-c7b-an-adapter-declares-the-states-and-the-permissions-and-the-host-places-them.md).**
      Rule 12 there maps a declared name onto core action bits per device kind.
      Rule 11 keeps the mapping in a table of its own. No stored grant moves,
      which is what this rule promised.
    - **Superseded by [ADR-20260901-b2z](20260901-b2z-driver-declared-permissions.md) rule 5.**
      The per-kind verb table is built instead of a per-kind permission column.
      No stored grant moves: the per-build half is a new dimension on the verb
      table and the tuples are untouched.

### the origin and the events

19. Render a device event's origin as `device:<id>`, one origin kind for every
    device. An adapter that handles no device is not a device interface but a
    module. This amends ADR-20260826-5yj step 5. That step renamed the event types and left
    the origin kind open.
20. Read rule 19 as the answer to the qualified kind in an origin. The origin
    grammar refuses `/` at `sdk/event/origin.go:27`, because that byte already
    separates parts of a source URN. Under rule 19 a device kind never renders
    into an origin at all, so the conflict is closed rather than worked around.
21. Take event types under the declaring module's namespace for anything
    specific to a contributed kind. `sdk/event.ValidateType` already passes
    them and the envelope does not change.

### what is not published

22. Publish no row for a contributed device kind. It adds no entry to
    `docs/vocabularies/resource-kinds.json`, none to the generated reference
    pages, and moves no content version under
    [ADR-20260827-qvk](20260827-qvk-a-published-version-is-refused-or-it-is-not-published.md)
    rule 1. This extends ADR-20260828-xdp rule 8's split from the authorization
    vocabulary to the device kind, capability and event type vocabularies,
    which have no per-install half today.
23. Serve a contributed kind from the per-install half of the vocabulary
    endpoint and gate the generated half alone. Two installs of one build then
    answer differently, and rule 8's display name is what makes that readable
    rather than confusing.
24. Ship a private device class as a module with `runtime: service`. The
    runtime table already permits it for a driver at
    `internal/kernel/ref/runtime.go:78`. A builtin runtime would link the class
    into the shipping binary. That is the leak rules 22 and 23 exist to stop,
    and the alternative is a private fork nobody should maintain.
25. Read a contributed device class as a module that carries an adapter. The
    artefact an operator installs is a module, because it declares. The thing
    the host constructs from it fills `connectorapi.Set`, so it is an adapter.
    ADR-20260827-wmv rule 4 keeps a printer driver an adapter, and this record does
    not move it. The driver inside the module is still an adapter. The
    declaration around it is the module act.

## the rules this record amends

A design rule that blocks the requirement may be amended. That permits the two
amendments below. It does not permit dropping what those rules protected. So
each one names its replacement.

Rule numbers below are this record's own unless an ADR is named.

### ADR-20260826-5yj rule 8, the closed kind set

**What it protected.** A program can switch on a kind. It picks an icon, a
filter, a default page and a capability floor from it. And two installs of one
build answer alike from the device kind vocabulary.

**What protects that now.** Rule 8 makes the module declare a display name, so
a program gets the material it would have switched on. Rule 9 checks the
capability floor at admission, so the kind is a checked claim and not a label.
ADR-20260826-5yj rule 13 already commits every reader to a kind this build does not
carry, because federation requires it.

The equal-answer half is genuinely given up. Two installs of one build will
answer differently, and rule 23 is where a reader is told so. That is the real
cost of this record and it is not hidden anywhere else.

### ADR-20260826-5yj rule 12, no kind an extension mints

**What it protected.** An extension cannot name a thing the core has to place.
That is ADR-20260827-rqc rule 13's answer for a relation, applied to a kind.

**What protects that now.** The module still only declares. The host places the
kind at install, from the installed overlay and never from a running module,
which is rule 6. The host derives the namespace and refuses one passed in,
which is rule 7. So declare-and-place is kept whole. Only the id space widens.

### two rules the requirement was checked against

Neither had to move for this record. One of the two moved later, and its entry
says so.

- **ADR-20260825-bhe rule 4**, a core vocabulary is a compile-time table and not a
  registry filled at start-up. Rule 5 keeps `ref.Kinds()` exactly that. The
  contributed kind is a string in a directory row and never a `ref.Kind`, so
  this record needs no change here
- **ADR-20260827-rqc rule 14**, a declaration naming a core object is refused. The
  requirement here is met with the core device actions.
  [ADR-20260831-c7b](20260831-c7b-an-adapter-declares-the-states-and-the-permissions-and-the-host-places-them.md)
  opens it later, and rule 18 above is what made that cheap

## why a private device class cannot be a core kind

The recommendation this record replaces was to build the first non-printer
device class as a core-minted kind. It would prove every seam under ADR-20260826-5yj rule
8 as written, and open the contributed door afterwards. That order was cheap
and safe. It is unavailable twice.

It is unavailable for a private class. Nothing about such a class may reach
the published core. A kind in `ref.Kind` reaches it three
ways. The constant is in the open source tree. The row is in the committed
vocabulary artefact. The page is in the generated reference.

It is unavailable as a general answer, because rule 1 refuses a core edit per
class. So the contributed path is the enabling work and not the second phase.

The per-install split does hold, and the reason is structural rather than a
promise. `cmd/generate-resource-kinds` and `cmd/generate-authz-vocabulary` read
compiled Go tables only. Neither opens the installed overlay, a database or
anything a module supplies. So a declaration cannot reach a committed file,
and the `-check` gates cannot fail on a machine that has a module installed.

What is true of the mechanism is not yet true of every part of the code.
ADR-20260828-xdp's own enforcement table marks every gate owed. One per-install half is
served: `GET /api/v1/vocabularies/device-states` publishes the declared state
sets of [ADR-20260831-c7b](20260831-c7b-an-adapter-declares-the-states-and-the-permissions-and-the-host-places-them.md)
rule 9, and it is the first of the split's two halves to exist.

## enforcement

| rule | gate | state |
|---|---|---|
| 1, 3 | a case that adds a device class and a printer dialect with no file under `internal/` or `ref` changed | owed. It is the gate rule 1 asks for and nothing like it exists |
| 4, 6 | `validDeviceRow` accepting the installed set, with a case per direction | owed. Nothing installs a device kind |
| 5 | `make resource-kinds-check`, unchanged | exists, and holds while rule 5 does |
| 7 | the namespace derivation ADR-20260827-rqc rule 16 already gates | exists for a declared action, owed for a kind |
| 9 | the admission check against `Baseline()` | owed, on the old backlog |
| 10 | `holdKind`, already written | exists |
| 14, 16 | the install-time validation of a declared verb list | owed |
| 19, 20 | a case that every device origin renders `device`, and one that a qualified kind never reaches `event.NewOrigin` | owed. `internal/app/stream.go:59` mints `event.KindPrinter` for every device row |
| 22 | the vocabulary generators reading a compiled table alone | exists as a property of the generators, and no case asserts it |
| 24 | the install loader refusing a device module the builtin runtime | exists for a camera driver, owed for a contributed kind |

## rejected

| option | why not |
|---|---|
| mint each new class in `ref.Kind`, the camera path repeated | it publishes the class, and it prices every community class at one of our releases. Rule 1 refuses both |
| open the kind set but keep verb placement in the core table | see [the half-open path](#the-half-open-path) |
| give a contributed class its own permission names on `device` | not yet. Rule 18 keeps it cheap later |
| one origin kind per device kind | the origin grammar refuses `/` at `sdk/event/origin.go:27`, so a qualified kind needs a rendering nothing else needs |
| the vendor's machine as a module's own object | see [a machine outside the device directory](#a-machine-outside-the-device-directory) |
| generate the contributed half into the committed vocabulary artefact | the artefact is per-build and a declaration is per-install, so the gate fails wherever a module is installed |

### the half-open path

Opening the kind set while verb placement stays in the core table leaves an
outside author with a working kind and no verb. They ship a machine nobody can
command. The core edit rule 1 refuses has not gone away. It has moved to a
different table, which is worse, because the design now looks finished.

### a machine outside the device directory

This is the option where the vendor's machine lives in the module's own
subtree, as a contributed resource kind under ADR-20260828-xdp. Everything it needs is
already decided, and it costs no new rule at all.

It is refused because the machine is then reached differently from every other
machine. It has no row in `/api/v1/devices`. It carries no per-device grant,
because a resource kind grant is kind-wide. It gets no host-side capability
check. Federation cannot see it as a device.

A machine reached differently from every other machine is the one outcome this
product exists to prevent. The option stays correct for nouns that are not
devices, such as a spool.

## open items

- No contract crosses the wire. `sdk/wirerpc` publishes `plugin.info` and
  `plugin.shutdown` and nothing else, so a `runtime: service` driver is
  launched and never called. Rule 1 is unreachable for either tier until the
  wire method set lands, which is ADR-20260827-7jn's open item and the old backlog's row
- ADR-20260828-xdp's mechanism is unbuilt in every part. This record copies a pattern
  that has no code, so the first build of it proves both
- Every site that reads a device kind is unaudited. One is known:
  `internal/app/stream.go:59` renders every visible device as origin kind
  `printer` whatever its row says. Under rule 19 that constant becomes
  `device`, and the audit is a row on
  the old backlog
- Rule 16 says a module verb is refused at install. Placing one today means
  editing two tables, the authorization table and the profile data, which
  `internal/app/verbaction_test.go` holds mutually total. And `CanonicalVerb`
  at `internal/authz/vocabulary.go:493` prefixes an unqualified verb with
  `core.command.`, so a module-namespaced verb cannot canonicalise. Rule 13's
  namespaced verb needs that path opened
- Nothing on the host turns a child process into a `connectorapi.Driver`.
  `internal/extension/supervised` carries `sink.go`, `camera.go` and
  `store.go` and no `driver.go`, so the wire method set is necessary and not
  sufficient
- A printer dialect is declared under `core.connectors`, and
  `internal/app/config/connectors.go:22` carries no `runtime` key, so there is
  no place to name an out-of-process dialect even once one can be called
- Rule 11's two bounds carry no number. The measurement that would set them is
  owed
- `GrantRef.ObjectID` at `internal/transport/http/httpapi/grants.go:77` caps at
  63 bytes and a qualified id needs 160. ADR-20260828-xdp rule 9 already owes that
  widening, and a device id is not a kind id, so this record inherits the seam
  rather than adding one
- Nothing decides which module verbs carry a payload. Rule 14 refuses a
  placement by naming `send_gcode`, and [ADR-20260831-c7b](20260831-c7b-an-adapter-declares-the-states-and-the-permissions-and-the-host-places-them.md)
  rule 17 retires that name from the core vocabulary. A module verb carrying an
  unconstrained string or an opaque argument, placed on `send_command`, then
  reaches an operator with a payload nothing inspected. ADR-20260831-c7b rule 18 exists
  to stop that and covers a payload action rather than a verb, so the gap is
  recorded here and no rule answers it yet
- Rule 8 now carries five things and its bound is unmeasured. ADR-20260831-c7b's open
  items put the bound on a declared state set per kind on ADR-20260828-xdp rule 5's
  model, and the same question reaches the transitions
