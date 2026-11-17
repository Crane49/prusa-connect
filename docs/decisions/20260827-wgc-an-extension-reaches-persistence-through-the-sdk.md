---
title: "ADR-20260827-wgc: an extension reaches persistence through the sdk"
description: "An extension reaches persistence through three doors. Records are key/value under its own namespace. Metrics and events leave as envelopes. Reads carry the caller's permission. No sdk/ contract exposes the host's *sql.DB."
date: 2026-08-27
status: accepted
tags:
  - adr
  - extensions
  - database
  - sdk
  - events
---
# ADR-20260827-wgc: an extension reaches persistence through the sdk

- status: accepted
- date: 2026-08-27

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Builds on [ADR-20260825-bhe](20260825-bhe-every-vocabulary-is-a-gated-published-resource.md), [ADR-20260826-w3y](20260826-w3y-the-script-tier.md), [ADR-20260826-6x5](20260826-6x5-a-namespace-is-legal-everywhere-it-is-used.md), [ADR-20260827-rqc](20260827-rqc-an-extension-declares-a-permission.md) | Proposal decisions: P1, P6, DB8, X10 | Epic: the old backlog

An extension that stores something has three doors and no fourth. A record is
a key/value pair under the extension's own namespace. A metric and an event
leave the core as envelopes, and the hub stores them. A read of history comes
back filtered by the caller's permission. The fourth door, the host's own
`*sql.DB`, stays shut. A handle placed in front of a compiled-in extension
reaches `authz_tuple`, writes tables no migration ledger records, and pins its
author to one engine. Rule 1 is the owner's, as an earlier design decision. Rules 2
to 18 are the mechanism, reasoned in
earlier research, and an agent can
revise them.

## rules

1. There are three kinds of stored thing. A metric and an event leave the core
   as envelopes. A record is a store contract with identity, and a change to a
   record emits an event. The `ref` is the join key across all three.
2. `sdk/stateapi` carries three interfaces and no more: `Records` for key/value,
   `Emitter` for the write side of metrics and events, `History` for the read
   side. The host implements all three and an extension constructs none of them.
   - **Amended by [ADR-20260901-rf2](20260901-rf2-pluggable-database-adapters.md) rule 4.** The
     backend behind `Records` is a registered adapter. The interface a module
     is handed, the namespace binding and the limits are unchanged, so rules 3,
     5 and 12 stand as written. The second sentence of this rule does not.
3. `Records` answers `Get`, `Put`, `CompareAndPut`, `Delete`, `List` and
   `Version` over a typed `Key` and an opaque value. `List` returns keys in
   sorted order and never a value.
4. The read side of core records is `sdk/dataapi.Store`, which grows the device
   record and the inventory item. No second read surface is minted for them,
   because two readers of one set is the shape ADR-20260825-bhe rule 11 refuses.
5. The namespace is bound at construction and is never a caller argument. A
   compiled-in extension takes the ADR-20260826-6x5 namespace `extension.NamespaceOf`
   derives from its module. A service-rung child takes the namespace its
   manifest declares, checked at install. The host registers on a script's
   behalf.
6. No contract under `sdk/` exposes `*sql.DB`, and no package under `sdk/`
   imports `database/sql`. `internal/arch` carries a case refusing that import,
   through the reviewed exception table the `gin` exception already uses.
   - **Amended by [ADR-20261021-bqc](20261021-bqc-a-module-may-keep-its-data-in-a-database-of-its-own.md) rule 32.**
     `sdk/moduledb` imports `database/sql` to open a module's own database,
     which the operator granted. No contract exposes a handle on the core's
     database.
7. An extension never writes a metric or an event into a store. Only the hub
   does. So an extension reaches no history provider's write side, and never
   learns which provider a deployment installed.
   - **Amended by [ADR-20260901-rf2](20260901-rf2-pluggable-database-adapters.md) rule 6.** A
     registered database adapter is a history provider's write side and is an
     extension. The rule survives for every *consuming* extension: nothing but
     the hub feeds a provider, and no consumer learns which provider is
     installed.
8. Back-pressure divides on the envelope's class. A `telemetry` envelope is
   dropped oldest first and the drop is counted. A `state` or `audit` envelope is
   held, and then `Emit` refuses with an error the caller sees.
   - **Named a bus rule by [ADR-20260901-rf2](20260901-rf2-pluggable-database-adapters.md)
     rule 14.** This is a back-pressure rule about the hub and not a storage
     rule. What a store keeps, and for how long, is the adapter's declared
     retention. The rule here is unchanged.
9. `Emit` buffers and flushes on a timer or a byte threshold, and `EmitBatch`
   takes a batch the author already holds. Order inside one stream is the order
   the author wrote.
10. A stored value at or below 1 KiB travels inside the control frame, and a
    larger one travels in a `data` frame on the same stream. The measured price
    is flat to 1 KiB and 20 times as high at 64 KiB inside JSON.
11. A `History` answer carries the caller's permission, filtered by the
    visibility predicate `internal/transport/stream/hub.go` applies to the event
    stream. An event a caller may not see on the stream is unreadable from
    history.
12. Five limits bound a namespace. A key is 150 characters, a value is 64 KiB,
    a namespace is 8 MiB, and the rate is 100 envelopes per second. Writes run
    under the supervisor's existing bulkhead. Every refusal is a fault with a
    code, so an author reads the limit.
    - **Amended by [ADR-20261007-br4](20261007-br4-an-operator-grants-a-module-room-for-its-records.md)**:
      8 MiB is the default, and an operator may grant one module from 1 to
      1024 MiB.
13. Removing an extension leaves its rows and writes a tombstone. The tombstone
    records the namespace, the removal time, the key count and the byte total.
    Health publishes it and an operator purges explicitly.
14. An extension declares the version pair it writes and migrates its own value.
    The host calls that migration on the first read after an upgrade, inside one
    transaction. An extension declaring no pair is held at 1.1 and is never
    migrated.
15. `GET /api/v1/state/namespaces` publishes the namespace, its key count, its
    bytes, its last write and its tombstone. The keys endpoint publishes keys,
    sizes and version pairs. No endpoint under it returns a stored value, and
    export is a permission of its own.
16. A provider that ships inside the binary writes core-owned tables under the
    `core` namespace, and DB8 does not bind it. Its migrations, its ordering and
    its removal are the core's.
    - **Narrowed by [ADR-20260901-rf2](20260901-rf2-pluggable-database-adapters.md) rules 8 and
      9.** This rule describes the reference adapters. Shipping inside the
      binary is not the condition of being a provider, and a provider's store
      is its own, never the host's.
17. A community provider that has outgrown key/value runs at the service rung
    with a database of its own. It holds its own credentials and its own pool,
    and it receives no handle from the host.
    - **Amended by [ADR-20260901-rf2](20260901-rf2-pluggable-database-adapters.md) rule 5.** A
      community provider is compiled into the binary at build time, the way
      every adapter arrives. It runs at the builtin rung, in process and
      supervised. The service rung is deferred
      for every kind until a wire method set per contract exists. Its own
      credentials, its own pool and no handle from the host stand.
18. A script persists nothing and emits by returning a value the host publishes.
    The host refuses more than 16 messages per event and more than 64 KiB per
    message, where it takes the returned value.

## enforcement

Rules 1, 2, 4 and 16 to 18 are design rules a reviewer holds. The gates below
are the mechanical half. Each is watched failing on a deliberate mutation
before it is trusted green, which is the house rule in ADR-20260825-smx's enforcement
table. A provider's conformance suite follows
`storageapitest.RunProviderContract`
(`sdk/storageapi/storageapitest/contract.go:137`), which ADR-20260825-smx rule 9 makes
binding on every provider.

| rule | gate | state |
|---|---|---|
| 6 | `TestTheSdkImportsOnlyTheStandardLibrary` (`internal/arch/layout_test.go:892`) refuses `database/sql` under `sdk/` | held |
| 3, 5 | `TestNoWireCallCarriesANamespace` (`sdk/stateapi/wire_test.go:22`), over every published params object | held |
| 12 | `TestANamespaceCannotBeFilledPastItsQuota` (`adapters/db/internal/recstore/store_test.go:219`) per engine | the three sizes; the rate is owed |
| 12 | `TestAModuleCannotFillTheHostsDisk` (`internal/app/statemount_test.go:248`), through a real process | the byte limit |
| 12 | `TestAModuleAtItsStateLimitIsRefusedAndNotQueued` (`internal/app/resourcehost/statecall_internal_test.go:52`) | the concurrency bound, with every slot held |
| 12 | `TestARefusalKeepsItsMeaningAcrossTheWire` (`sdk/stateapi/wire_test.go:106`) | a module reads that refusal as `ErrBusy` |
| 12 | `TestABurstOfStateCallsFromOneModuleIsAccountedFor` (`internal/app/statemount_test.go:447`) | a burst from a live module loses no call and no record |
| 12 | `TestAModulesRoutesAndItsRecordsAreBoundedSeparately` (`internal/app/statemount_test.go:505`) | one semaphore per direction |
| 15 | `TestTheNamespaceListingIsAuthorizedLikeTheModuleItDescribes` (`internal/app/modulemount_test.go:103`) | the listing, as an operator and as nobody; two endpoints owed |
| 11 | `TestAnAuditFrameReachesOnlyASubscriberWithAuditSight` (`internal/transport/stream/audit_test.go:94`), on the stream | held for the audit class |
| 11 | `TestAnAuditFactIsUnreadableWithoutAuditSight` (`internal/app/commandaudit_test.go:122`), from history | held; ADR-20260901-atx's equivalence gate |
| 2 | the conformance suite a history provider owes, run once against the value and once against the client stub | the pattern only |
| 8 | a test that stops granting credit, then asserts a dropped metric and a refused event | owed |
| 18 | a case in the script conformance suite over both output caps | owed |

## rejected

| option | why not |
|---|---|
| hand an extension the host's `*sql.DB` | it reaches `authz_tuple`, creates tables with no ledger row, and pins the author to one engine |
| the namespace as an argument on every call | the caller then names its own namespace, and X5 and ADR-20260826-6x5 exist so the host names it |
| an extension writing a history provider directly | it would learn which provider is installed, and the class would stop deciding drop and replay |
| a second read surface beside `dataapi.Store` for core records | two readers of one set drift, which ADR-20260825-bhe rule 11 states for the same reason |
| an `sdk/databaseapi` so a third engine ships as a plugin | a third SQL file per migration, a third parity column, a third test lane. [ADR-20260901-rf2](20260901-rf2-pluggable-database-adapters.md) reopens it |
| community-owned tables in the shared database | eight pre-conditions, DB3's missing migration lock among them, against one child process with its own file |
| a script that keeps state between events | ADR-20260826-w3y rule 18 refuses it. An adapter that has to remember is a service-rung child |
| one growing blob per time series in a key/value row | a range query then reads and rewrites the whole value, and a metric has a class that already says how it may be lost |
| retention or downsampling of `state` and `audit` | the class forbids a silent loss, so only `telemetry` may be reduced. [ADR-20260901-rf2](20260901-rf2-pluggable-database-adapters.md) rules 14 and 15 reopen it; no loss is silent |
| a tombstone without a byte total | the namespace leaves the quota accounting while its rows are still on the disk |

## open items

- `sdk/stateapi` carries `Records` (`sdk/stateapi/records.go:180`) and no
  `Emitter` or `History`, so rules 7 to 9 bind no code. The backend behind it is
  `stateapi.Backend` (`sdk/stateapi/provider.go:27`), attached per instance by
  `supervised.Backend` (`internal/extension/supervised/records.go:24`). The read
  side rule 11 is held to is `historyapi.Provider`
  (`sdk/historyapi/history.go:419`). The old backlog carries the two that are missing.
- Rule 4's `sdk/dataapi.Store` does not exist. `internal/arch/build_page_paths_test.go:85`
  lists the package as stale, and no read surface for the device record or the
  inventory item is written under `sdk/`.
- Rule 10 is not implemented. `sdk/stateapi/wire.go:58` records that every value
  `Records` accepts fits in one control frame, so the data-frame path is a
  throughput change with the same method names.
- Rule 12's rate limit belongs to `Emitter` and is unwritten. The three sizes
  are the constants at `sdk/stateapi/records.go:51`, enforced on both engines.
  The byte quota is a sum over rows read inside the write's own transaction
  (`adapters/db/internal/recstore/store.go:248`), exact for one host only.
- Rule 12's bulkhead is a semaphore of its own and not the supervisor's, at the
  instance's own `Policy().Concurrency`. `gatedRecords`
  (`internal/app/resourcehost/statecall.go:43`) says why one shared semaphore
  wedges a route that reads a record.
- Rule 14 has no code. `Records` stores opaque bytes and no version pair, and
  `sdk/stateapi/records.go:33` tells the author to carry the pair inside the
  value. So the host calls no migration, rule 15's keys endpoint has no version
  pair to publish, and no test proves a migration runs in one transaction.
  Either the rule loses its pair or `module_state` grows two columns and
  `Records` grows a declaration; nothing here decides which.
- Rule 15 is three fields of five. `Accounting` (`sdk/stateapi/provider.go:47`)
  holds the namespace, its key count and its bytes. `module_state.updated_at`
  is written by `upsertRecordSQL` (`adapters/db/internal/recstore/store.go:252`)
  and read by nothing. The tombstone of rule 13 does not exist, and the keys
  endpoint and the export are unwritten.
- Rule 15's listing shows a namespace only while an instance claims it. The
  permission is `view_resource` on that instance
  (`internal/transport/http/httpapi/statenamespaces.go:144`), because a
  namespace is not an authorization object and ADR-20260827-rwq rule 9 says it never
  becomes one. A removed module's rows are invisible until the tombstone has a
  reader.
- Rule 5's install-time check of a service-rung namespace is not written.
  Earlier research records it as owed.
- Every number in rule 12 and both caps in rule 18 are proposals reasoned from
  the printer budget, and no plugin has stored anything real against them.
