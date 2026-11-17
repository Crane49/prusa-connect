---
title: "ADR-20260828-xdp: a contributed resource kind is a grantable object"
description: "The projection's resource id space opens to the kinds installed modules declare, the host writes the tenancy tuple, the row count is bounded, and a declared action still takes no bit."
date: 2026-08-28
status: accepted
tags:
  - adr
  - authorization
  - extensibility
  - modules
  - security
---
# ADR-20260828-xdp: a contributed resource kind is a grantable object

- status: accepted, and every gate is owed
- date: 2026-08-28

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) · Builds on [ADR-20260825-bhe](20260825-bhe-every-vocabulary-is-a-gated-published-resource.md), [ADR-20260827-rqc](20260827-rqc-an-extension-declares-a-permission.md) · Reads with [ADR-20260827-qvk](20260827-qvk-a-published-version-is-refused-or-it-is-not-published.md), [ADR-20260827-wmv](20260827-wmv-a-module-is-mounted-and-an-adapter-is-constructed.md), [ADR-20260827-rwq](20260827-rwq-a-module-keeps-its-data-out-of-the-host-schema.md)

[ADR-20260827-rqc](20260827-rqc-an-extension-declares-a-permission.md) says who holds a
permission an extension declares. Nothing holds one, because a grant written on
a contributed kind reaches no projected row.

Earlier research
found the lock, and it is not the action bit everyone expected. A stored grant
is a tuple naming a relation, and no tuple ever names an action. Both tables
carrying an action mask are deleted and rewritten inside every mutation, and
`Store.Reconcile` rebuilds before the process binds a port. So a bit is a cache
key inside one build.

The lock is one slice of strings. `configurableKinds` in
`internal/authz/projection.go:112` is the id space the resource projection is
compiled over. Earlier research executed a grant on
`resource_kind:com.github.acme.mk4/filament-sensor`: 0 rows over that list, and
2 rows carrying the manager mask once the id is in it. This record opens that
list and rules on what follows.

## rules

1. Compile the resource projection over the core configurable kinds and over
   every contributed kind an installed module declares. `ref.Kinds()` stays the
   compile-time table
   [ADR-20260825-bhe](20260825-bhe-every-vocabulary-is-a-gated-published-resource.md) rule 4 and
   [ADR-20260827-wmv](20260827-wmv-a-module-is-mounted-and-an-adapter-is-constructed.md) rule 8
   require, and no switch that reads it gains a branch.
2. Read the contributed half of that id space from the set the host installed,
   and never from a running module. The overlay record
   [ADR-20260827-rwq](20260827-rwq-a-module-keeps-its-data-out-of-the-host-schema.md) rule 10
   writes is the source, so `Store.Reconcile` builds the same id space before
   the port binds that the install mutation built.
3. Give a contributed kind the five resource actions a core kind carries and no
   mask of its own. `levelManager.resourceMask()` is 7040 on both, so a manager
   on a contributed kind holds `view_resource`, `operate_resource`,
   `configure_resource`, `read_storage` and `write_storage`.
4. Write a contributed kind's `#org` tenancy tuple from the host at install, and
   refuse a module that names an org. `Bootstrap` writes one `RelOrg` tuple per
   configurable kind and refuses to run twice, so a kind installed later takes
   its tuple from the install mutation.
5. Bound the contributed id space at 8 kinds per namespace and 16 per
   deployment, and refuse the install that passes either. Earlier research
   measures 11.6 µs per projected row. Sixteen kinds then add about 37 ms to a
   grant write on a 202-principal fleet, against 170 ms for one write and
   withdrawal today. Raise the bound when per-principal incremental recompute
   lands and beats those numbers.
6. Retain a grant on a kind no installed module declares, and delete no tuple
   when a module is uninstalled. The kind leaves the id space, so the tenancy
   tuple and the grant both compile to no row and confer nothing.
7. Refuse to mount a module whose declared permission set changed until an
   operator's explicit act accepts the new set. A contributed kind id carries no
   version, so a surviving grant applies to whatever version two says the kind
   means.
8. Split the served authorization vocabulary into a generated half and a
   per-install half, and gate the generated half alone. A declaration is
   per-install, so it adds no row to `docs/vocabularies/authorization.json` and
   moves no content version under
   [ADR-20260827-qvk](20260827-qvk-a-published-version-is-refused-or-it-is-not-published.md)
   rule 1.
   - **The split has its first per-install half, and it is another
     vocabulary.** `GET /api/v1/vocabularies/device-states` serves the declared
     state sets of [ADR-20260831-c7b](20260831-c7b-an-adapter-declares-the-states-and-the-permissions-and-the-host-places-them.md)
     rule 9. It carries a format version and no content version, and
     `internal/kernel/devicestate` holds the shape with none of the values. The
     authorization half this rule names is still owed.
9. Accept a qualified resource kind id on the grant surface, at 160 bytes.
   `GrantRef.ObjectID` in `internal/transport/http/httpapi/grants.go` caps at 63
   and documents a DNS-1123 label. 160 is 96 for the namespace, one separator
   and 63 for the name, which `maxActionNameLen` in
   `internal/authz/declaration.go` already names.
10. Assign a declared action no bit while
    [ADR-20260827-rqc](20260827-rqc-an-extension-declares-a-permission.md) rule 2 fixes its
    level at the administering one. A change to that rule is the only thing
    that reverses this one.
    - **That change was taken.** [ADR-20260831-c7b](20260831-c7b-an-adapter-declares-the-states-and-the-permissions-and-the-host-places-them.md)
      rule 10 drops ADR-20260827-rqc rule 2, so a declared permission reaches the
      operator level. This rule
      is reversed on its own terms. Rules 11 and 12 there are the replacement:
      a declared grant is a row, compiled onto core action bits per device
      kind.

## a declared bit adds no authority

Rule 10 follows from ADR-20260827-rqc rule 2 and from nothing else. Under that rule a
declared permission enters the administering level and no other level.
`AdministerAction` returns `configure_resource` for a resource kind, and that
action sits at `levelManager`.

So a declared action is held by exactly the principals already holding
`configure_resource` on that kind. The same relations confer it, the same grant
writes it, and the same withdrawal removes it. The two can never separate.

What a bit buys is a name in the published vocabulary, a refusal that says which
operation was denied, and a token the module's own handler branches on. Those
are worth having. They are not worth a bit-allocation scheme, a second grant
table or a per-install mask, while rule 2 stands.

## enforcement

| rule | gate | state |
|---|---|---|
| 1 | `TestAGrantOnAnInstalledContributedKindCompilesToARow`, over one installed kind and the org admin `Bootstrap` wrote | owed. Nothing installs a kind, so the rule is unbreakable today |
| 2 | `TestTheProjectionIDSpaceComesFromTheInstalledSet`, mutated by handing it a declaration a running module supplied | owed |
| 3 | `TestAContributedKindTakesTheCoreResourceMask`, asserting 7040 at manager on a core kind and a contributed one | owed |
| 4 | `TestOnlyTheHostWritesTheTenancyTupleForAContributedKind` | owed. `Declare` refuses a core object already, and no path writes the tuple at all |
| 5 | `TestTheNinthContributedKindInANamespaceIsRefused`, and the deployment case at 17 | owed |
| 6 | `TestAGrantOnAnUninstalledKindSurvivesAndConfersNothing` | owed. `retireNames` gained a dependency hook, and `actionDependants` refuses the retirement earlier research measured |
| 7 | `TestAModuleWithAChangedPermissionSetDoesNotMount` | owed. `install.Service` carries a path, a checksum and a contract major, and no permission list |
| 8 | `make authz-vocabulary-check`, both directions over the generated half | exists for the generated half. This vocabulary's per-install half is unserved and ungated |
| 9 | `TestTheGrantSurfaceAcceptsAQualifiedKindID`, and `make api-check` over the widened schema | owed |
| 10 | none. No bit is assigned, so a gate would assert an absence no code produces | none |

## rejected

### a declared mask scoped to the contributed kind

This is the option that returns the day ADR-20260827-rqc rule 2 changes, and it is
option B in earlier research. It gives every namespace its own ceiling above bit 13,
so a mask means something only beside the `kind` column on the same row.
`resource_access` already carries that column, and Windows `ACCESS_MASK` is the
one surveyed design that splits a mask by object owner.

It loses today for one reason: rule 2 makes the bit confer nothing
`configure_resource` does not already confer. Its cost is a runtime bit table
where the core region has a compile-time literal, which ADR-20260825-bhe rule 4 refuses
and only the owner may amend. Buying that for a name in a vocabulary is the
wrong trade while the authority is identical.

Take this and not a global bit if rule 2 moves. It survives an uninstall without
disturbing another module, and it has no shared ceiling.

| option | why not |
|---|---|
| one global appended bit, as ADR-20260827-rqc rule 6 is written | two binaries on one database compile different assignments into one projection |
| a string-keyed grant table beside the projection | rows multiply by actions per kind, and the point check gains a join. No declaration exists to need it |
| leave a module authorizing on its own instance grant | a module's configuration and the objects it manages would share one grant |
| assign declared bits globally at start-up over sorted names | the same two-binary hazard as the global bit, arriving silently on the check path |
| refuse to retire an `authz_action` row while a tuple names it | no tuple names an action, so the check can never fire. The dependency it asserts does not exist |
| version the contributed kind id, as `namespace/name@2` | every grant becomes version-scoped, so an upgrade drops standing with nothing reporting it |
| let the module declare which org its kind belongs to | the module would choose whose administrators inherit its permission |
| generate the declared half into the committed vocabulary artefact | the artefact is per-build and a declaration is per-install, so the gate fails on any machine with a module installed |
| cascade-delete a grant when a module is uninstalled | Jenkins does the equivalent and loses grants across a reinstall |
| widen `actionSet` to `uint64` in this change | 18 bits are free and no declaration exists. It is a migration on two engines for a ceiling nothing reached |
| aggregate a module's permission into the core admin role, as Kubernetes does | it widens a grant already written with no tuple change, so nothing records the widening. Earlier research executed it |
| open `ref.Kind` so a contributed kind is a resource kind | ADR-20260827-rqc rule 15 and ADR-20260827-wmv rule 8 refuse it, and rule 1 opens a `[]string` instead |

## open items

- **Rule 10 depended on ADR-20260827-rqc rule 2, and that rule is dropped.**
  [ADR-20260831-c7b](20260831-c7b-an-adapter-declares-the-states-and-the-permissions-and-the-host-places-them.md)
  rule 10 drops it. A module may hold a permission below the manager level. So rule 10
  here is wrong on the condition it named, and a declared bit would confer what
  `configure_resource` does not. ADR-20260831-c7b rules 11 and 12 carry the
  replacement: a row per declared grant, mapped onto core action bits per
  device kind. Nothing else in this record moves.
- Every gate in the table is owed except the generated half of rule 8. Nothing
  installs a contributed kind, so no rule here can be broken by the code as it
  stands.
- Rule 5's bound is written down and not built. `Store.reproject`'s own comment
  names per-principal incremental recompute as the next step, and earlier research
  holds the numbers it has to beat.
- Rule 2 has no id-space seam yet. Earlier research gap 4 names it: the note measures
  the compiler around the seam and builds no wiring from an installed module.
- Every measurement behind rule 5 is SQLite. Earlier research gap 1 records the
  Postgres half as a hypothesis, on one batched insert statement per engine.
- Rule 8 needs a runtime half of `GET /api/v1/vocabularies/authorization`.
  `actionProse` in `internal/authz/publish.go` is a static composite literal
  held in `Actions()` order by a test, so a declared action can carry no row in
  it.
- Nothing bounds who may check a declared action, which ADR-20260827-rqc's open items
  already record. Rule 1 makes the permission reachable and leaves that open.
