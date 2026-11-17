---
title: "ADR-20261021-bqc: a module may keep its data in a database of its own"
description: "The needs.database request: a module asks for a database of its own, the operator puts it in a SQLite file, on a Postgres server they name, or on the core's own Postgres server, the module migrates it in one transaction before it reports ready, an update that does not start is taken back, and a database no module uses any more stays listed until the operator purges it."
date: 2026-10-21
status: accepted
amends: [ADR-20260827-wgc]
tags:
  - adr
  - modules
  - database
  - migrations
---
# ADR-20261021-bqc: a module may keep its data in a database of its own

- status: accepted
- date: 2026-10-21

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Amends: [ADR-20260827-wgc](20260827-wgc-an-extension-reaches-persistence-through-the-sdk.md) rule 6 | Builds on: [ADR-20260827-rwq](20260827-rwq-a-module-keeps-its-data-out-of-the-host-schema.md) rules 3 to 7, [ADR-20261019-3ns](20261019-3ns-a-module-declares-its-ports-and-gets-a-data-directory.md), [ADR-20261007-br4](20261007-br4-an-operator-grants-a-module-room-for-its-records.md)

A module keeps small state in the record store, the key/value store the core
holds for every module. A module that keeps a long history, such as every
job of every printer, does not fit a key/value store held to a few MiB.
ADR-20260827-rwq rule 3 says such a module gets a store of its own. This record says
how a module asks for one and where the operator puts it. It also says how the
module migrates it, and what an update, a move and a removal leave.

A database of its own is one of three things, and the operator chooses:

- a SQLite file in the module's data directory. This is the default, and it
  needs no server
- a database on a Postgres server the operator names with a connection string
- a database the core makes for the module on the core's own Postgres server,
  with a login of its own

## rules

### what the manifest asks

1. A module asks with `needs.database: true` in `module.yaml`. Absent or
   `false` asks for none.
2. A module that asks ships its migrations for both engines, because the
   operator chooses the engine and the module does not.
3. A module that uses the key states the first core that reads it in its
   `core` range. A core range that names a prerelease of the core's own three
   numbers, such as `>=0.0.1-alpha.7`, is held against the whole version. Any
   other range is held against the three numbers alone. A core that does not read
   the key refuses the manifest before anything is written, with
   `needs carries database, which this version does not read`.

### what the operator approves and chooses

4. `database` is approved with `approve`, together with permissions, events,
   records and the data directory. Its request line is `database of its own`.
5. The operator chooses where the database is, with `database` beside
   `approve` over HTTP (`{"mode": "...", "dsn": "..."}`) and with `-database`
   and `-database-dsn` on `module add`:

   | mode | where the data is | who makes the database |
   |---|---|---|
   | `sqlite` | `database.sqlite` in the module's data directory | the module, at its first start |
   | `postgres` | the database a `postgres://` URL names | the operator, before the install |
   | `provision` | a database on the core's own Postgres server | the server, at the install |

6. No mode on an update keeps what the declaration grants. No mode on a first
   install takes back the newest database the same module left behind under
   the same instance name (rule 26), and is `sqlite` when there is none.
7. A mode or a connection string without `approve`, or for a manifest that
   asks for no database, is refused with a usage error, which HTTP answers as
   `REQUEST_INVALID`. So is `provision` on a server whose
   `persistence.type` is not `postgres`, and a `dsn` that is not a
   `postgres://` or `postgresql://` URL. No refusal quotes the connection
   string.
8. A database the manifest asks for and the operator was not shown refuses an
   approved install with `MODULE_REQUESTS_CHANGED`, as any other request does
   (ADR-20261005-64j rule 10). An update that adds the request lists it under
   `added`.

### what the declaration records

9. The declaration records the grant as `service.database`, with `engine`
   (`sqlite` or `postgres`), `dsn` for Postgres, and `provisioned: true` when
   the server made the database. The operator may edit it, and the module reads
   the new value at its next start.
10. The connection string is a credential. The machine-owned overlay holds it
    at mode `0600`, as it holds every other credential option. A resource read
    never answers it. `module.database` on the listing carries the engine,
    whether the server made it, and the server's host, port and database name.
    It also carries the schema the running module reported (rule 20). The diagnostics bundle
    removes the password of every granted and every left-behind database
    wherever it appears.
11. `persistence.postgres.module_admin_dsn` is refused at start unless it is a
    `postgres://` URL with a host and a database, and on a server whose
    `persistence.type` is `sqlite`.

### how the server makes a database

12. `provision` connects with `persistence.postgres.module_admin_dsn`, and with
    `persistence.postgres.dsn` when that is empty. The role it connects as
    needs `CREATEROLE` and `CREATEDB`. The server never needs a superuser.
13. It makes a role and a database, both named `module_` followed by the name
    of the core's own database and the instance name, such as
    `module_connect_notes`. The name holds only lower case letters, digits
    and underscores. So two cores whose
    databases share one Postgres server each make their own. A name over 63
    characters is refused. The role may log in and may do nothing else: no
    superuser, no new databases, no new roles, no replication. Its password
    is 256 random bits. The database is owned by the role, `PUBLIC` loses
    every right on it, and the comments of both name the module's type.
14. The module's connection string takes the host, the port and the TLS
    settings of the connection the database was made with, with the module's
    role, its password and its database. The module never receives the core's
    credentials or the admin's.
15. A role of that name whose comment names another module, or nothing, is
    refused with `MODULE_DATABASE_NOT_MADE`, and so is a database of that name
    owned by another role. A role that names the same module is taken back with
    a new password, and its database is kept. Naming `provision` again on an
    installed module is how its password is rotated.
16. The database is made before the declaration is written. A failure writes
    nothing and installs nothing.

### how the module migrates

17. The host sends the grant in `plugin.configure` as `grants.database`, with
    `engine` and `dsn`. For SQLite the `dsn` is the absolute path of the file.
    The host makes the data directory first, under ADR-20261019-3ns rule 12, even when
    the manifest asks for no data directory.
18. The module opens the database itself and migrates it before it answers
    `plugin.configure`, so it never reports ready on a schema it has not
    applied (ADR-20260827-rwq rule 5). A migration that fails refuses the start.
19. `sdk/moduledb` opens the database and runs the migrations. A migration is
    a SQL file, `migrations/sqlite/<version>_<what>.sql` and
    `migrations/postgres/<version>_<what>.sql`, in a file system the module
    embeds. Or it is a Go step with a version of its own, for a change SQL
    cannot say, such as data read, transformed and written back. They run in version
    order, a step after the file of its version, and each is recorded once in
    `module_migration` with its version and a checksum.
20. Every migration one start has to apply runs in one transaction: the
    database moves to the release's highest version, or it stays exactly as it
    was. Postgres takes an advisory lock inside that transaction, and SQLite
    takes its write lock when the transaction begins (`BEGIN IMMEDIATE`), so
    two starts never migrate one database at once. The module answers
    `plugin.configure` with the schema version and the migrations it applied,
    and the host shows them on the resources listing and in the install's
    answer. The host never opens the module's database.
21. A statement that cannot run inside that transaction is refused before
    anything runs. Those are `CONCURRENTLY`, `VACUUM`, a statement on a whole
    database, `ALTER SYSTEM`, the SQLite pragmas a transaction ignores, and a
    transaction statement of its own. There is no opt-out. Such work belongs in
    the module's own code after it starts.
22. A file that changed after it was applied is refused. So is an applied
    migration the release no longer ships, at or below its own highest
    version, and a new migration at or below the version the database holds.
    There are no down migrations.
23. A module store carries its compat floor as an integer in `module_schema`
    (ADR-20260827-rwq rule 6). A release whose highest version is below the floor a
    database records refuses it before it writes anything. A release below the
    database's version and at or above its floor opens it and applies
    nothing.

### what an update, a move and a removal leave

24. An update installed over HTTP keeps the release it replaces aside until
    the new one has started, and waits up to a minute for it. A new release
    that fails to start is taken back, and that includes a migration that
    failed and a release below the compat floor. The old files, the old
    declaration and the old grants return, the old release runs again, and
    the answer is `MODULE_UPDATE_ROLLED_BACK`. The database needs no repair,
    by rule 20.
25. `module add` writes for the next start and keeps no previous release. A
    release that then does not start leaves the instance failed and the
    database as it was, and installing the previous release again restores
    the module.
26. A removal leaves the database where it is: the SQLite file in the data
    directory, and the Postgres database and role on their server (ADR-20260827-rwq
    rule 7). So does an update that names another mode, or that grants no
    database. Each records the database in the overlay as left behind, with
    the instance, the module's type, the grant, the time and the reason
    (`removed`, `moved` or `not_granted`). An install of the same module type
    under the same instance name that grants the same place takes the
    database back, data and all, and the record goes.
27. `GET /api/v1/modules/databases`, `module list` and the Extensions page
    list the databases left behind, to a caller who may configure the
    instance's kind.
28. A purge is the operator's explicit act. It is `DELETE
    /api/v1/modules/databases/{kind}/{name}` with `confirm` repeating the
    instance, `module purge <name> -yes`, or the Extensions page after the
    person types the instance name. It drops a database and a role the server
    made, on the server it made them, and only when their comment and owner
    are the module's. It deletes a SQLite file and the two files beside it. It
    only forgets a database the operator named. `forget` forgets any record
    and touches nothing. A database a declaration grants again is never
    purged. A failure answers `MODULE_DATABASE_NOT_PURGED` and keeps the
    record.
29. The server copies no data between places. A module that wants its data
    moved offers an export and an import of its own.

### what the record store means beside it

30. A module with a database of its own keeps its record namespace and the
    default `records_mib` grant (ADR-20261007-br4). The record store suits the
    settings and the cursors the module needs before or without its
    database, and the database holds the rest. Such a module asks for no
    more than the default room. The host counts nothing in the module's own
    database. Its size is bounded by the disk or by the Postgres server.

### the files and the drivers

31. `sdk/moduledb` creates the SQLite file at mode `0600`, and SQLite gives
    the files it keeps beside it the same mode.
32. `sdk/moduledb` imports only the standard library (ADR-20260824-gfr). The module
    registers the drivers: it imports `modernc.org/sqlite` and
    `github.com/lib/pq`, or any driver it names to `sdk/moduledb`. It is the
    one package under `sdk/` that imports `database/sql`, which ADR-20260827-wgc
    rule 6 refuses elsewhere. Its handle reaches the module's own database
    and never the core's.
33. A `//go:embed` naming a migration directory is admitted in a `main`
    package under `extensions/` or `examples/`. A `main` package is a module's
    own binary, and no other package can import it, so its migrations never
    reach the host's binary or the host's ledger (ADR-20260827-rwq rule 4).

## enforcement

`test/moduledatabase` runs rules 5 to 28 end to end, on the built server with
`examples/database-module`, over six layouts of SQLite and two Postgres
containers. `make module-database-e2e` runs it, and so does the nightly
workflow.

| rule | gate | state |
|---|---|---|
| 1 | `sdk/manifest` needs tests: the key read, refused when it is not a bool | exists |
| 3 | `TestACoreRangeTellsPrereleasesOfOneReleaseApartOnlyWhenItNamesOne` in `sdk/manifest` | exists |
| 4, 6, 7 | `internal/app` module database tests: a grant, each refusal, an update that keeps the mode | exists |
| 8 | `internal/app` module database tests, and `TestARefusedDatabaseInstallsNothing` in `test/moduledatabase` | exists |
| 9 | `internal/app/config/install` database tests: a grant and each refusal at load | exists |
| 10 | `internal/app/resource` tests: no connection string on the listing, and the secret sweep | exists |
| 11 | `TestTheModuleAdminConnectionIsHeldToAPostgresURL` in `internal/app/config` | exists |
| 12 to 16 | `internal/app` provisioning tests on the Postgres fixture, under `make test-engines` | exists |
| 13 | `TestTwoCoresOnOnePostgresServerEachMakeTheirOwnModuleDatabase` in `test/moduledatabase` | exists |
| 17, 18 | the `internal/app` serve tests start `examples/database-module` | exists |
| 19 to 23 | `sdk/moduledb` tests over a driver of their own, and `examples/database-module` tests on both engines | exists |
| 24 | `TestAnUpdateWhoseMigrationFailsIsTakenBack` in `internal/app`, and `test/moduledatabase` | exists |
| 25 | none. A command makes no start to judge | review |
| 26 to 28 | `internal/app` purge tests, `httpapi` contract tests, the web app's `left-databases` tests, `test/moduledatabase` | exists |
| 29 | none. Nothing copies a module's data | review |
| 30 | none. The default room is ADR-20261007-br4's | review |
| 31 | `TestTheSQLiteFileIsTheModulesAlone` in `examples/database-module` | exists |
| 32 | `TestTheSdkImportsOnlyTheStandardLibrary` in `internal/arch`, with `sdk/moduledb` as its one `database/sql` exception | exists |
| 33 | `TestTheEmbedNamesOnlyTheHostsOwnMigrationDirectories` in `internal/arch` | exists |

## rejected

| option | why not |
|---|---|
| the module's tables in the core's own database | ADR-20260827-rwq rule 1. It shares the core's lock, its ledger and its start |
| a schema of the module's own in the core's database | ADR-20260827-rwq rejects it: the module's locks block the core, and its pool counts against the core's connections |
| the host runs the module's migrations | ADR-20260827-rwq rule 5. A failed migration would stop the core's start, not the module's |
| the host passes an open handle to the module | a handle does not cross a process boundary |
| the module receives the core's credentials | a module could then read and write every table of the core |
| a superuser to make the database | `CREATEROLE` and `CREATEDB` are enough, and a superuser can read every database on the server |
| `sdk/moduledb` imports the drivers | ADR-20260824-gfr. Every module that compiles the sdk would compile both drivers |
| the module chooses the engine in its manifest | the operator knows the machine. A small installation runs on one file, a large one on the server it already backs up |
| a transaction per migration | a release whose third migration fails leaves the database between two releases, and neither release reads it |
| an opt-out from the transaction for one migration | the upgrade could then not be undone whole, and rule 24 takes it back on that promise |
| the server copies the data when the mode changes | a copy between engines is the module's schema, and the server does not know it |
| dropping the database on removal | ADR-20260827-rwq rule 7. A reinstall would find nothing |
| a purge with no confirmation | a purge cannot be undone, and a click on the wrong row is the likely mistake |
| a capability in `requires` that says the core reads `needs.database` | an older core refuses the unknown key before it reads `requires` |
| the instance name alone as the provisioned name | two cores on one Postgres server would share one database and take each other's role |

## open items

- the host does not check that a module opens only the database it was
  granted. A module process can reach anything its machine can
- `module add` cannot take an update back by itself (rule 25). The operator
  installs the previous release again
