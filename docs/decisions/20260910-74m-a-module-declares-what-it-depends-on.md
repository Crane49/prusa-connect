---
title: "ADR-20260910-74m: a module declares what it depends on"
description: "A module or adapter names what it needs in its descriptor, by qualified type, capability key and minimum core version, and the host does not start it without them."
date: 2026-09-10
status: accepted
tags:
  - adr
  - extensibility
  - modules
  - adapters
  - sdk
---
# ADR-20260910-74m: a module declares what it depends on

- status: proposed
- date: 2026-09-10

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Builds on [ADR-20260825-rre](20260825-rre-one-registration-rule-and-one-descriptor.md), [ADR-20260825-dee](20260825-dee-the-capability-vocabulary.md), [ADR-20260831-nbj](20260831-nbj-a-module-declares-a-device-kind-and-the-host-places-it.md), [ADR-20260831-c7b](20260831-c7b-an-adapter-declares-the-states-and-the-permissions-and-the-host-places-them.md), [ADR-20260901-7t4](20260901-7t4-build-time-adapter-composition.md)

The descriptor carries `Requires`, a list of capability keys
(`sdk/extension/descriptor.go:357`), and only the reference generator reads it
(`cmd/generate-extensions/reference.go:99`). The condition kernel names
`DependenciesSatisfied` (`internal/kernel/condition/condition.go:51`), and
nothing writes it. This record joins the two.

## rules

### what a module may depend on

1. A module declares its dependencies, and the host does not run it without
   them.
2. A dependency on another module or adapter names it by qualified type,
   `namespace/name` (`sdk/extension/descriptor.go:413`). The instance id is
   the operator's (`internal/kernel/ref/ref.go:284`) and the UID is minted
   per install (`internal/app/resource/model.go:96`), so an author knows
   neither. The host derives the type from the module path
   (ADR-20260901-7t4 rule 8, `internal/app/resource/model.go:122`).
3. A type dependency is met when one enabled instance of that type is ready.
   Which instance answers is the host's choice (`internal/app/resource/model.go:88`).
4. A dependency on a host feature names a capability key from the served
   vocabulary (`internal/transport/http/httpapi/vocabularies.go:304`). The
   `core.feature.*` keys are the feature registry
   (`docs/src/content/docs/capabilities.md:922`).
5. A dependency on the core is a version range, and so is a dependency on
   another module or on the component library. The grammar is the one module
   authors know from npm and Cargo. It has an exact version, `^`, `~`, the
   comparison operators, a hyphen range, `x` or `*` wildcards, and `||`
   between alternatives. Prereleases follow semver. A bare MAJOR.MINOR.PATCH
   means that exact version, as it does in npm, so a module that wants a
   floor writes `>=1.2.3` (`sdk/manifest/versionrange.go`). The host takes
   its own version from the build (`internal/app/observefleet.go:286`).
5a. One parser reads every version field: the manifest's `core`, each entry
   of `requires`, and the library range in `ui.library`; the browser side
   accepts the same grammar. A range the parser cannot read refuses the
   module at identify, never at run time.
6. A wire method set is not a dependency. The contract version carries it
   (`sdk/wirerpc/lifecycle.go:51`).

### where it is declared

7. `Requirement` (`sdk/extension/descriptor.go:313`) gains `type` and `core`
   beside `capability`. One requirement sets exactly one of the three, and
   `Declare` refuses any other shape.
8. A builtin, a composed adapter and a service module all declare in
   `Doc.Requires`. `plugin.info` (`sdk/wirerpc/lifecycle.go:44`) carries the
   same list, so the host reads one shape whichever way the code arrived.

### when it is checked

9. The host checks a builtin at load (`internal/app/resource/load.go:146`)
   and a module at launch, in `identify`
   (`internal/extension/wirehost/launch.go:303`), before either is ready.
10. A missing requirement is refused with the code
    `RESOURCE_DEPENDENCY_MISSING` in the `core.resource` domain
    (`sdk/fault/code.go:83`), registered as the supervisor registers its own
    (`internal/extension/supervisor/faults.go:23`). The detail names what is
    missing.
11. The refused instance carries `DependenciesSatisfied` as `False`, reason
    `DependencyFailed` (`internal/kernel/condition/condition.go:60`), and
    `/api/v1/resources` lists it as `pending`
    (`internal/transport/http/httpapi/resourceroutes.go:20`,
    `internal/kernel/condition/state.go:144`).
12. An unmet dependency affects that instance only. The core keeps serving,
    every other instance starts, and `required` stays a separate switch
    (`internal/app/readiness.go:91`).

### ordering

13. The reconcile starts a dependency before its dependants. Today a plan is
    ordered by kind then name (`internal/app/resource/reconcile.go:72`) and
    carried out in that order (`internal/app/resourcehost/host.go:229`).
14. A cycle is refused at load as a `Failure`
    (`internal/app/resource/model.go:146`), reason `DependencyCycle`
    (`internal/kernel/condition/condition.go:61`). Every instance on the
    cycle is refused, and nothing else is.
15. A dependency that stops takes each dependant to rule 11's condition, and
    the detail names it. A dependency back at ready restarts its dependants.

### features

16. The capability vocabulary is the registry of features. A feature is added
    as a key there, generated and gated by `capabilities-check`
    (`Makefile:991`), never as a list in a module.
17. A host that lacks a named key refuses the module under rule 10.

## enforcement

Every gate is owed.

| rule | gate | state |
|---|---|---|
| 7 | a descriptor test: a requirement setting two fields, or none, is refused by `Declare` | owed |
| 8 | a wirehost test: `plugin.info` naming a key the host lacks is refused in `identify` | owed |
| 9 to 12 | a resourcehost test: a builtin naming an absent type reads `pending` with the code, and its sibling reaches `ready` | owed |
| 13 | a reconcile test: the plan lists a dependency before its dependant across kinds | owed |
| 14 | a load test: two instances naming each other both fail with `DependencyCycle` | owed |
| 15 | a resourcehost test: stopping the dependency moves the dependant to `pending`, restarting it moves it back | owed |
| 16 | `capabilities-check`, unchanged | exists |

## consequences

- `sdk/extension.Requirement` widens, a contract change under the `[sdk]`
  changelog marker
- a module missing a piece fails once at start, with a name

## rejected

| option | why not |
|---|---|
| depend on an instance id or a UID | an author knows neither: one is the operator's, the other is minted per install |
| a second list in `plugin.info` with its own shape | two shapes for one fact |
| block the core on an unmet dependency | that is `required`, a switch the operator owns |

## tasks

Each is one agent's work. Tasks 1, 2 and 6 touch the sdk contract.

1. widen `Requirement` with `type` and `core`, one field set, with the
   descriptor test (sdk)
2. carry `Requires` on `wirerpc.Info` and hold it in `identify` (sdk)
3. register `RESOURCE_DEPENDENCY_MISSING` and write `DependenciesSatisfied`
   from the resource host
4. check a builtin at load, with the cycle refusal
5. order the plan by dependency, and cascade a stop to dependants
6. publish requirements in `extensions.json` and the served vocabulary (sdk
   reference)
7. add a `core.feature.*` key for each host feature a shipped module needs

## what is not decided

- optional dependencies, which a module tolerates missing
- whether a remote node over Fractal (ADR-20260921-wkf) may satisfy a dependency
