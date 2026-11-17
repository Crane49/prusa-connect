---
title: "ADR-20260827-rwq: a module keeps its data out of the host's schema"
description: "Where a module's data lives, who runs its migrations, what removal and a downgrade do to it, where the record of an installed module is kept, and why a module's objects stay out of the authorization graph."
date: 2026-08-27
status: accepted
tags:
  - adr
  - extensions
  - database
  - migrations
  - authorization
---
# ADR-20260827-rwq: a module keeps its data out of the host's schema

- status: accepted, rules 2, 4 and 5 are gated and three rows are owed
- date: 2026-08-27

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Builds on [ADR-20260827-wgc](20260827-wgc-an-extension-reaches-persistence-through-the-sdk.md), [ADR-20260827-rqc](20260827-rqc-an-extension-declares-a-permission.md), [ADR-20260827-wmv](20260827-wmv-a-module-is-mounted-and-an-adapter-is-constructed.md) | Rule 5's start budget is amended by [ADR-20260828-dse](20260828-dse-a-modules-start-budget-is-thirty-seconds.md), which opens the record store to a starting instance so a module can obey it | Reads with [ADR-20260827-2bp](20260827-2bp-a-module-travels-as-a-verified-artefact.md)

[ADR-20260827-wmv](20260827-wmv-a-module-is-mounted-and-an-adapter-is-constructed.md) rule 5
puts an executing module in its own process, and
`internal/kernel/ref/runtime.go` already declares `KindDB: {RuntimeBuiltin}`. So
a module holds no handle, no transaction and no lock of the host's, and this
record states what it holds instead.

The host's file has one writer. Every failure this record guards against comes
from sharing it. A migration stops the host starting, a lock blocks the host, a
schema left behind refuses a later install, and two modules migrate at once.

## rules

1. Create no table in the host's database from a module. Nothing a module ships
   reaches the host's schema, its migration ledger or its write lock.
2. Reach the host's storage through `sdk/stateapi`, which
   [ADR-20260827-wgc](20260827-wgc-an-extension-reaches-persistence-through-the-sdk.md) rules 2
   to 5 define. A module's key/value door is `Records` under the namespace the
   host bound at construction, and this record mints no second table beside it.
3. Give a module that has outgrown key/value a store of its own, which is
   ADR-20260827-wgc rule 17: its own credentials, its own pool, and no handle from the
   host.
4. Run no migration the host did not write, and embed none.
   `//go:embed migrations/sqlite/*.sql` names the host's own directories, and a
   module's belong to the module.
   - **Narrowed by [ADR-20260901-rf2](20260901-rf2-pluggable-database-adapters.md) rules 8 and
     9.** The rule binds the host's own database. An adapter or module with a
     store of its own embeds and runs its own migrations there, inside its own
     supervised start. Rule 5 here already requires that start of a module.
5. Migrate a module's own store inside the module's own process, before it
   reports ready. Route nothing to a module that has not reported ready, which
   the supervisor and the reconcile outcomes already decide per instance.
6. Carry a module store's compat floor as an integer and ship no down migration.
   `internal/authz/compat.go` is the shape to copy, because an unstamped build
   calls itself `0.0.0-dev` and would refuse a database it wrote itself.
7. Keep a removed module's data and write a tombstone, which is ADR-20260827-wgc rule
   13. A purge names the namespace and is an operator's explicit act.
8. Keep a module store's schema number separate from the range it declares
   against `CoreContract`. One number answers what the store holds and the other
   answers which host it speaks to, so they move for different reasons.
9. Add no authorization object type, no relation and no level for a module. A
   module declares permissions on `resource_kind:<namespace>/<name>`, which is
   [ADR-20260827-rqc](20260827-rqc-an-extension-declares-a-permission.md) rule 14, and its
   administrators hold them from the moment the host registers the declaration.
10. Record an installed module in the machine-owned overlay, as a `plugins`
    section beside the instance records and under the fencing token
    `Document.Version` already computes. A database would break an earlier design decision,
    which reproduces an air-gapped install by copying one file.
11. Open a generalised `object_access` table only when a module's objects are
    granted to different people. Until then the trigger is written down and the
    graph gains nothing.

## what rule 11 costs if it is lifted early

The storage already takes a new object type: `authz_type` is a seeded lookup
table on both engines. Two pieces of core code do not. `project` in
`internal/authz/project.go` carries two object axes by name, and each axis has a
materialised table of its own.

A materialised object type puts a module's item count into a table the host
recompiles inside every grant write. `Store.mutate` reprojects the whole graph
in the transaction that bumps the epoch. Earlier research measured that on SQLite at
about 23 projection rows per object.

| objects | projection rows | compile | rebuild |
|---|---|---|---|
| 400 | 9,247 | 8.3 ms | 88.7 ms |
| 2,000 | 46,047 | 37.7 ms | 505 ms |
| 4,000 | 92,047 | 73.8 ms | 1,183 ms |

So a module holding 2,000 spools would make one unrelated grant on one printer
cost half a second. `resource_access` avoids this deliberately: its own schema
comment says it is keyed by kind and not by instance.

The design that lifts rule 11 is one generalised `object_access(object_type,
object_id, ...)` table with a flat projector over it. A projection is derived
from the tuples, so the migration creates the table empty and `Rebuild` refills
it. Its price is the first rise of the compat floor in this project's history,
after which every older binary in a fleet refuses the database and says why.

## the three modules against these rules

| module | its store | its permissions | a new object type |
|---|---|---|---|
| the UI | none. It renders what the API answers | none. The API authorizes every call | no |
| a job planner | its own, indexed, job history across 400 devices | declared on the kind it contributes | no |
| the Connect adapters | `Records`: a cursor, a token, a name mapping | none beyond its own sink's | no |

A job planner is the demanding one and still needs no object type, because its
views query objects the core already owns and already projects.

## enforcement

Rules 1 and 9 hold by construction today, so each is watched by review and by
the absence of any mechanism to break it. Rules 2, 4 and 5 are gated, and the
owed gates follow the house practice of being watched failing on a deliberate
mutation first.

Rule 2's gate runs two modules under two namespaces in two processes, against
one database. Each one names the other's namespace and the other's key. It was
watched failing on a store whose read dropped its namespace filter, where it
printed the neighbour's record.

Rule 4's gate reads the whole tree for a `//go:embed` naming a migration
directory and holds the set to the host's own. Those are two: the graph's, and
the record store rule 2 keeps a module's key/value in. Rule 5's gate drives a
module whose process is up and whose handshake has not landed. The supervision plane
lists it, refuses every call into it, and ends the attempt on the start budget
rather than holding the loop.

Rule 5 has a second half, because a record travels the other way. A module's
session is open before the host has finished starting the instance. The host
asks a child `plugin.info` during the launch, so what the child says from that
handler it says while it is still starting. A state call in that window is
refused with `stateapi.ErrNotReady`, which the module retries out of once the
instance is serving. The refusal names the instance and its state, so an author
reads which of the two it is waiting on.

| rule | gate | state |
|---|---|---|
| 1 | none possible. The host never sees the file, so it can prove nothing about it | review |
| 2 | `TestOneModuleReachesNoOtherModulesRecords` in `internal/app`, over two modules in two processes | exists |
| 3 | ADR-20260827-wgc's own rule 17, held by review | review |
| 4 | `TestTheEmbedNamesOnlyTheHostsOwnMigrationDirectories` in `internal/arch` | exists |
| 5 | `TestNothingIsRoutedToAModuleThatHasNotReportedReady` in `internal/app/resourcehost` | exists for the routing half. The budget went to research on 2026-08-28 |
| 5 | `TestAModuleCallingTheStoreBeforeItIsReadyIsToldToRetry` in `internal/app`, over a live module | exists for the record half |
| 6 | `internal/authz`'s `requireCompatFloor` is the precedent, per published rule | none possible |
| 7 | ADR-20260827-wgc rule 13's tombstone, and a health row that names it | owed |
| 8 | review. Two numbers in two records | none |
| 9 | `TestADeclarationOnACoreObjectIsRefused` and its siblings | exists |
| 10 | `TestThePluginsSectionSurvivesAnAtomicOverlayWrite` | owed |
| 11 | `Types()` is a Go table the compiler checks | exists |

## rejected

| option | why not |
|---|---|
| a module's own tables inside the host's database | it produces all five failures at once: the lock, the ledger, the start, the leftovers, two writers |
| a `module_state` table minted beside `sdk/stateapi.Records` | two readers of one set is the shape ADR-20260825-bhe rule 11 refuses. See [the store half](#module_state-and-sdkstateapi) |
| a module's own schema inside the host's Postgres | it still takes locks the host waits on, and its pool counts against `max_connections` |
| the host running a module's migrations | it runs them inside the host's start-up, which is Odoo's partly migrated database |
| dropping a module's data on removal | Odoo's uninstall is data loss behind a checkbox, and a reinstall then has nothing |
| leaving the data with no tombstone | Nextcloud leaves tables for ever with no command to drop them, and the namespace stays in the quota |
| a down migration per module | earlier research settled it for the host: restore the backup and start the older version |
| a version string as the compat floor | `0.0.0-dev` is below every floor, so an unstamped build refuses its own database |
| a database row for the record of an installed module | an air-gapped install is reproduced by copying one file, and an earlier design decision keeps that true |
| a third projection axis, one table per module | one core migration per module, which does not scale past the first |
| no materialised rows, traversing at check time | the list path then fetches and filters, which leaks through pagination and counts |

### `module_state` and `sdk/stateapi`

Earlier research proposes a
core-owned `module_state` table keyed by namespace and key, and ADR-20260827-wgc rule 3
had already published the same door as `Records` on `sdk/stateapi`. The note was
written before that record was accepted.

Rule 2 takes ADR-20260827-wgc's version, and the two differ in more than a name.
`Records` binds the namespace at construction so a caller cannot name its own,
carries `CompareAndPut` and `Version`, and arrives with the five limits ADR-20260827-wgc
rule 12 sets. It is also published at `GET /api/v1/state/namespaces`, so an
operator reads what a module is holding.

Earlier research's five failure modes and its removal answer survive unchanged. What
does not survive is the second table.

## open items

- `sdk/stateapi` carries `Records` and neither `Emitter` nor `History`, so rule
  2 binds code and ADR-20260827-wgc rules 2, 7 to 9 and 11 still do not. The old backlog carries
  the two that are missing.
- Rule 2's store keeps a module's records in the host's database, in a table and
  a migration ledger of the host's own beside the graph's. Rule 1 is about a
  module's tables and this is not one: a module ships no migration, holds no
  handle, and reaches the rows through six methods. Two hosts on one Postgres is
  untested. The namespace quota is a per-process lock, so each host holds its
  own.
- Rule 5's timeout is the host's own `StartTimeout`, 30 seconds for every
  instance, and no surface widens it per instance. A module whose migration
  takes two minutes therefore never starts and re-runs the migration on every
  rung of the ladder. The owner sent this to research on 2026-08-28 rather than
  answering it, and named his own leaning as a candidate: the module declares its
  time and the host enforces a maximum. The note comes first, then a record,
  then tasks, then code, and
  the old backlog carries the row.
- The tombstone in rule 7 has no reader. Health is the surface both records
  name, and the row is unspecified.
- Whether a module may read the host's data at all is untouched here.
  `sdk/dataapi.Store` is ADR-20260827-wgc rule 4's read side, and which of its records a
  module may reach is undecided.
- Nothing writes the administering tuple for a contributed kind, so rule 9
  confers a declared permission on an administrator the graph does not hold yet.
  ADR-20260827-rqc's open items own that.
- The projection numbers behind rule 11 were measured on SQLite alone, against a
  scratch export that predates the device rename. Postgres holds `actions` as
  `BIGINT` and writes through a different driver, so its curve is a hypothesis.
- The generalised `object_access` table's own query cost is unmeasured, because
  the table does not exist.
- The compat floor has never risen in this repository, so the refusal path in
  `requireCompatFloor` has been exercised by tests and by no upgrade.
