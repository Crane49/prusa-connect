---
title: "ADR-20261027-xrq: A rollback finishes when PostgreSQL is protected outside the server"
description: "A rollback restores the local files and the previous binary and leaves an acknowledged external or omitted PostgreSQL database to the operator."
date: 2026-10-27
status: accepted
supersedes: []
amends: [ADR-20261025-4z5]
tags:
  - adr
---
# ADR-20261027-xrq: A rollback finishes when PostgreSQL is protected outside the server

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md)

`core.upgrade.postgres_backup` says how a PostgreSQL database is protected
before an upgrade. `pg_dump` puts a dump in the backup. `external` and `none`
put nothing there, and the operator confirms that when the backup is taken.
Rule 11 of ADR-20261025-4z5 restores every database with the files. A database
with no dump cannot be restored, and a rollback that refused it left the
upgrade journal in `rolling-back`, so the server refused every start. This
record changes rule 11 for those databases.

## rules

1. Restore the files, the SQLite databases, the dumped PostgreSQL databases
   and the previous binary when a trial fails, and finish as `rolled-back`.
2. Skip a PostgreSQL database whose backup entry is in `external` or `none`
   mode. Log a warning that names it, and record its name in the journal's
   `external_restore`.
3. Ask for the PostgreSQL acknowledgement before apply whenever a PostgreSQL
   database is outside the backup. That is the host database and every module
   database.

## enforcement

- `TestRunnerRollsBackWhenPostgresIsProtectedOutsideTheServer` fails a trial
  with a host and a module database in each mode and expects `rolled-back`,
  the restored files and binary, and both names in `external_restore`.
- `TestUpgradeApplyAsksToAcknowledgeAModulePostgresDatabase` covers rule 3 for
  a SQLite host.

## rejected

| option | why not |
|---|---|
| Refuse the rollback until the operator restores PostgreSQL | The journal stays in `rolling-back` and the server never starts again |
| Refuse apply unless every PostgreSQL database uses `pg_dump` | It removes the upgrade from installations whose database is backed up by its own service |
