---
title: "ADR-20261007-br4: an operator grants a module room for its records"
description: "The records_mib request: a module asks for more than the 8 MiB default for its records, the operator grants it at install, and the record store holds the namespace to that grant."
date: 2026-10-07
status: accepted
amends: [ADR-20260827-wgc]
tags:
  - adr
  - modules
  - persistence
---
# ADR-20261007-br4: an operator grants a module room for its records

- status: accepted
- date: 2026-10-07

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Amends: [ADR-20260827-wgc](20260827-wgc-an-extension-reaches-persistence-through-the-sdk.md) rule 12

ADR-20260827-wgc rule 12 holds every module's records to 8 MiB. A catalogue of a few
thousand entries fills that, and the operator had no way to give it more. The
files a module keeps are in a storage and never counted. This record lets an
operator grant one module more room for its records. The grant is the
operator's, and the module cannot widen it.

## rules

### what the module asks and the operator grants

1. A module asks for room with the manifest key `records_mib`, a whole number
   of mebibytes from 1 to 1024. Absent is a module content with the default.
2. The declaration carries the grant as `service.records_mib`, from 1 to 1024.
   Absent is no grant, and the namespace holds 8 MiB. A grant below 8 is
   allowed, and it holds the module to less.
3. `module add` and `POST /api/v1/modules` write the key into the declaration
   only with approve, as they write `permissions` and `events`.
4. An install without approve lists the request as `records <n> MiB`. An
   approved install lists the same line among what it gave.
5. The operator may grant another amount than the module asks for, with
   `module add -approve -records-mib <n>` or `records_mib` beside `approve`
   in `POST /api/v1/modules`. A room named without approve is refused.
   Installing again with another number changes the grant, and the new limit
   holds from the module's next start.

### what the store enforces

6. The host hands the record backend the limit with the namespace, in
   `stateapi.Backend.Records(namespace, limit)`. The store refuses a write
   that would take the namespace past it with `ErrNamespaceFull`, naming the
   limit.
7. A lower limit than what the namespace holds deletes nothing. Reads,
   deletes and rewrites of a value already held go on, and a new byte is
   refused until the namespace is back under the limit.
8. The settings the web app keeps for a person under a module's namespace are
   held to the same limit as the module.
9. The other limits of ADR-20260827-wgc rule 12 are unchanged: a key is 150 bytes and
   a value 64 KiB.

### what the module reads

10. `GET /api/v1/state/namespaces` answers each namespace's `limit` beside
    its `bytes`, so the operator reads the grant against what is held.
11. `state.usage` answers what the namespace holds and the limit it is held
    to. A host without the method answers `CodeMethodNotFound`, and the module
    then reads the 8 MiB default.
12. The record backend contract stays at version 1.0.0. `Records` takes the
    limit and has the `Usage` call, and nothing outside this repository
    implements the contract yet, so it changes in place.

## enforcement

| rule | gate | state |
|---|---|---|
| 1 | `sdk/manifest` tests, which read the key and refuse one outside 1 to 1024 | exists |
| 2 | `internal/app/config/install` records tests, a grant and a refusal at load | exists |
| 3, 4, 5 | `internal/app` module records tests, over the command and over HTTP | exists |
| 6, 7 | `TestANamespaceIsHeldToTheLimitItWasGranted`, on SQLite and on Postgres | exists |
| 6, 10 | `TestAModuleHoldsTheRoomTheOperatorGranted`, through a real module process | exists |
| 11 | `sdk/stateapi` wire tests, `Usage` over the wire | exists |

## rejected

| option | why not |
|---|---|
| raise the default for every module | every module could then fill more of the disk, whether it needs to or not |
| let the module set the limit at run time | the limit is what protects the disk from the module, so the module cannot hold it |
| one setting for the whole server | one large catalogue would raise the limit of every other module too |
| count the files toward the limit | the files are in a storage the operator chose, with its own room |

## open items

- the web app's install dialog does not show the request yet, and has no
  field for the grant
