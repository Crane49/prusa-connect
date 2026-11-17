---
title: "ADR-20260827-rqc: an extension declares a permission"
description: "A third-party extension declares an authorization permission and names no level. Whoever administers the object holds it from the moment the host registers it. The registration is a graph mutation, so it moves the epoch and reports every principal it widened."
date: 2026-08-27
status: accepted
tags:
  - adr
  - authorization
  - extensibility
  - security
---
# ADR-20260827-rqc: an extension declares a permission

- status: accepted
- date: 2026-08-27

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Rules 2, 6 and 14 are amended by [ADR-20260831-c7b](20260831-c7b-an-adapter-declares-the-states-and-the-permissions-and-the-host-places-them.md), which lets an adapter declare a permission on a device and grants it by a row of its own | Builds on [ADR-20260825-bhe](20260825-bhe-every-vocabulary-is-a-gated-published-resource.md), [ADR-20260826-6x5](20260826-6x5-a-namespace-is-legal-everywhere-it-is-used.md), whose rule 1 list rule 10 extends

Rules 6 to 13 and 15 to 17 follow from rules 1 to 5 and rule 14. Rules 2 and
14 are amended by
[ADR-20260831-c7b](20260831-c7b-an-adapter-declares-the-states-and-the-permissions-and-the-host-places-them.md).

Every extension point in this tree has a `Register`. The authorization action
vocabulary has none. `internal/authz/vocabulary.go:83` declares the actions as
Go constants and `internal/` is unreachable from `sdk/`, so a plugin has nothing
to call and nowhere to call it from.

Free bits in the mask are not what closes the gap. `actionSet` is a `uint32`
with 21 free bits. An action is a cell in a lattice: one bit from `Action.mask()`,
exactly one object type from `Action.domain()`, and a level in a four-value enum
whose masks are `level.printerMask()` and `level.resourceMask()`, two more
switches in the same file. A permission in no level's mask is conferred by no
relation, so no tuple carries its bit and no check passes on it. A name on its
own is therefore a permission that denies everybody, and somebody has to put it
in a level before it means anything.

So the question this record settles is who holds a permission the first time
anybody hears of it. The answer is the object's administrator, and rules
1 to 5 are that answer.

Earlier research measured what it costs. One action added to
`levelManager.printerMask()` moved an org admin from mask 1055 to 3103 on the
same printer, with nine tuples unchanged and no grant API call. `authz_epoch`
counts tuple writes, and no tuple was written. Rule 5 makes the registration
itself the write, so that movement is reported rather than silent.

## rules

1. A declared permission is held by whoever administers the object it is
   declared on. It is held from the moment the host registers the declaration.
   Nobody grants it and nobody places it.
   - **Retired by [ADR-20260901-b2z](20260901-b2z-driver-declared-permissions.md) rules 1 and
     4.** No adapter-declared permission *name* exists under that record. A
     driver declares a verb, the verb carries a class from a closed core set,
     and nothing is declared for a role to hold.
2. The administering level is the only level a declared permission enters, and a
   declaration names no level. The action per object type comes from
   `AdministerAction` at `internal/authz/grant.go:37`: `manage` on a printer,
   `configure_resource` on a resource kind. Both sit at `levelManager`. So a
   declaration widens one object type's manager mask and no other mask.
   - **Dropped by [ADR-20260831-c7b](20260831-c7b-an-adapter-declares-the-states-and-the-permissions-and-the-host-places-them.md)
     rule 10.** A declared permission enters the operator level too.
3. A declaration on an object type carrying no administering action is refused
   when it is declared. That covers `org`, `group`, `printer_group` and the four
   principal types. A permission declared on one is held by nobody, so the
   vocabulary would publish a name every surface denies.
   - **Moot under [ADR-20260901-b2z](20260901-b2z-driver-declared-permissions.md) rule 4.**
     There is no declared permission name to refuse on an object type. A
     declared verb enters an existing class.
4. A declared permission is conferred by the administering role and never on its
   own. Nothing changes on the grant path: `escalates` at
   `internal/authz/grant.go:522` still refuses a grant conferring an action the
   writer does not hold, and no relation below manager confers a declared
   action. Handing one to a second principal therefore means granting manager on
   that object, which is a grant an operator already reads.
   - **Retired by [ADR-20260901-b2z](20260901-b2z-driver-declared-permissions.md) rule 4.** A
     declared verb is conferred by whatever grant
     already carries its class, from operator upward, so nothing waits on the
     administering role. The dangerous tier is the one exception and it is
     conferred by no role at all.
5. Registering a declaration is a mutation of the graph, and retiring one is the
   same mutation in reverse. It runs through `Store.mutate` at
   `internal/authz/store.go:675`. So it bumps `authz_epoch` and recompiles both
   projections inside one transaction, and it answers with every principal whose
   mask it widened and every object it widened them on. That report is the only
   record that an administrator's authority grew.
   - **Retired by [ADR-20260901-b2z](20260901-b2z-driver-declared-permissions.md) rule 4.**
     The mechanism it governs goes with the declared permission name.
6. The declared region of `actionSet` is disjoint from the core region. Core
   actions keep their compiled bits and their compiled level masks. A declared
   action takes a bit above the core region, assigned at start-up in a stable
   order over the sorted qualified names, and a declaration past the 32-bit
   width is refused there. A bit is never assigned zero to mean absent, because
   `actionSet(1)<<32` is `0` and is indistinguishable from an action outside the
   vocabulary.
   - **Retired by [ADR-20260831-c7b](20260831-c7b-an-adapter-declares-the-states-and-the-permissions-and-the-host-places-them.md)
     rule 14.** Nothing assigns a bit over the sorted names. Rule 11 there puts
     a declared grant on a row of its own. Rule 12 compiles that row onto core
     action bits per device kind, so the assignment has no work left. One
     sentence of this rule survives, quoted by rule 17 there: a bit that moves
     reinterprets every row already written.
7. A declared permission that governs reading is enforced by `Visible` or
   refused when it is declared. `internal/authz/visible.go:82` and `:181`
   hardcode `ActionViewResource` and `ActionViewTelemetry`, and they read the
   read action per object type from the built table instead. A declared read
   permission finer than an `event.Origin` is refused at declaration, because
   `internal/transport/stream/hub.go:247` routes on `map[event.Origin]*channel`
   with no type predicate and no class predicate. A permission enforced on two
   of the three surfaces creates AGENTS.md's first standing suspicion by
   construction, and is worse than refusing the permission.
8. An authorization answer that cannot be computed is an error. `Visible`
   returns an empty visibility and a nil error for a principal whose projection
   row carries a bit naming no action, where `Can` and `ListPrinters` return
   `core.auth/UNKNOWN_ACTION`. The three surfaces answer alike.
9. A third-party permission is `namespace/action`: the ADR-20260826-6x5 namespace, `/`,
   and an action in the existing `[a-z][a-z0-9_]*`, so
   `com.github.aux.acme.mk4/read_filament_sensor`. The namespace is derived by
   `extension.NamespaceOf` from the declaring module and is never passed in. A
   core action carries no namespace and keeps its bare spelling, so the
   committed rows of `docs/vocabularies/authorization.json` do not move. An
   action never appears in a tuple, so `authz_relation_shape` and `authz_tuple`
   carry no change at all.
   - **This rule stands unamended.**
     [ADR-20260831-c7b](20260831-c7b-an-adapter-declares-the-states-and-the-permissions-and-the-host-places-them.md)
     rule 11 carries a declared grant in a table beside `authz_tuple`, so no
     action reaches a tuple and neither table moves.
   - **Retired by [ADR-20260901-b2z](20260901-b2z-driver-declared-permissions.md) rules 1 and
     24.** A declared verb is published in the per-build half of the verb
     table, not as a permission name in the authorization vocabulary.
10. The authorization action vocabulary carries a namespace within the meaning
    of ADR-20260826-6x5 rule 1, and this record adds it to that rule's list of three.
    ADR-20260826-6x5 rule 8 applies: the rendering is run through this vocabulary's own
    validator, `authz_action.name` on both engines, and `docsref.Anchor`, in a
    test, before it is chosen.
11. `authz_action` carries the declaration. It is the seam: `Reconcile` at
    `internal/authz/reconcile.go:72` already writes every action into it, and it
    is the one place a declaration can live where `Store.mutate` can write it
    inside the transaction that bumps the epoch. The projection compiler reads
    each declared action's object type from it.
    - **The retirement half of this rule was false for an action.** An action
      appears in no tuple. So a tuple check had nothing to count, and the row
      was deleted with no check at all. Earlier research measured it.
      `retireNames` at `internal/authz/reconcile.go:370` now takes a dependency
      hook. `actionDependants` refuses a retirement while this build still
      compiles a bit for the name, or an installed mapping still expands onto
      it. `retireShapes` at `:280` is the tuple half this rule described.
      [ADR-20260831-c7b](20260831-c7b-an-adapter-declares-the-states-and-the-permissions-and-the-host-places-them.md)
      rule 16 puts the revocation on the install, and the refusal stands beside
      it.
    - **Retired by [ADR-20260901-b2z](20260901-b2z-driver-declared-permissions.md) rule 4.**
      Nothing is held on declaration, so there is no first holder to name.
12. A plugin never writes a permission and never names the principal holding
    one. `sdk/authapi.Identity.Roles` is an ungated plugin-filled string set
    that nothing in `internal/` reads, and it is not the channel a declaration
    travels on.
    - **Retired by [ADR-20260901-b2z](20260901-b2z-driver-declared-permissions.md) rule 4.**
      With no declared permission name there is no name for an uninstall to
      withdraw. What an uninstall leaves is ruled by rules 27 to 29 there.
13. The relation vocabulary stays closed. Earlier research names an
    extensible relation vocabulary as the condition under which this project's
    own graph is the wrong engine. A declared action adds no edge, no traversal
    and no rewrite rule, and the list predicate stays `(actions & ?) <> 0`, so
    the engine decision is untouched.
    - **This rule stands too.** The declared grant table of
      [ADR-20260831-c7b](20260831-c7b-an-adapter-declares-the-states-and-the-permissions-and-the-host-places-them.md)
      rule 11 is chosen over opening the relation set. No relation is
      added and the predicate keeps one parameter.
14. A declaration naming a core object is refused. An extension declares on a
    resource kind it contributes and on nothing else, which is
    `resource_kind:<namespace>/<name>`. Rule 3 refused the types carrying no
    administering action; this refuses `device` as well, and refuses the core
    ids under `resource_kind`. Without it rule 1 hands a driver's permission to
    every device manager in the fleet, on machines that driver never touches.
    - **Opened for the device object by
      [ADR-20260831-c7b](20260831-c7b-an-adapter-declares-the-states-and-the-permissions-and-the-host-places-them.md).**
      An adapter declares a permission on `device`. A grant of it is a row
      naming one device of the declaring adapter's kind, which is rules 11 and
      34 there. The fleet-wide reach this rule refused is what rule 34 refuses:
      an org, a group and a device group take no declared grant.
15. A contributed resource kind is an authorization object id and never a
    `ref.Kind`. Nothing configures, constructs or supervises one, so it reaches
    none of the four switches that read `ref.Kind` and `ref.Kinds()` stays the
    compile-time table ADR-20260825-bhe rule 4 requires. `NewObject` takes the qualified
    id for `resource_kind` alone, so no second id space opens beside it.
16. The host derives the declaring namespace and the extension never passes one
    in. It is `extension.NamespaceOf` over the declaring module, which is rule
    9's derivation applied to the kind as well as to the action. A declaration
    naming a kind under another namespace is refused, because that kind's
    administrators would hold the permission.
17. A resource kind outside the closed set is refused as unknown, and never as a
    kind of the wrong class. `ref.Kind.Classify` reads one table total over
    `Kinds()` and reports `ErrUnknownKind` for anything else. `Configurable` and
    `Supervised` answered false to both cases, so a caller could not tell them
    apart, and two callers named a declared kind unknown.

## what this record does not claim

[ADR-20260825-bhe](20260825-bhe-every-vocabulary-is-a-gated-published-resource.md)'s rejected
row reads *"an action set a plugin can widen is a security surface a plugin
controls"*. That row and rule 1 look opposed and are not, and the difference is
rule 14.

ADR-20260825-bhe refuses a plugin adding to **the core's** action set. This record opens
declaration for a plugin's **own** behaviour, on an object only that plugin
contributes. Before rule 14 the two were the same thing, because every
declaration landed on `device` or on a core resource kind. After rule 14 a
declared action reaches no core object, and no core action moves, so ADR-20260825-bhe's
row stays rejected on its own terms.

## enforcement

The rules are accepted and none of the mechanism is built. The table names the
gate each rule needs and marks the two that exist today. Every owed gate is
watched failing on a deliberate mutation before it is trusted green, per the
house rule this project already applies to `conferred()`.

| rule | gate | state |
|---|---|---|
| 1 | `TestADeclaredPermissionIsHeldByTheObjectsAdministrator`, over one declared action and two principals | owed. Nothing declares today, so the rule is unbreakable |
| 2 | none | retired with rule 2 by ADR-20260831-c7b rule 10 |
| 3 | `TestADeclarationOnANonAdministrableTypeIsRefused`, over every type `AdministerAction` reports false for | owed |
| 4 | `TestADeclaredActionIsNotGrantableBelowManager` | owed. `escalates` is unchanged; the case over a declared bit is what is owed |
| 5 | `TestRegisteringADeclarationMovesTheEpochAndNamesWhatItWidened` | owed. Mutated by writing the declaration outside `Store.mutate`, which already bumps the epoch and builds both access maps |
| 6 | `TestEveryRelationConfersWhatItsSubjectReaches` and `cmd/generate-authz-vocabulary`'s `TestEveryNameReachesBothArtefacts` keep their hand-written tables and are restricted to the core region | exists. Observed green at 21 subtests under `go test -tags sqlite ./internal/authz/` on 2026-08-26 |
| 6 | none | retired with rule 6's bit assignment by ADR-20260831-c7b rule 14 |
| 7 | `TestADeclaredReadPermissionFinerThanAnOriginIsRefused` at declaration, and `TestVisibleReadsTheReadActionFromTheTable` | owed |
| 8 | `TestEverySurfaceRefusesABitThatNamesNoAction`, over the forged projection row earlier research built | owed |
| 9, 10 | ADR-20260826-6x5 rule 8's shape: one module path through `NamespaceOf` to a qualified action, asserted as an exact string against `authz`'s validator, the primary key on both engines, and `docsref.Anchor` | owed |
| 11 | `internal/arch`'s two import gates hold the direction the table is built in, and `TestRetiringAnActionThisBuildStillCompilesIsRefused` holds the removal | exists |
| 12 | none. The field is unread, so a gate over it asserts an absence no code produces. Deleting it from `sdk/authapi/provider.go` is the enforcement |
| 13 | none needed while `relationSpecs` is a composite literal the compiler checks |
| 14 | `TestADeclarationOnACoreObjectIsRefused`, over every type | exists. It asserts the correction the message carries, because a later check refuses the same input with worse advice |
| 14 | `TestADeclarationOnACoreResourceKindIsRefused`, over every `ref.Kinds()` entry | exists |
| 14 | `TestADeclarationOnAContributedKindIsAccepted`. Refusing everything passes rule 14 and delivers nothing | exists |
| 15 | `TestOnlyAResourceKindTakesAQualifiedID` over every type, and `TestAContributedKindIsNotAConfigurableInstance` | exists |
| 16 | `TestADeclarationOnAnotherExtensionsKindIsRefused` | exists |
| 17 | `TestEveryKindCarriesOneClass` and `TestClassifySeparatesAnUnknownKindFromADeclaredOne` in `internal/kernel/ref` | exists |

## rejected

Each heading below is the option, unchanged, so a reviewer can link to the
argument rather than repeat it.

### the declaring extension names the level that holds its permission

Rule 2 refuses the choice, not the automatic conferral. A level an extension
picks reaches a viewer, and a viewer is somebody who installed nothing. An
administrator is the principal who chose to run the extension, which is the
whole reason rule 1 lands there and nowhere lower.

This is also OctoPrint's `default_groups` and Kubernetes' `aggregate-to-admin`,
and both need admission control, audit and a named escalation verb to make an
author-chosen level safe. This project has one binary and a SQLite file.

### an operator places each declared permission in a level at install

The record proposed it, and it is rejected. It buys one confirmation and
costs a surface that does not exist. That surface is a grant written on a
permission rather than on an object, with an authority to write it that is not
"hold the permission already". Until somebody builds it, every check in the
driver denies. An administrator who installed the extension is the same person
who would have confirmed the placement.

### a plugin calls `authz.RegisterAction` from `init`

ADR-20260824-kkr forbids the import in both directions, and ADR-20260825-bhe rule 4 refuses
`init`-time registration for a core-owned set. Earlier research measured
that shape at +1.84 MB heap and +5.16 MB RSS, against 0 bytes for a built table.

### every declared permission starts held by nobody, with no other change

It was executed as the `agent` relation, which is already reachable from no
role. The answer was `ACCESS_DENIED: a grant may not confer an action the
caller does not hold`, from a caller holding the whole manager mask. It needs a
minting path per permission, which is a second grant system.

### an `escalate`-style override on the grant path, as Kubernetes has

It defeats `escalates`, `mayRemove` and `mayStandDown` at once, and those three
are the whole safety of the grant surface. Rule 4 keeps all three by conferring
a declared permission through the administering role, so the grant path sees an
ordinary manager grant and needs no override.

### carry the level inside the action's own name

Rule 2 fixes the level at the administering one, so a level in the name would
be the same word on every declared action. It also spends part of the name on a
fact the vocabulary already publishes per object type.

### namespace the action and keep the set closed

Earlier research's rejected table: a namespace on a closed set is decoration that
implies an extension point exists.

### accept the fleet-wide reach and say so

The other half of the question, and it is rejected.
Rule 1 confers on the administering role, so a driver for one printer model
would hand its permission to every device manager already written, service
accounts and nodes included. The permission would then be held on machines the
driver never touches, and the operator's only control would be whether the
extension is installed at all.

### open `ref.Kind` so a contributed kind is a resource kind

Earlier research proposes
it: a module kind that is addressed and listed, never configured, constructed or
supervised. That is the right answer for **addressing**, where a module needs
`spool:reel-01` in an event envelope and a health key. It is the wrong answer
for **authorization**, and rule 15 takes the narrower path for three reasons.

`ref.Kinds()` would stop being a compile-time table, which ADR-20260825-bhe rule 4
refuses and only the owner may amend. The published `resource-kinds` vocabulary
would become per-deployment, so `make resource-kinds-check` and
`GET /api/v1/vocabularies/resource-kinds` would answer differently on two
installs of one build. And the four switches would each need an explicit branch
for a value they can never construct. An authorization object id needs none of
that: `resource_access.kind` is `TEXT` on both engines.

A tuple on a kind outside the projection's id space compiles to no row. That is
not evidence the id space can stay closed. It is why a declared permission
reaches nobody, and earlier research executed it: the org admin `Bootstrap` wrote
is refused on the plugin's own object. Rule 1 of
[ADR-20260828-xdp](20260828-xdp-a-contributed-resource-kind-is-a-grantable-object.md) opens that
id space and leaves `ref.Kinds()` closed, so the three grounds above still
hold.

The two are not in conflict. Earlier research stays open for the addressing
half, and this record takes no position on it.

### give an extension an object type instead of an action

The larger change. `Type` is closed, `authz_relation_shape` is a composite
foreign key over it, `AdministerAction` maps a type to its administering
action, and `relationSpecs` would need a role layer per type. It stays open.

### per-event authorization on the hub, so a stream permission can be finer than an origin

`internal/transport/stream/hub.go:136` records the decision as one per
subscription, costing O(subscribes per hour) instead of O(events times
subscribers). Reversing it pays E10's cost for a permission not yet declared.

### refuse any declared permission a surface cannot enforce, writes included

The stream enforces no write action today and never did, so the rule would
refuse `send_command`. Rule 7 scopes the refusal to reads, which is the only
granularity the third surface carries.

### swap the graph for OPA or embedded OpenFGA now that the vocabulary opens

Earlier research measured it on the only single-binary backend: 9.0 ms
per point check, and 3.0 s per 400-printer list. 116 of 400 rows came back
silently wrong. An open action set changes none of those numbers.

### use `authapi.Identity.Roles` as the channel

It is the widest possible form of letting the plugin choose: ungated,
unnamespaced, unpublished, and plugin-controlled end to end.

### leave the set closed and tell driver authors to reuse `send_command`

Defensible for verbs, and ADR-20260825-z65 already separates `send_gcode` on that
argument. It is not defensible for a permission over a thing the core has no
concept of, which is a design requirement's case.

### widen `actionSet` to `uint64` in the same change

21 bits are free and no declaration exists yet. It is a migration on two
engines plus the projection scan in `grant.go`, and it is the change for the
day 21 is not enough.

### gate the declared region with a hand-written mask table, as the core region is gated

The table cannot be written, because the set does not exist at compile time.
Rule 6's split keeps the hand-written table on the region where it still has a
compile-time literal to compare against.

## open items

- **A declared permission is validated and is held by nobody.** Rule 14's
  refusals are built and rule 6's bit assignment is not. So `Declare` returns a
  qualified name that reaches no mask, no projection row and no check.
  `configurableKinds` in `internal/authz/projection.go:112` is the projection's
  id space and it is `ref.ConfigurableKinds()`, so a tuple on a contributed kind
  compiles to no row. That id space is the outer lock and rule 6's bit is not,
  which earlier research executed and
  [ADR-20260828-xdp](20260828-xdp-a-contributed-resource-kind-is-a-grantable-object.md) rules 1
  and 10 rule on. The tasks are on the old backlog.
- **No surface calls `Declare`.** `sdk/extension.Doc` carries no permission
  list, so an extension has nothing to declare through and the composition root
  has nothing to pass. Rule 16's namespace derivation is proved against a
  literal namespace rather than against a real module path.
- Nothing writes the administering tuple for a contributed kind. `Bootstrap`
  writes `RelOrg` for each of `ref.ConfigurableKinds()`, so a contributed kind
  starts with no administrator and rule 1 confers on nobody until somebody
  writes a grant.
- Rule 15 keeps `ref.Kinds()` closed, so a contributed kind is an authorization
  object and is not addressable. A module that needs `spool:reel-01` in an event
  envelope or a health key still needs earlier research.
- The namespace grammar is now spelled in three places: `sdk/extension`,
  `sdk/event` and `internal/authz/declaration.go`. Rule 10 asks for this
  vocabulary's own validator, and `sdk/fault` and `sdk/event` each hold theirs
  for the same reason. So the third copy is the house shape, and it is still a
  third copy.
- Nothing bounds who may check a declared action. `Can` takes an action string,
  so a second extension can check a permission the first declared and attach its
  own behaviour to that name. The published entry names the declaring namespace
  and nothing enforces it.
- `manager` is not one person. `grantable` admits `user`, `group#member`,
  `service_account` and `node`, and an extension's own event sink is
  `service_account:<instance id>` (`internal/app/sinks.go`). Every manager
  tuple already written widens under rule 1, sinks and nodes included.
- No operator surface refuses a declaration. Under rule 1 the only control is
  whether the extension is installed at all, and `install.Service` carries a
  path, a checksum and a contract major with no permission list to approve.
- `AdministerAction` refuses `org`, so a deployment cannot add a second
  administrator after `Bootstrap`. Rule 1 hands a declared permission to the
  administrators a deployment has, in a product with no handover for the
  bootstrapped account.
- Nothing in the binary calls `Store.Rebuild`. A widened level mask therefore
  lands on the next unrelated grant write, because `mutate` reprojects the whole
  graph, so the epoch does move and it names the wrong cause. Rule 5 makes the
  registration itself the write; until that is built, a vocabulary change and a
  tuple change are indistinguishable from the epoch.
- ADR-20260825-bhe rule 5 requires enforcement before shipping and rule 4 refuses a
  table assembled at start-up. Rule 6's declared region is both. This record
  does not overturn either: a set a deployment assembles has no earlier moment
  than its start, and the refusal is a refusal to serve. Rule 15 removes the
  same conflict for resource kinds and leaves it standing for the action set,
  so the reconciliation is still a human's amendment.
- A design requirement asks for roles extensible instead of an enum in the core, and
  `level` is a four-value enum with two fixed role tables. This record opens the
  action set and leaves the role layer closed, so the requirement needs the
  reading it means written down. Earlier research is not this record's to edit.
- The widening report in rule 5 is the operator's only defence against a name
  that implies less than the permission does. OctoPrint's `dangerous` field is
  the prior art for that problem and it is declared prose with no enforcement.
  Nothing catches a misleading summary.
- Earlier research's upgrade attack was executed against SQLite alone.
  `printer_access.actions` is `INTEGER` there and `BIGINT` on Postgres, and the
  mechanism is the projection compiler rather than the column, so the Postgres
  result is a hypothesis.
