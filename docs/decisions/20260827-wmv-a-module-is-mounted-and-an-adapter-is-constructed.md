---
title: "ADR-20260827-wmv: a module is mounted and an adapter is constructed"
description: "Where an adapter ends and a module begins, what a module may add to a set the core owns, which module shape runs a process, and why a customer's module needs no rebuild of this binary."
date: 2026-08-27
status: accepted
tags:
  - adr
  - extensibility
  - extensions
  - vocabulary
  - api-compatibility
---
# ADR-20260827-wmv: a module is mounted and an adapter is constructed

- status: accepted, rule 5 is gated and four rows are owed
- date: 2026-08-27

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Builds on [ADR-20260825-n55](20260825-n55-adapters-are-reference-implementations.md), [ADR-20260825-rre](20260825-rre-one-registration-rule-and-one-descriptor.md), [ADR-20260825-bhe](20260825-bhe-every-vocabulary-is-a-gated-published-resource.md), [ADR-20260827-rqc](20260827-rqc-an-extension-declares-a-permission.md) | Reads with [ADR-20260827-rwq](20260827-rwq-a-module-keeps-its-data-out-of-the-host-schema.md), [ADR-20260827-2bp](20260827-2bp-a-module-travels-as-a-verified-artefact.md)

Three modules are named: the UI, a job planner and the Connect adapters. Nothing in the core mounts one. `sdk/routesapi` is the only
surface that lets outside code add an HTTP route, its one wiring is
`legacy/app/app.go:1521`, and that wiring mounts contributed routes on the
ungated router.

His test selects the mechanism: *"pluggable bits basically that extend the
core, but cannot break it."* Containment is the property, and rules 5 and 6 are
where it lands.

## rules

1. Build an adapter where the core owns every noun the thing touches. Build a
   module where it brings a noun the core has no schema for. An adapter fills a
   contract the core published; a module is a feature the host mounts. This is
   also the line a build falls on. An adapter needs no build, and a module that
   extends the core does.
2. Apply rule 1 with three questions. Does the thing need a row the core has no
   schema for, a screen of its own, or a permission the core does not define?
   One yes makes it a module, and three noes make it an adapter under
   [ADR-20260825-n55](20260825-n55-adapters-are-reference-implementations.md).
3. Ship a module as an artefact this binary does not contain. A rebuilt host
   binary per customer module is refused: ten optional plugins take the tag
   matrix to 794,624 binaries, and a requirement makes an air-gapped install the
   normal deployment.
4. Keep a printer driver an adapter and never a module. It fills
   `connectorapi.Set`, a contract the core wrote, so rule 1 places it. Which
   runtime a driver may take is an open question, and this record takes no
   position on it.
5. Run a module that executes as `runtime: service`. A module compiled into this
   binary is an adapter that named itself wrong, because ADR-20260824-nyw tier 1 shares
   the address space and containment is unavailable there.
6. Serve a module that executes nothing from the host's own code. Its artefact
   is data, its digest is verified at install, and no process is launched for
   it. The UI is that shape, and this binary serves it.
7. Derive every name a module adds from the namespace `extension.NamespaceOf`
   computes. This is [ADR-20260825-rre](20260825-rre-one-registration-rule-and-one-descriptor.md)
   rule 4 applied to routes, event types, error domains and settings.
8. Refuse a resource kind a module mints. `ref.Kinds()` stays the compile-time
   table [ADR-20260825-bhe](20260825-bhe-every-vocabulary-is-a-gated-published-resource.md) rule
   4 requires, and
   [ADR-20260826-5yj](20260826-5yj-a-device-is-the-object-and-a-printer-is-the-type.md) rule 12
   already refuses a minted kind. Both are the owner's.
9. Address a module's own objects inside the module's own subtree, and carry the
   object id in the event envelope's `subject`. A module's event names the
   module instance's own `ref` as its origin.
10. Declare a module's permissions on `resource_kind:<namespace>/<name>`, which
    is [ADR-20260827-rqc](20260827-rqc-an-extension-declares-a-permission.md) rules 14 to 17. A
    module declares no object type, no relation and no level.
11. Add an event type and an error domain under the module's own namespace, and
    add nothing to a set the core owns. `sdk/event.ValidateType` and
    `sdk/fault.NewDomain` already hold that split.
12. Serve a module's HTTP surface under a group the host gives it, at
    `/api/v1/modules/{namespace}/`. Where no namespace derives a path, an
    operator names the mount and the host refuses a second claim on it.
13. Authorize a module's route the way the rest of `/api/v1` is authorized. A mode
    of the retired server shipped on the ungated router, and that is the shape this rule refuses.
14. Add no property to a schema the core publishes. A module serves its own
    subtree, so uninstalling it leaves `/api/v1` unchanged and costs no breaking
    change under [ADR-20260825-zkq](20260825-zkq-api-compatibility.md) rule 4.
15. Version a module against `CoreContract`, which
    [ADR-20260827-qvk](20260827-qvk-a-published-version-is-refused-or-it-is-not-published.md)
    rule 8 defines over the published sets and the wire method set. A module
    declares a range against that one number, and this record mints no second
    one.
16. Refuse a `contract_major` this host does not serve before anything executes,
    which [ADR-20260827-7jn](20260827-7jn-the-plugin-wire-is-a-specification.md) rule 11 holds
    at the handshake.
17. Keep a module's settings inside its own instance block. A module declares no
    key in a block the core decodes, so ADR-20260825-bhe rule 14's exemption stays true
    and the strict decoder keeps refusing what it does not know.

## why a module's event names its instance

The stream enforces rule 9 and could not enforce an addressable kind.
`VisibleOrigins` in `internal/app/stream.go` renders every device and every
configured instance the principal may observe, and a subscription is allowed
those origins alone. An origin that walk cannot render reaches a channel no
subscriber may attach to, so a minted origin kind would publish into silence.

An instance already has a `ref`, a grant on `resource_kind`, a health key and a
place in the resource listing. A module's object id in `subject` therefore rides
a channel the graph already authorizes, and the module's own subtree answers
the question a reader asks next.

## the three modules this record carries

| module | shape | what it needs | rules |
|---|---|---|---|
| the UI | assets and a mount | a directory, a digest, a path an operator names, and this binary serving it | 6, 12 |
| a job planner | a feature pack | rows, routes, screens, a permission | 5, 10, 12, 14 |
| the Connect adapters | a compatibility shim | rows, routes, its own event type, removal | 9, 11, 14 |

The UI can start first. It needs no wire, no rows, no permission and no change
to a published set, so rules 6 and 12 are its whole dependency.

The Connect label shim turns on rule 14. Connect reads printer labels off the
device record today, and
[ADR-20260826-5yj](20260826-5yj-a-device-is-the-object-and-a-printer-is-the-type.md) rule 10
gives the core a string map with no identity in it. A module that decorated
`GET /api/v1/devices` with the shipping shape would make its own removal a
breaking change to `/api/v1`. So Connect is pointed at the module's own subtree,
and the day it stops asking the module is uninstalled with nothing else moving.

## enforcement

Most of these rules are still unbreakable and carry no gate, because nothing
mounts a module's routes and nothing serves an asset module. Each owed gate is
watched failing on a deliberate mutation before it is trusted green, which is
the practice [ADR-20260826-6x5](20260826-6x5-a-namespace-is-legal-everywhere-it-is-used.md)
already applies.

Rule 5 was adopted on 2026-08-28 and closed all three doors onto a declaration:
the operator's file, the machine-owned overlay and the resource API. A module
type declared `builtin` fails its own instance and the host keeps serving.

| rule | gate | state |
|---|---|---|
| 1, 2 | review. It is a judgement over a design, on ADR-20260826-5yj rule 2's shape | none possible |
| 3, 5 | `TestAModuleTypeIsRefusedAsBuiltin`, and `TestAModuleDeclaredAsBuiltinNeverReachesTheSupervisionPlane` | exists |
| 4 | review, on rule 1's test. `ref.KindRuntimes` holds the runtime half | none |
| 6 | `TestAnAssetModuleIsVerifiedAndNeverLaunched` | owed |
| 7 | `TestTheRegistrantIsTheFirstFrameOutsideTheWrappers` | exists |
| 8 | `make resource-kinds-check`, both directions | exists |
| 9 | `TestAModuleEventNamesItsInstanceAsTheOrigin` through the hub | owed |
| 10 | `TestADeclarationOnACoreObjectIsRefused` and its three siblings | exists |
| 11 | `make event-types-check` and `make error-codes-check` | exists |
| 12, 13 | `TestAModuleRouteIsMountedUnderItsNamespaceAndAuthorized` | owed |
| 14 | `make api-check` over a baseline no module can move | exists |
| 15, 16 | `CoreContract`, which ADR-20260827-qvk rule 8 owes | owed |
| 17 | the strict decoder in `internal/app/resource/load.go` | exists |

## rejected

| option | why not |
|---|---|
| a rebuilt binary per module, as the owner assumed | ours build that way and a customer cannot. See [the rebuild half](#a-rebuild-is-ours-and-never-a-customers) |
| a module as a fourth runtime beside builtin, service and script | a module is a feature and a runtime is how a type is reached. Two of the three shapes already fit |
| a compiled-in module, contained by a detector | every in-process ecosystem surveyed built a detector and none built a barrier |
| `ref.Kind` opened for a module kind | see [the kind half](#a-module-kind-in-refkind) |
| a version per module surface, so each set moves alone | a module author would track seven numbers, which ADR-20260827-qvk already refuses per vocabulary |
| a module that adds a field to a core record | its removal is then a breaking change, which is the one outcome this record refuses |
| `routesapi` handing a module the base router | it reaches every path the host serves, which is how a mode of the retired server bypassed authorization |
| a module declaring a config key in a core block | ADR-20260825-bhe rule 14 rests on a decoder that refuses what it does not know |

### a rebuild is ours and never a customer's

`cmd/detect-build-tags` derives build tags from the declared instances, so a
first-party type compiles in and ships in a named variant. That stays.

A customer's module cannot take the same path. Earlier research measured a cold build
at 1,480 MiB of peak resident memory and 1.35 GiB of disk, which a 1 GB Pi
cannot link. The shipped tag matrix is 776 binaries, and ten optional plugins
take it to 794,624. Caddy's hosted build service is the one answer to that, and
an air-gapped customer cannot reach it.

### a module kind in `ref.Kind`

Earlier research proposes
a fourth class: a kind that is listed and addressed, and never configured,
constructed or supervised. Rule 8 refuses it, and the reason is whose rules it
would move.

ADR-20260825-bhe rule 4 and ADR-20260826-5yj rules 8 to 13 are both the owner's, and an agent
may not reverse either. Opening the set would make `ref.Kinds()` a table
assembled at start-up, and it would make `GET /api/v1/vocabularies/resource-kinds`
answer differently on two installs of one build. ADR-20260827-rqc rule 15 took the
narrow path for the same reason on 2026-08-27, and this record agrees with it.

Rule 9 is what covers the need earlier research named. The addressing it asked for
was an event origin, a health key and a listing entry, and a module instance
already carries all three.

## open items

- No contract publishes a wire method set, so rule 5 describes a runtime that
  launches a module and cannot call it. ADR-20260827-7jn's open items own that gap and
  the old backlog carries the work.
- Nothing in the core mounts a `routesapi` registration, so rules 12 and 13 have
  no code to change until the core grows the wiring `legacy/` has.
- `CoreContract` does not exist. ADR-20260827-qvk rule 8 waits on its own rule 4, and
  three vocabularies publish an empty version, so rules 15 and 16 bind a number
  the build does not compute yet.
- An asset module has no kind, no mount and no install record, so rule 6 names a
  shape the core cannot serve. The tasks are on the old backlog.
- Rule 12 refuses a second claim on one mount and no surface writes a mount
  today, so the refusal has nothing to refuse.
- The Connect label shim needs a word for its noun that is not `label`, because
  ADR-20260826-5yj rule 10 gives the core that word. This record states the constraint
  and picks nothing.
- Rule 17 keeps a module's settings opaque to the core, so a UI cannot render a
  module's settings form from a schema the host holds. ADR-20260825-rre rule 10's config
  key descriptors are the surface that would, and a module declares none.
- The UI bundle needs a build and the module class it belongs to is the one the
  owner put on the build side on 2026-08-28. Who runs that build is unsettled: he
  said a build is needed and never said the host runs one, and earlier research
  measured a cold build of this binary at 1,480 MiB of peak memory. The task is
  on the old backlog.
