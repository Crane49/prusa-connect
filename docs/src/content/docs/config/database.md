---
title: "Database"
description: "The database the accounts, permissions, printers and their commands are kept in: SQLite in a file, or Postgres you run."
audience: run
rewrite:
  describes: current
  implementedBy:
    - internal/app/config
    - cmd/generate-config-reference
---

The server keeps its accounts, permissions, printers and their commands in one
database. A module may keep its data in a database of its own, which
[a module's own database](#a-modules-own-database) describes. `persistence.type` chooses it:

- **sqlite**, the default, is the file `config.d/authz.db` beside your
  configuration file, and `core.authz.database` moves it. It needs nothing
  else and suits one server well.
- **postgres** is a database server you run, named by
  `persistence.postgres.dsn`. Choose it when several servers share one
  database, or when you back up Postgres already.

A server started before its Postgres accepts connections, as compose,
Kubernetes or systemd may start them, waits up to 45 seconds for it and logs
each try. A database that is still not there by then stops the start, with
the last refusal as the reason.

The pool keys bound how many connections the server opens. The defaults suit
almost everybody. A printer's history is kept apart, in its own store: see
[Storage, history and integrations](resources.md).

## A module's own database

A module that keeps more data than the record store holds, such as the
history of every job, asks for a database of its own with
`needs.database: true` in its manifest. You approve it with the install, and
you choose where it goes.

### Choosing a place

| mode | where the data is | what you do first | choose it when |
|---|---|---|---|
| `sqlite` | `database.sqlite` in the module's data directory | nothing | one server, and you back up its folder |
| `postgres` | the database a `postgres://` URL names | make the database and a login for the module | the module's data belongs on a Postgres server you run |
| `provision` | a new database on this server's own Postgres server | run this server on Postgres | this server runs on Postgres, and it may make databases there |

`sqlite` is the default. The module's data directory is `module-data/<instance>`
beside the modules folder. The module creates `database.sqlite` there, readable
by the server's own account only.

Choose the place on the command line or in the Extensions page:

```console
$ prusa-connect module add github.com/acme/notes@1.0.0 -from ./release -approve -database provision
  database    postgres db.lan:5432/module_connect_example_database, made by this server
```

Over HTTP the place is `database` in the body of `POST /api/v1/modules`, such
as `{"mode": "postgres", "dsn": "postgres://notes:secret@db.lan:5432/notes"}`.
The server never answers the connection string back. A read of the module
says the engine, the server and the database name, and the schema version the
running module reported.

### What provision makes

With `provision` the server makes a role and a database, both named `module_`,
the name of this server's own database and the instance name. For a core
database `connect` and an instance `example-database` that is
`module_connect_example_database`. The name holds this server's database, so
two servers that share one Postgres server each make their own. The role has a
random password and may log in and do nothing else.

The server makes them with `persistence.postgres.module_admin_dsn`, or with
`persistence.postgres.dsn` when that key is empty. That role needs `CREATEDB`
and `CREATEROLE`. It does not need to be a superuser, and the module never
receives its login. A role or a database of that name that belongs to
something else is refused with `MODULE_DATABASE_NOT_MADE`, and nothing is
installed.

### Two Postgres servers

The core keeps its data on one server, and the module on another. Make the
module's database and its login on the second server:

```sql
CREATE ROLE notes LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE PASSWORD 'a-long-password';
CREATE DATABASE notes OWNER notes;
REVOKE ALL ON DATABASE notes FROM PUBLIC;
```

```yaml
persistence:
  type: postgres
  postgres:
    dsn: "postgres://connect:core-password@db-core.lan:5432/connect"
```

```console
$ prusa-connect module add github.com/acme/notes@1.0.0 -from ./release -approve \
    -database postgres -database-dsn "postgres://notes:a-long-password@db-modules.lan:5432/notes"
```

To have the server make module databases on its own server with a role that
does only that, give it a second login:

```sql
CREATE ROLE module_admin LOGIN CREATEDB CREATEROLE NOSUPERUSER PASSWORD 'another-password';
GRANT CONNECT ON DATABASE postgres TO module_admin;
```

```yaml
persistence:
  type: postgres
  postgres:
    dsn: "postgres://connect:core-password@db-core.lan:5432/connect"
    module_admin_dsn: "postgres://module_admin:another-password@db-core.lan:5432/postgres"
```

Then install with `-database provision`.

### One Postgres server for both

The core's own role may make the module's database when it has `CREATEDB` and
`CREATEROLE`:

```sql
CREATE ROLE connect LOGIN CREATEDB CREATEROLE NOSUPERUSER PASSWORD 'core-password';
CREATE DATABASE connect OWNER connect;
REVOKE ALL ON DATABASE connect FROM PUBLIC;
```

Install with `-database provision`, and leave `module_admin_dsn` out. Or make
the module's database yourself on the same server, as above, and install with
`-database postgres`. Each database refuses every role but its owner, so the
module cannot read the core's tables, and the core's role cannot read the
module's.

### Updates and migrations

A release of a module carries its migrations, and the module applies them when
it starts. Every migration one start applies runs in one transaction, so the
database moves to the new release's schema or stays as it was.

An update in the Extensions page or over HTTP keeps the old release aside
until the new one has started. A new release that does not start is taken
back. The old release runs again, and the answer is
`MODULE_UPDATE_ROLLED_BACK` with the reason. The same happens to a release
older than the database allows, which says `migrated by a newer release`.

`module add` writes for the next start and keeps no old release. If the new
release then does not start, the instance is failed and its database is as it
was. Install the previous release again with the same command.

### Moving a module's database

Install the same release again with another place:

```console
$ prusa-connect module add github.com/acme/notes@1.0.0 -from ./release -approve -database postgres -database-dsn "postgres://notes:a-long-password@db.lan:5432/notes"
  database    postgres db.lan:5432/notes
  left behind sqlite, database.sqlite in the module's data directory, which the module no longer uses. `module purge sink:example-database` deletes it
```

The server copies no data. The module starts with an empty database in the
new place, and the old one stays with its data until you purge it. To keep the
data, copy it before the module starts in the new place. Use the module's own
export and import when it has them, or a tool that copies a SQLite database
into Postgres. Moving back to the old place takes the old database back.

### Rotating a password

For a database this server made, install the same release again with
`-database provision`. The role gets a new password, and the data stays. For
a database you made, change the password on your server, then install the
same release again with the new connection string. The same database is not
listed as left behind.

### Removing and purging

A removal deletes the module's files and keeps its database with the data.
`module list`, `GET /api/v1/modules/databases` and the Extensions page list it.
Installing the same module under the same name again takes it back.

A purge deletes it, and cannot be undone:

```console
$ prusa-connect module purge sink:example-database
would drop    postgres db.lan:5432/module_connect_example_database, left behind by sink:example-database
Nothing was purged. A purge cannot be undone: run it again with -yes.
$ prusa-connect module purge sink:example-database -yes
dropped   postgres db.lan:5432/module_connect_example_database, left behind by sink:example-database
```

| the database | what a purge does |
|---|---|
| a SQLite file | deletes `database.sqlite` and the files SQLite keeps beside it |
| made by this server | drops the database and its role, on the server it was made on |
| one you made | forgets it, and leaves the database on your server for you to drop |

Over HTTP the purge is `DELETE /api/v1/modules/databases/{kind}/{name}` with
`confirm` repeating the instance as `kind:name`. The Extensions page asks you to
type the name. `-forget` on the command and `forget=true` over HTTP only forget
the record, for a database you dropped yourself. A database the server cannot
prove is the module's is refused with `MODULE_DATABASE_NOT_PURGED`.

### Backups

| place | what to back up |
|---|---|
| `sqlite` | `module-data/<instance>/database.sqlite`. Stop the server first, or copy it with the `.backup` command of `sqlite3` |
| made by this server | the database `module_<core database>_<instance>`, with `pg_dump` as a role that may read it |
| one you made | your database, the way you back up the rest of that server |

Back up `config.d/resources.json` with your configuration too. It holds the
grant, with the module's connection string, and the list of databases left
behind. Restore the module's database before you start a release older than
the one that wrote it.

### Trying it on your machine

`make postgres-dev-up` starts a Postgres for development, with one database
for the server and one for a module, each with its own login. It prints both
connection strings. Point the server at the first one:

```yaml
persistence:
  type: postgres
  postgres:
    dsn: "postgres://connect:connect@127.0.0.1:55433/connect?sslmode=disable"
```

Build with Postgres, `make build CONFIG_FILE=<this file>`, and start the
server. Then install a module with `-database postgres -database-dsn
"postgres://notes:notes@127.0.0.1:55433/notes?sslmode=disable"`, or with
`-database provision` to have the server make a new one. The role `connect`
may create databases and roles, so provision works without
`module_admin_dsn`. `make postgres-dev-down` stops the container and keeps
the data, and `make postgres-dev-reset` deletes it.

### How this was verified

`make module-database-e2e` runs the check below. It builds the server with
Postgres and the example module that keeps notes in five releases, and starts
two Postgres 18 containers, named `pcl-moddb-e2e-*`. It removes the containers and
their volumes when it ends. The nightly workflow runs it too.

On every layout below, one run takes the module through the same steps. The
steps are an install, a restart, an update with a schema and a data migration
and an update whose migration fails. Then come a fixed update, a downgrade
below the compat floor, a removal, a reinstall, a purge and a fresh install.

| layout | install, restart | update with migration | failed update taken back | downgrade refused | removal listed, reinstall finds data | purge | fresh install |
|---|---|---|---|---|---|---|---|
| core on SQLite, module on SQLite | pass | pass | pass | pass | pass | file deleted | empty |
| core on SQLite, module on your Postgres | pass | pass | pass | pass | pass | forgotten, database kept | finds the data |
| core on server A, module on server B | pass | pass | pass | pass | pass | forgotten, database kept | finds the data |
| core on server A, module made on A with `module_admin_dsn` | pass | pass | pass | pass | pass | database and role dropped | empty |
| one server, module made by the core's role | pass | pass | pass | pass | pass | database and role dropped | empty |
| one server, module in a database you made | pass | pass | pass | pass | pass | forgotten, database kept | finds the data |

The same run checks what is refused, and what two servers do at once:

| case | result |
|---|---|
| `provision` on a core that runs on SQLite | `REQUEST_INVALID`, nothing installed |
| a connection string that is not a `postgres://` URL | `REQUEST_INVALID`, the password not quoted |
| a database chosen without approve | `REQUEST_INVALID`; without approve the request is listed |
| a release that newly asks for a database | `MODULE_REQUESTS_CHANGED` |
| a database of the module's name that another role owns | `MODULE_DATABASE_NOT_MADE`, no role made |
| two cores on one Postgres server, both with `provision` | a database each, and neither reads the other's |
| two servers starting the module on one database at once | each migration applied once |

The run of 22 October 2026 passed every case above, on macOS on arm64 with
Go 1.26.10 and Docker, in 99 seconds. The unit and engine suites check the
same rules one by one, on SQLite and, under `make test-engines`, on Postgres.

## Keys

<!-- generated by make generate-config-reference for every key the core reads. Do not edit: change the struct and its row in config.Keys or resource.Keys, then regenerate. -->

### core

Every key has a working default, so a file with no `core:` block starts a server. A key this build does not read is refused at startup, with the line it is written on.

| key | type | default | required | what it does |
|---|---|---|---|---|
| <a id="key-core-authz-database"></a>`core.authz.database` | text | `config.d/authz.db` beside your file | no | The SQLite file that holds the whole database when persistence.type is sqlite. Postgres does not use it. The server creates it on the first run. |

### persistence

The database engine, shared by the permission graph, the device records and the command queue. It is at the top level of the file and is refused inside `core:`.

| key | type | default | required | what it does |
|---|---|---|---|---|
| <a id="key-persistence-type"></a>`persistence.type` | text | `sqlite` | no | The database engine: sqlite or postgres. Any other value stops the server. |
| <a id="key-persistence-max-open-conns"></a>`persistence.max_open_conns` | number | `16` | no | How many database connections this process opens at once, in total for all its stores. |
| <a id="key-persistence-max-idle-conns"></a>`persistence.max_idle_conns` | number | `8` | no | How many of those connections stay open while no query runs. |
| <a id="key-persistence-conn-max-lifetime"></a>`persistence.conn_max_lifetime` | duration | `30m` | no | How long one connection is used before it is replaced. A running server then picks up a database failover or a new password. |
| <a id="key-persistence-sqlite-page-cache-bytes"></a>`persistence.sqlite.page_cache_bytes` | number | `2048000` | no | How many bytes of database pages one SQLite connection keeps in memory. The default is SQLite's own. The printer build defaults to 524288. Refused on Postgres. |
| <a id="key-persistence-postgres-dsn"></a>`persistence.postgres.dsn` | text | - | when persistence.type is postgres | The connection string. The server passes it to the database driver and does not parse it. A postgres:// URL and a key=value string both work. |
| <a id="key-persistence-postgres-module-admin-dsn"></a>`persistence.postgres.module_admin_dsn` | text | - | no | The postgres:// URL the server makes and drops a module's database and role with. Its role needs CREATEDB and CREATEROLE. Empty uses dsn. Refused on sqlite. |
| <a id="key-persistence-postgres-statement-timeout"></a>`persistence.postgres.statement_timeout` | duration | `30s` | no | How long one statement may run on Postgres. A connection string that already names statement_timeout keeps its own value. |

<!-- end generated -->
