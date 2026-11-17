---
title: "ADR-20260901-rf2: pluggable database adapters"
description: "History and record storage become supervised, registerable extensions. The two contracts stay separate, a provider declares the message classes it consumes, the read surface is late-bound, an adapter owns its own schema and migrations, retention is a declared quantity, and the query API refuses rather than approximates."
date: 2026-09-01
status: accepted
amends: [ADR-20260827-wgc, ADR-20260827-rwq]
tags:
  - adr
  - storage
  - database
  - extensibility
  - adapters
---
# ADR-20260901-rf2: pluggable database adapters

- status: proposed. Every gate is owed
- date: 2026-09-01

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Amends [ADR-20260827-wgc](20260827-wgc-an-extension-reaches-persistence-through-the-sdk.md) rules 2, 7, 16 and 17 and its rejected row on retention of `state` and `audit`, [ADR-20260827-rwq](20260827-rwq-a-module-keeps-its-data-out-of-the-host-schema.md) rule 4, and an earlier design decision | Upholds HI1, HI4, [ADR-20260824-nyw](20260824-nyw-extension-mechanisms-and-the-hot-path.md) rules 1 to 3, [ADR-20260825-n55](20260825-n55-adapters-are-reference-implementations.md) rules 1 and 7, [ADR-20260825-bhe](20260825-bhe-every-vocabulary-is-a-gated-published-resource.md) rule 4, [ADR-20260827-wmv](20260827-wmv-a-module-is-mounted-and-an-adapter-is-constructed.md) rule 8, O4 and a design requirement | Reads with [ADR-20260901-atx](20260901-atx-durable-command-records.md) and ADR-20260901-7t4 (build-time adapter composition, companion)

The history provider and the record backend stop being hand-built in the
composition root and become registered, supervised extensions, the way an event
sink already is. Most of the mechanism exists and is switched off.
`extension.KindDatabase` is declared, `ref.KindDB` is in the closed kind table,
`registry.pointOf` maps one onto the other, and the supervision policy arm
exists. One boolean, `readerOfKind[KindDB] = false`, is what refuses a `db:`
declaration at load. This record enables an existing design; it does not invent
one. What it adds is the registration halves of two contracts, one new resource
kind for history, a declared class filter, a late-bound read surface, and an
honest retention seam.

"Registered" means compiled into the binary, and that is the model, not a
limit. Every adapter arrives at build time. A build configuration pulls the adapter's code, links it against the
core as it is, and the adapter hooks in like any other surface. There is no
live loading of new adapters, by design. How a build names and links an
adapter without editing core source is ADR-20260901-7t4's, a companion record this one
depends on but does not decide. Rule 5 carries the runtime consequence.

## the rules this record amends

Each subsection names the rule, why it blocks the requirement, what it was
protecting, and what protects that thing instead. The sub-bullet quoted in each
is the one permitted edit to the old record, to be placed on the amended rule at
merge.

**ADR-20260827-wgc rule 2, second sentence.** *"The host implements all three and an
extension constructs none of them."* It blocks the directive outright. A
pluggable record backend is an extension constructing the implementation behind
`stateapi.Records`. It was protecting the module-facing interface from drift and
from a module learning which store a deployment runs. Rule 4 here protects the
same thing. The interface a module is handed is unchanged, the namespace is
still bound at construction, and a module still cannot tell one backend from
another. Sub-bullet for the old rule:

> - **Amended by [ADR-20260901-rf2](20260901-rf2-pluggable-database-adapters.md) rule 4.** The
>   backend behind `Records` is a registered adapter. The interface a module is
>   handed, the namespace binding and the limits are unchanged, so rules 3, 5
>   and 12 stand as written. The second sentence of this rule does not.

**ADR-20260827-wgc rule 7.** *"An extension never writes a metric or an event into a
store. Only the hub does. So an extension reaches no history provider's write
side."* A registered history adapter *is* the write side, and it is an
extension, so the sentence is false as written. What it was protecting, a
consuming extension cannot bypass the hub, and never learns which provider is
installed, is preserved exactly. Rule 6 here keeps the provider an ordinary hub
subscriber, and the only path into any store is still an envelope through the
hub. Sub-bullet:

> - **Amended by [ADR-20260901-rf2](20260901-rf2-pluggable-database-adapters.md) rule 6.** A
>   registered database adapter is a history provider's write side and is an
>   extension. The rule survives for every *consuming* extension: nothing but
>   the hub feeds a provider, and no consumer learns which provider is
>   installed.

**ADR-20260827-wgc rule 16.** *"A provider that ships inside the binary writes
core-owned tables under the `core` namespace, and DB8 does not bind it."* It
implied only built-ins may be providers. It was protecting the host's
schema and ledger; rules 8 and 9 here protect them harder, by keeping every
adapter's store out of the host's database entirely. Sub-bullet:

> - **Narrowed by [ADR-20260901-rf2](20260901-rf2-pluggable-database-adapters.md) rules 8 and
>   9.** This rule describes the reference adapters. Shipping inside the binary
>   is not the condition of being a provider, and a provider's store is its
>   own, never the host's.

**ADR-20260827-wgc rule 17.** *"A community provider that has outgrown key/value runs at
the service rung with a database of its own."* The service rung cannot carry it.
No contract crosses the wire yet (`sdk/wirerpc` speaks `plugin.info` and
`plugin.shutdown` and nothing else). So the rule as written offers the one rung
that does not work and omits the one this record chooses. Under rule 5 a community
provider is compiled in at build time like every other adapter and runs at the
builtin rung; the service rung is deferred for every kind. The
credentials-and-pool half of the rule stands. Sub-bullet:

> - **Amended by [ADR-20260901-rf2](20260901-rf2-pluggable-database-adapters.md) rule 5.** A
>   community provider is compiled into the binary at build time, the way every
>   adapter arrives (the owner's model of 2026-09-01; the composition surface
>   is ADR-20260901-7t4's), and runs at the builtin rung, in process and supervised.
>   The service rung is deferred for every kind until a wire method set per
>   contract exists. Its own credentials, its own pool and no handle
>   from the host stand.

**ADR-20260827-wgc, rejected row "retention or downsampling of `state` and `audit`".**
*"The class forbids a silent loss, so only `telemetry` may be reduced."* The
owner directive makes retention the adapter's configuration. That means the
class no longer decides what a store keeps. What the row was protecting, no *silent*
loss, is protected by rule 15: an expiry-shortened answer is marked
`Incomplete` and a window the provider cannot answer is refused by name. The
bus half of the reasoning survives untouched as ADR-20260827-wgc rule 8, which is a
back-pressure rule about the hub and was never a storage rule. This record says
so to stop it being mistaken for one.

**ADR-20260827-rwq rule 4.** *"Run no migration the host did not write, and embed
none."* An in-process adapter with a store of its own must embed and run its own
migrations. The absolute form therefore blocks the directive. It was protecting the
host's boot, its migration ledger and its write lock. Rules 8 and 9 protect all
three. An adapter migrates only its own store, inside its own supervised
`Start`, so a failed migration is a retrying instance rather than a refused
boot. The host's database, ledger and lock are never touched. Sub-bullet:

> - **Narrowed by [ADR-20260901-rf2](20260901-rf2-pluggable-database-adapters.md) rules 8 and
>   9.** The rule binds the host's own database. An adapter or module with a
>   store of its own embeds and runs its own migrations there, inside its own
>   supervised start, which rule 5 here already requires of a module.

**Proposal decisions.** HI2 is amended: the `history` kind and the five methods
survive, and the method set grows `Retention()` (rule 14); the capability
bitset stays closed (rule 13). HI7 and HI8 stop being core rules and become the
reference SQLite adapter's documented defaults (rule 15). The measurement
behind HI7 stands as the reason the default is what it is. P4 is narrowed: the
graph and the machine-owned overlay keep one engine selector; the history
providers and the record backend choose their own engines (rule 12). DB8's
boundary moves: a compiled-in community provider with a schema of its own is
admitted, under rules 8 and 9. HI1 and HI4 are upheld, and the rules below rest on them.

## rules

1. The history provider and the record backend are pluggable extensions. Each
   is declared in the `resources:` block or through the resource API, and is
   built through a registry from a descriptor. It runs as a supervised instance
   with a breaker, a bounded queue, a health entry, restart and
   `resource.state.changed` like every other resource. Nothing about them is
   constructed by an `if` in the composition root.
2. The two contracts stay separate. `sdk/historyapi` (append-only, time-
   windowed, cursor-paged reads) and `sdk/stateapi` (per-key, versioned,
   `CompareAndPut`, no query) share no method and are not unified. One contract
   carrying both method sets behind a capability bitset is the fat interface
   an earlier design decision already refused for storage.
3. Each contract gains the four registration pieces `sdk/eventsapi` already
   has. They are a `Contract` version constant, `Register(name, doc, factory)` called
   from `init()`, a `Registration` carrying `Descriptor()`, `Qualified()` and
   `Build(spec)`, and `NewRegistry()` draining the gathering list.
   `ProviderSpec.Config` is `json.RawMessage`, decoded by the adapter and never
   read by the host, which is a design requirement.
4. For `sdk/stateapi` the registered thing is a backend factory that mints a
   `Records` per namespace. `stateapi.Records` itself is unchanged: same
   methods, same limits, namespace bound at construction, so no module sees a
   difference and ADR-20260827-wgc rules 3, 5 and 12 bind exactly as written.
5. A database adapter is compiled into the binary, exactly like every other
   adapter. An adapter arrives
   at build time, and a build configuration pulls its code and links it
   against the core as it is. There is no live loading of new adapters, for
   any kind. A community adapter is a first-class citizen under this model and
   arrives the same way a driver does. How a build names and links one without
   editing core source is ADR-20260901-7t4's, not this record's. At runtime,
   `runtimesOfKind` is `{RuntimeBuiltin}` for `history` and stays so for `db`.
   A `runtime: service` instance would build a target that satisfies nothing,
   because no contract crosses the wire yet. Out-of-process
   is deferred for every kind, and opens if the old backlog's wire method set per
   contract ever lands; `runtimesOfKind` cites that as the gate.
6. A history provider is an ordinary hub subscriber and adds no queue to the
   hub, which is HI4 unchanged, now one subscription per instance instead of
   one for the process. Each instance subscribes under a principal of its own,
   `history:<id>`, a new `authz.Type` beside `TypeSink`, holding exactly the
   grants an operator wrote. An instance declaring a class in `Consumes` that
   its principal holds no grant to read reports degraded in its health entry,
   so it never stores nothing silently. That per-class check is sufficient only
   because the bootstrap grant is relation-shaped over every device origin,
   present and future (ADR-20260901-b2z rule 34). Such a grant cannot hold on three
   printers and miss a fourth. An operator who replaces it with hand-written
   per-origin grants owns the audit gap on the origins left out, and the health
   entry names the class, not the hole. The provider never receives a caller's
   principal: `Refs` on a query is already reduced to what the caller may read,
   and that pre-filter stays in the composition root.
7. `extension.Doc` gains `Consumes []event.Class`: the message classes a
   registration is fed. Empty means none, so a registration that catches
   nothing is visible as such in the published reference. The filter is applied
   by the feed, one switch on `frame.Class` against the declared set; the hub
   is untouched. The declared set never widens what a principal may see and
   never changes what a frame means, it is a subscription filter and nothing
   more.
8. An adapter owns its schema. Its store is its own, its own file, its own
   credentials, its own pool, and it never touches the host's database, the
   host's migration ledger or the host's write lock. Where its data lands, and
   on what engine, is the adapter's business.
9. An adapter migrates its own store inside its own `Start`, on the
   supervisor's restart ladder. A failed migration is a retrying instance and
   the server serves without that provider. An operator who wants today's
   fail-fast boot writes `required: true` on the declaration, which is decision
   O4 doing the work it was written for. The embed gate that guards ADR-20260827-rwq
   rule 4 admits an adapter's migrations by one pattern over the `adapters/`
   tree, never by naming the adapter. A new adapter therefore edits nothing
   under `internal/`. A per-adapter entry in `internal/arch` would be a core
   patch per adapter, which is what the owner directive forbids.
10. History providers take a new resource kind, `history`: `ref.KindHistory` in
    `Kinds()`, `ConfigurableKinds()`, `SupervisedKinds()`, `classOfKind`,
    `runtimesOfKind` and `readerOfKind`, plus `extension.KindHistory` and its
    `registry.pointOf` entry. The record backend takes the existing `db` kind.
    `readerOfKind[KindDB]` flips to true once the reader exists, and the `db`
    vocabulary prose is rewritten to stop claiming the instance holds the event
    history. `ref.Kinds()` stays a compile-time table, this record adds two
    core entries once and mints nothing at runtime, so ADR-20260825-bhe rule 4 and
    ADR-20260827-wmv rule 8 stand.
11. With no serving instance the feature is absent and says so, which is HI1
    strengthened. The history routes answer
    `501 core.resource/HISTORY_NOT_CONFIGURED`, computed per request from the
    resolver rather than from a nil captured at boot. A provider that is
    restarting therefore reads the same as one never declared. A record backend that is
    not serving answers a module `CodeMethodNotFound`, the nil path the plane
    already has.
12. Two or more instances may consume the same class, and writes fan out: every
    instance whose `Consumes` matches a frame's class receives it. The read
    side never guesses. The resolver picks the answering instance per verb by
    its capabilities. Where two serving instances claim the same verb the
    query is refused with a fault of its own, registered in `sdk/fault` and
    naming both instances. The refusal names the way out: the operator writes
    `reads:` on one instance's declaration, listing the query verbs it answers.
    An instance with no `reads:` list claims every verb its capabilities carry.
    `extension.ErrAmbiguous` is not reused here, it reports two namespaces
    answering one bare name, not two instances claiming one verb.
13. The capability seam is the closed bitset `Caps()` and nothing else. The
    resolver picks readers by it (rule 12) and the served OpenAPI document
    gates on it (rule 15). A capability the core never named is a contract
    change, taken when something reads it.
14. The provider contract gains `Retention() []Resolution`, where a
    `Resolution` is a step (0 = raw) and a keep window (0 = forever), sorted
    coarsest last, empty = keeps everything. The adapter reads its retention
    out of its own `Spec.Config` and declares the result; the core reads the
    declaration, never writes a default and never deletes anything. The
    declaration earns its place because rule 15 consumes it: the refusal that
    names the coarsest step still available is computed from `Retention()`,
    not guessed. The 48-hour raw window with 5-minute and hourly buckets
    survives as the reference SQLite adapter's default, published in its
    `extension.Doc.Options`.
15. The query API degrades honestly. A provider refuses a query it cannot
    answer truthfully rather than approximating, and a bucketed query is never
    answered from raw rows. A raw query older than the declared raw window
    is refused by name, telling the caller the coarsest step still available.
    `sdk/historyapi` gains a typed query refusal beside `ErrUnsupported`, so a
    third-party refusal reaches the caller as a 422 and not a 500. A window
    shortened by expiry sets `Incomplete`, so it is distinguishable from a
    window in which nothing happened. Aggregate query parameters appear in the
    served OpenAPI document only while a provider declaring `CapAggregate` is
    serving.
16. `httpapi.Config.History` and `.State` become resolvers, `func() (T, error)`,
    and `resourcehost.Config.State` is bound from the plane, late, through the
    same shape. The error is typed, because one boolean cannot carry two
    different absences. No serving instance is rule 11's 501, and two instances
    claiming the asked verb is rule 12's fault, a configuration conflict, not
    an absent feature. This is what breaks the supervision cycle. The record
    backend is supervised by the plane and the plane's state door resolves per
    call. Nothing at plane construction therefore needs a value that the plane
    itself will build. One boundary keeps the resolver from re-entering itself.
    An instance of kind `db` resolves no `Records`, because a backend consuming
    the state door could be handed the very backend that is still starting.
17. The reference implementations move out of `internal/platform/history` and
    become adapters: `adapters/db/sqlite` and `adapters/db/postgres`, two
    adapters and not one with a mode flag, per ADR-20260825-n55 rules 1 and 7. The move
    takes `internal/platform/history/migrate.go` out of the embed gate's host
    set (`internal/arch/migrations_test.go:36-44`, three entries today), and
    `declaredEmbedCount()` moves with it. The moved schema is admitted by rule
    9's pattern like any other adapter's. The SQLite adapter stays the shipped
    default and stays off by default: a deployment that never enables it opens
    no file, runs no migration and holds no subscription.
18. The old `core.history.{enabled,database}` keys are deleted from the
    configuration vocabulary, with no refusal machinery and no translation.
    Nothing has shipped, so no deployment holds them and
    there is nobody to warn. The `history:` declaration is the only spelling
    that ever ships. An adapter that needs a driver declares its build tag
    in `adapters/tags.json`; `detect-build-tags` stops deriving the Postgres
    driver from `persistence.type` alone.
19. Migration proceeds in the research's six steps, each shippable. First,
    publish the registration halves. Second, move the reference store to
    `adapters/`, the step that proves the contract is complete, because
    ADR-20260825-n55 rule 2 forbids an adapter importing `internal/`. Third, make the
    read surface late-bound. Fourth, add the `history` kind and regenerate
    every vocabulary artefact. Fifth, supervise the provider and rebuild the
    feed on the `sinkFeed` reconcile shape. Last, the record backend, because
    it carries the startup cycle rule 16 breaks.

## enforcement

Every gate is owed and each is watched failing on a deliberate mutation before
it is trusted green, per the house rule.

| rule | gate | state |
|---|---|---|
| 1, 10 | `internal/arch/resource_readers_test.go`, failing by design until the readers exist; both vocabulary files regenerated | owed |
| 2, 3 | the conformance suite ADR-20260827-wgc already owes a history provider, run against the reference adapters via the registry | owed |
| 4 | ADR-20260827-wgc rule 2's existing gates, unchanged, run against a registry-built backend | owed |
| 5 | a case refusing `runtime: service` on both kinds, citing the old backlog | owed |
| 6 | a subscription carries the `history:<id>` principal only; a declared but ungranted class reads degraded in health | owed |
| 7 | a test that a frame outside the declared set never reaches the instance, and that an empty set attaches no subscription | owed |
| 8, 9, 17 | the embed gate: host set drops to two (rule 17); one `adapters/` pattern admits adapter migrations, no per-adapter entry | owed |
| 11 | the existing 501 test, moved onto the resolver; a case with a declared-but-restarting instance | owed |
| 12 | a two-instance fan-out test; a two-reader refusal watched failing unguarded, the fault naming both instances and `reads:` | owed |
| 14, 15 | the existing refuse-rather-than-approximate case (the bucketed query), extended over a declared retention window | owed |
| 16 | `CodeMethodNotFound` before the backend serves, success after; two errors for absence and ambiguity; no `Records` for `db` | owed |
| 18 | no `core.history.*` key in any vocabulary artefact; `detect-build-tags` reads the Postgres tag from `adapters/tags.json` only | owed |

## rejected

| option | why not |
|---|---|
| one unified store contract subscribing by class | append/window/cursor and per-key/CAS/no-query share nothing; a bitset over one interface is the fat interface S2 refused |
| registry lookup only, construction stays in the composition root | no supervision, health, restart or per-provider grant; one provider; entrenches a `core.history:` shape later removed |
| out-of-process providers now | the wire speaks only `plugin.info` and `plugin.shutdown`; a service-rung provider stores zero rows; deferred for every kind |
| an open declarative capability half beside the closed bitset | an extension point with zero consumers: no rule in this record reads such a key. Added when something reads it |
| synthesise a `history:` declaration from the old `core.history` keys, or refuse them with a message naming the replacement | machinery for deployments that do not exist; no config holds the old keys; only `history:` ever ships |
| the core owns a retention default | retention is the adapter's configuration; a core default is a core rule under another name |
| refuse a second instance on a class already claimed | Timescale for metrics beside Postgres for audit is the case to serve; only the read side must not guess (rule 12) |
| drop the built-in SQLite adapter | a zero-config record of what happened is worth one adapter directory; off by default preserves HI1's Pi-install property |
| filter classes in the hub | HI4: the provider is an ordinary subscriber, the hub gains no queue and no predicate; the feed owns the routing switch |

## open items

- **The allowlist.** `adaptedSinks` and its siblings in
  `internal/app/builtin/adapters.go` are an inventory of what an artefact
  carries, not a security barrier. The lists gate in-binary names only,
 and under rule 5 every adapter is in-binary. Keep them for
  the shipped reference build, they document what we ship and cost nothing.
  Whether a build-composed community adapter is admitted without a line in
  this file, and by what surface, is ADR-20260901-7t4's to settle. This record only
  requires that admitting one is not an edit to core source.
- The proposal's target tree names `sdk/dbapi/`, which does not exist. This
  record keeps the contracts in `sdk/historyapi` and `sdk/stateapi` and the
  proposal line is amended at merge; no third package is minted.
- The history principal is `history:<id>` from the first release; `Bootstrap`
  writes its grant on first run the way it does per configurable kind. The
  failure to guard against is a provider with no grant storing nothing,
  silently, rule 6's degraded health entry is the guard.
- Boot-failure semantics invert for a store that cannot migrate: refusal
  becomes retry unless `required: true`. Operators are told.
- The generated artefacts, resource kinds, authorization vocabulary, the
  extension reference, the claims files, regenerate at merge. Never
  hand-merged.
- `ClassCommand` in a `Consumes` set is reachable only once
  [ADR-20260901-atx](20260901-atx-durable-command-records.md) rules 2 and 3 land; until then a
  provider declaring it is fed nothing.
