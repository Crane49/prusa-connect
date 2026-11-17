---
title: "ADR-20261027-kqd: a migration is named by the date it was written"
description: "A new migration file is named by the UTC date and time it was written, the ledger is judged by name, a migration missing from the ledger is applied wherever it sorts, the compatibility floor names a migration, and no migration holds a statement that ends its transaction."
date: 2026-10-27
status: accepted
supersedes: []
amends: [ADR-20261016-pmj, ADR-20261021-bqc]
tags:
  - adr
  - schema
  - migrations
---
# ADR-20261027-kqd: a migration is named by the date it was written

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Amends: [ADR-20261016-pmj](20261016-pmj-a-table-schema-lives-beside-its-code.md) rules 6 and 9, [ADR-20261021-bqc](20261021-bqc-a-module-may-keep-its-data-in-a-database-of-its-own.md) rules 19, 22 and 23

A new migration file is named by the date and time it was written, in UTC, to
the second: `20261027T093015_device_notes.sql`. It never takes the next
number. A migration is a SQL file that changes a database schema, and the
ledger is the table that records which migrations a database has applied.

Numbers caused two failures. Two pull requests that each added a migration
took the same number, and the second one to merge had to rename its file. And
a runner that numbered the files across all owners of the host database moved
every later file when one owner grew. Every database an earlier release had
made then refused to start, with "an applied migration is not in this build".
A date is picked alone, so two pull requests never take one name, and the
order of two migrations is the order they were written in.

These rules hold for the host's database (`internal/kernel/sqlstore`), for an
adapter's own store (`adapters/db/internal/engine`) and for a module's own
database (`sdk/moduledb`).

## rules

### how a file is named

1. A new migration file is named `<date>T<time>_<words>.sql`. The date and
   time are UTC and are when the file was written, as
   `date -u +%Y%m%dT%H%M%S` prints them. The words are lower case letters and
   digits, joined by single underscores. `make migration-new DIR=<package>
   NAME=<words>` writes the file for both engines with one name.
2. The SQLite and the Postgres directory of an owner hold the same file names.
3. A numbered file, such as `0002_job_objects.sql`, that exists today keeps its
   name. Its name is in the ledger of real databases. No new file takes a
   number.
4. A shipped file is never edited, renamed or deleted. A change to the schema
   is a new dated file.
5. A Go step of a module database is named the same way. `Step.Stamp` gives
   its date, and the ledger records it as `<date>T<time>_<name>.go`. A step
   that shipped with `Step.Version` keeps it.

### the order they run in

6. Numbered files run before dated files. In the host database the numbered
   files run owner by owner, in the order `internal/app/hostschema` lists the
   owners. In a module database they run by number.
7. Dated files run by date across all owners. At one date the owners keep
   their order, and a module runs a file before the step of the same date.
8. A migration the ledger does not record is applied, even when it sorts
   before a migration the ledger records. Two pull requests that each added one
   merge into a release that ships both, and a database a build of either one
   migrated takes the other.

### what the ledger may hold

9. A ledger row is judged by its name. No number or position is recorded or
   compared. A file whose checksum differs from the one the ledger recorded is
   refused (`ErrMigrationChanged`).
10. A ledger row this build does not ship is a newer build's when it is dated
    and sorts after every file this build ships of the same owner. A row for
    an owner this build does not have counts too. The build opens that
    database, and the compatibility floor decides whether it may. Every other
    row is a file that was renamed or deleted, and the build refuses the
    database with `ErrMigrationMissing` before it writes anything.
11. In a module database a numbered row may also be a newer release's, because
    a module outside this repository may still number its files. The rule is
    the same: it sorts after every migration the release ships.
12. The host ledger `schema_migration` has the columns `name`, `checksum`,
    `binary_version` and `applied_at`. A database a build of the upgrade
    branch migrated may have one more column, `version`. It is nullable,
    nothing reads it, and a new row leaves it empty.

### the compatibility floor

13. The compatibility floor names a migration: `Schema.Floor` in the host,
    `Ledger.Floor` in an adapter store, `Schema.Floor` in `sdk/moduledb`. It
    is the newest migration an older build cannot survive. A build that does
    not ship that migration refuses the database before it writes anything.
    It is stored in `min_migration`, beside the older number, and it only
    moves to a migration that runs later.
14. Set the floor only for a migration an older build cannot survive, such as a
    dropped column it still reads. A migration that only adds something leaves
    it alone. The floor must name a migration the build ships, or the build
    refuses to start.
15. The number in `min_schema_version` stays for builds that know no names.
    In the host it is a count of files. When a build records a floor
    migration, it raises the count to the number of files it ships, so a build
    from before dated names refuses the database. In an adapter store and a
    module database it is a numbered version. A build that finds a floor
    migration recorded judges by the name alone.
16. `Schema.Compat` of the host stays at the count the numbered files left, and
    a module or an adapter store keeps its `Compat` version. A new floor is a
    name.

### what a migration may hold

17. A migration holds no statement that begins, ends or prepares a
    transaction (`BEGIN`, `COMMIT`, `END`, `ROLLBACK`, `ABORT`,
    `START TRANSACTION`, `PREPARE TRANSACTION`). It holds no `CONCURRENTLY`,
    `VACUUM`, `ALTER SYSTEM`, statement on a whole database, or SQLite pragma
    a transaction ignores. All migrations of one upgrade run in one
    transaction. A `COMMIT` inside a migration would keep what ran before it
    when the upgrade fails or a rehearsal rolls back. The body of a trigger or
    of a function is part of its `CREATE` statement and may hold `BEGIN` and
    `END`.
18. The runner refuses such a file before anything runs, with
    `ErrOutsideTransaction`, in all three places.

## enforcement

`TestEveryMigrationIsDatedAndNoneIsRenamed` in `internal/arch` holds rules 1
to 4 and 17 over every migration file in the tree. It lists every numbered
file. It refuses a new number, an undated name, and a date before 2026-10-27
or in the future. It also refuses a numbered file that is gone, two engines
with different names, and a statement rule 17 refuses. `TestTheMigrationNameGateRefusesEachRule` is
its mutation check. `TestPublishedMigrationBytesNeverChange` holds rule 4 for
the files a release shipped, and `ErrMigrationChanged` holds it at start.

The other rules have these tests. The host and adapter cases run on SQLite
always, and on Postgres under `make test-engines`.

| rule | where the test is |
|---|---|
| 5, 6, 7 | `sdk/moduledb`: the order of numbered and dated names, files and steps |
| 8 | `internal/app/hostschema`, `adapters/db/internal/engine`, `sdk/moduledb`, `examples/database-module` |
| 9, 10, 11 | `internal/app/hostschema`, `adapters/db/internal/engine`, `sdk/moduledb` |
| 12 | `TestALedgerWithTheBranchVersionColumnStillStarts` in `internal/app/hostschema` |
| 13 to 16 | the floor cases in `internal/app/hostschema`, `adapters/db/internal/engine`, `sdk/moduledb` |
| 17, 18 | `TestATransactionStatementIsRefused` in `sdk/moduledb`, and a case per runner |

## rejected

| option | why not |
|---|---|
| keep numbers and renumber on a conflict | a build of the branch has already recorded the old name |
| number the files across all owners | one owner that grows moves every later number, and older databases refuse to start |
| minutes and a random suffix, like record ids | seconds and the words already tell two files apart, in an order a person chose |
| apply only migrations after the newest applied one | a migration from a parallel pull request with an earlier date never runs |
| take every unknown ledger row as a newer build's | a renamed or deleted file then passes in silence |
| keep the floor as a count of files | a count set in one pull request is wrong after another adds a file first |
| rename the numbered files to dates | their names are in real ledgers, and those databases would refuse to start |
| a ledger per owner in the host database | ADR-20261016-pmj rejects five ledgers, locks and floors over one database |

## open items

- The upgrade backup inspection in `internal/upgrade` reads the ledger
  `version` column when a database has it, and reads the floor as a number
  only. It should report the floor migration too.
- The module schema version on the resources listing is the highest numbered
  version, so a module that has moved to dated files shows the same version
  after each upgrade. The listing should show the newest migration name as
  well.
- A file renamed to an earlier date than the one the ledger holds looks like a
  newer build's row. The renamed file then runs a second time and fails, which
  rolls the whole migration back, but the error names the failed statement and
  not the rename.
