---
title: "ADR-20261016-pmj: a table's schema lives beside its code"
description: "Each package that keeps tables in the host database owns their migrations, under one ledger and one runner, and every chain starts from one baseline per engine."
date: 2026-10-16
status: accepted
tags:
  - adr
  - schema
  - migrations
---
# ADR-20261016-pmj: a table's schema lives beside its code

- status: accepted
- date: 2026-10-16

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md)

The host database holds the authorization graph, the device records and
prints, the registrations, the accounts and credentials, and the dashboards.
Each of those has its own Go package, and the schema of each is now beside it:
a `migrations/{sqlite,postgres}` directory the package embeds and exports as
`Migrations()`. The host applies them all under one ledger. The history store
and the module store are adapters with stores of their own and keep their own
ledgers ([ADR-20260901-rf2](20260901-rf2-pluggable-database-adapters.md) rules 8 and 9).

## rules

### who owns a table

1. The package whose Go code reads and writes a table owns its schema. Its
   migrations sit in that package's `migrations/sqlite` and
   `migrations/postgres` directories, with the same file names, and it exports
   them as `Migrations()`.
2. The owners of the host database are `internal/authz` (the graph and the
   device directory `authz_device`), `internal/platform/devicestore` (device
   records, prints and registrations), `internal/platform/auth/dbstore`
   (accounts, credentials, pairings) and `internal/platform/dashboards`.
   `internal/app/hostschema` lists them in the order they are migrated.
3. A statement of one owner names another owner's table only where a rule of
   the schema needs it, and a comment at the statement says which table and
   why. There are three: `device_record` and `printer_job` reference
   `authz_device` with `ON DELETE CASCADE`, and the print history joins
   `device_access` so the permission is resolved inside the query.
4. An owner whose tables reference another owner's is migrated after it.

### one database, one ledger

5. The host database is one database with one ledger, `schema_migration`, one
   migration lock and one compatibility floor. A ledger row is named
   `<owner>/<file>`.
6. The runner is `internal/kernel/sqlstore`. It refuses these before writing
   anything:
   - a SQLite handle without foreign keys;
   - a floor above this build;
   - a ledger row this build does not ship;
   - a table a migration would create that exists without its ledger row.
7. An adapter's store runs its own ledger with
   `adapters/db/internal/engine.Migrate`, which makes the same two refusals of
   a foreign database. It looks for the store's own tables only, because on
   PostgreSQL the store may share the host's database.

### baselines

8. Every chain starts from one file per engine, `0001_schema.sql`, that states
   its tables as they are. A comment in it describes the schema, never a
   change that led to it.
9. From the first beta, a shipped file is never edited or renamed. A change to
   the schema is a new file in the owner's directories, and it keeps the data
   it finds. A migration an older build cannot survive raises the floor in
   `internal/app/hostschema`.
10. A database made by a build before the baselines is not upgraded. The rule
    in 6 refuses it and names a table it found.

## why

- Every table's schema is beside the code that depends on it, so a reader of
  `devicestore` finds `device_record` there, not in the authorization package.
- One database stays one, as earlier research decided: the records and the
  grants are joined, and a foreign key needs both tables in one database.
- The chains this replaces recorded how the schema was found during the
  rewrite, renames and conversions included. Only the 0.0.1-alpha
  prereleases shipped them, to internal testers, so nothing needs the steps.

## rejected

| option | why not |
|---|---|
| a ledger per owner in the host database | five ledgers, five locks and five floors, with ordering rules between them, over one database |
| keep the device tables in `internal/authz` and move only the SQL of the others | leaves the device and print code in the authorization package |
| separate databases per owner | breaks the cascades to `authz_device` and the permission join |
| upgrade the old chains into the baselines | keeps every legacy step in the tree for a handful of internal alpha databases |
