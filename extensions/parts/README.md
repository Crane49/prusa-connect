# Parts

This module keeps a private catalogue of parts on one server. It works like a
small Printables that nobody outside the server sees. A person makes folders
and puts parts in them. Everything about a part is kept in one place: what it
is, how it is printed, the files behind it, and what people said about it.

A part has a name, a one-line summary, a long description, tags and comments.
Its files are its versions. A version is the set of files the part was at,
with a note on what changed, so the history of a part stays readable. A
version holds any file the storage takes: print files, 3MF projects, STL and
OBJ models, drawings and pictures. A print file of a version is printed from
the part's page, on a printer the person picks.

Files are added by dropping them onto a folder or a part, or with the Upload
files button and the file picker of the version dialog, which need no mouse.
A version that is not saved deletes the files it uploaded, whichever way its
dialog or the part's page is left.

The module ships two programs. This directory holds the Go program, which
serves the routes listed below. `ui/` holds the browser bundle, which draws the
Parts page. One release carries both, and one command installs them.

## Dashboard cards

The module adds two cards that a person can put on a dashboard.

| card | what it shows | its settings |
| --- | --- | --- |
| Parts folder | the newest parts of one folder, or its parts by name | the folder, the folders inside it, a tag, the order, how many parts |
| Chosen parts | parts a person chose one by one, in that order | the parts, found by a search of the catalogue, at most 12 |

Each card draws its own settings, because the core's form has no field for
a folder or a part of this module. The core still holds every value to the
fields in `module.yaml`. A card reads its parts once when it is drawn and
when its settings change, and each part links to its page in the catalogue.

## Where things are kept

| what | where | who writes it |
|---|---|---|
| folders, parts, versions, comments | a database of the module's own: a SQLite file or a Postgres database | this module |
| the files of a version | one of the server's storages, under `<root>/<part id>/` | the person, through the core's storage routes |

The module asks for a database of its own with `needs.database` in
`module.yaml`. The operator chooses where it goes when the module is
installed. The default is `database.sqlite` in the module's data directory. A
database on a Postgres server is the other choice. The module applies its
migrations when it starts, before it reports ready.

The Go program reads the database at the first request and keeps a copy of
the rows in memory while it runs. Every write goes to the database first, and
a change that touches several rows is one transaction. So a restart or an
upgrade leaves the catalogue as it was. One module process per catalogue is
supported. Do not run a second instance against the same database. Within
that process, changes are applied one at a time, and a version number is never
given out twice, also after the newest version was deleted.

### Upgrading from 0.2

Releases up to 0.2.3 kept the catalogue in the server's record store. The
first request to 0.3.0 moves those records into the module's own database, in
one transaction, and records that it did. A move that cannot read the record
store writes nothing, and the next request tries again. The move runs once.
The records stay in the record store, so 0.2.3, installed again, still finds
the catalogue as it was at the move.

The module never writes or deletes a file. The page uploads a file through the
core's storage routes, which take `write_storage` for the person. The page
then asks the module to record a version, and the module asks the storage, as
that person, whether each file is there and how long it is. A version records
the size and the digest the storage answered, never what the page said.

When a part or a version is deleted, the module answers which files nothing
names any more, and the page deletes them from the storage. When a part is
deleted, the module names every folder its files were kept in, also folders
from an earlier `storage` or `root`.

## Installation

The module needs a storage and a database of its own. The server's default
configuration declares a durable storage named `local`, which is the default
of the `storage` setting. The server needs no record backend.

A storage takes the extensions its `allowed_extensions` list names. The
default list takes print files, models and pictures. A catalogue that keeps
drawings or documents as well declares a storage of its own with an empty
list, which takes any file:

```yaml
resources:
  storage:
    catalogue:
      type: filesystem
      enabled: true
      config:
        root: /var/lib/prusa-connect/catalogue
        allowed_extensions: []
```

and names it in the module's settings, below.

One command builds the module, installs it into a demo server, restarts the
server, and drives the page in a browser:

```sh
make -C extensions check EXT=parts
```

That run also packs a release for the machine it runs on, in
`bin/parts/release` at the repository root. A release is the directory
`module add` reads: `module.yaml`, the binary for one platform, the bundle under
`ui/` and a `checksums.txt`. A release for the marketplace is built by the
workflow in [Releasing to the marketplace](../README.md#releasing-to-the-marketplace).

To install that release into a server of your own on the same platform:

```sh
prusa-connect module add github.com/prusa3d/parts@0.3.3 \
  -config /etc/prusa-connect/config.yaml \
  -from bin/parts/release -approve
```

This module asks for no permission of its own. `-approve` grants the database
and switches the instance on. The database is a SQLite file in the module's
data directory unless `-database` names another place:

```sh
prusa-connect module add github.com/prusa3d/parts@0.3.3 \
  -config /etc/prusa-connect/config.yaml \
  -from bin/parts/release -approve \
  -database postgres -database-dsn 'postgres://parts:secret@db.example.com/parts'
```

The Extensions page asks the same question in its install dialog. A core
before 0.0.1-alpha.7 does not read `needs.database` and refuses the release.

## Configuration

An operator writes a key under `config:` in the module's entry of the server
configuration file. The server refuses a key that is not in the table below,
and it does that before the module starts. A key the operator leaves out keeps
the default.

| key | type | default | summary |
|---|---|---|---|
| `storage` | string | `local` | The server storage the catalogue's files are uploaded to. |
| `root` | string | `parts` | The folder inside that storage the catalogue's files are kept under. |

```yaml
resources:
  sink:
    parts:
      config:
        storage: catalogue
        root: parts
```

A changed setting moves where new files go. The files already recorded keep
the storage and the path they were written under, so nothing is lost.

A `storage` that is not one word of at most 63 bytes, or a `root` that is not a
relative folder inside the storage, refuses the start. The instance shows the
reason.

## Permissions

The module acts only for the person who made the request. It runs no loop, no
poll and no work of its own, so it asks the server for no permission of its
own. `module.yaml` therefore carries no `permissions` list.

The server decides every call before the module sees it:

| what a person does | what the server checks |
|---|---|
| read the catalogue, a part, its versions and its comments | `view_resource` on this module |
| make, change or delete a folder, a part, a version or a comment | `operate_resource` on this module |
| upload a file, delete a file | `write_storage` on the storage |
| see a picture, download a file, record a file in a version | `read_storage` on the storage |
| print a file | `operate` on the printer, and `read_storage` on the storage |

One rule is the module's own: a comment is changed or deleted only by the
person who wrote it. Anybody else is answered `parts/NOT_AUTHOR`.

The page asks the server what the person may do, with
`POST /api/v1/access/check`, and draws no control the server would refuse. A
person who may only read sees the catalogue and every file, and no button that
changes them. Deleting a part also deletes its files, so the page offers it
only to a person who has both `operate_resource` and `write_storage`.

The module requires one method on the server, `storage.stat`, and
`module.yaml` lists it under `requires`. A server that does not serve it does
not start the module. The module's status then shows the error
`RESOURCE_DEPENDENCY_UNSERVED` with the name of the missing method. The module
also calls `state.get` and `state.list`, once, to move the records of a
release up to 0.2.3. A server with no record store has nothing to move.

## The routes

Every path below is relative to the address the server gives the module, so a
caller reaches the first one at `/api/v1/modules/<namespace>/overview`.

| method and path | what it does |
|---|---|
| `GET /overview` | the folder tree, the tags, the size of the database |
| `POST /folders` | make a folder |
| `PATCH /folders/{id}` | rename, move or describe a folder |
| `DELETE /folders/{id}` | delete an empty folder |
| `GET /parts` | one page of the parts that match a folder, a tag or a search |
| `POST /parts` | add a part |
| `GET /parts/{id}` | one part, with its versions and its comments |
| `PATCH /parts/{id}` | change a part |
| `DELETE /parts/{id}` | delete a part, and answer the folder its files are in |
| `POST /parts/{id}/versions` | add a version with the files the page uploaded |
| `PATCH /parts/{id}/versions/{number}` | rewrite the note of a version |
| `DELETE /parts/{id}/versions/{number}` | delete a version, and answer the files nothing names now |
| `POST /parts/{id}/comments` | write a comment |
| `PATCH /parts/{id}/comments/{comment}` | rewrite your own comment |
| `DELETE /parts/{id}/comments/{comment}` | delete your own comment |

`openapi.json` describes all of them. `openapi_test.go` compares that document
with the routes the module registers, one operation at a time. The typed
client under `client/` is generated from the same document.

## The limits

The catalogue's rows (folders, parts, versions and comments) live in the
module's own database, and its files in a storage. The server counts nothing
in that database, and its size is bounded by the disk or by the Postgres
server. The limits below are the module's own.

| what | limit |
|---|---|
| the name of a folder or a part | 120 bytes |
| the description of a folder | 2 KiB |
| the summary of a part | 280 bytes |
| the description of a part | 16 KiB |
| tags on one part | 16, each at most 32 bytes |
| folders inside folders | 12 deep |
| versions of one part | 200 |
| files in one version | 32 |
| the note of a version | 2 KiB |
| comments on one part | 500, each at most 4 KiB |
| the name of a file in a version | 200 bytes |
| the path of a file inside its storage | 512 bytes |

The module sets no limit on the size of a file. The core's upload route takes
up to `core.api.max_upload_size`, 200 MiB by default. A request body to the module is at most 160 KiB, so a field at
its limit is accepted however JSON spells it.

## Errors

When the module cannot do what a request asks, it answers with an error. The
error has a `code`, such as `parts/PART_NOT_FOUND`, and a `title` in English.
A program that calls the module should check the `code`. The title is for
people. The Parts page shows the error in the language the person chose. If it
has no translation for a code, it shows the English title.

| code | status | when the module answers it |
|---|---|---|
| `parts/BODY_TOO_LARGE` | 413 | the request body is over the module's bound |
| `parts/CHANGED` | 409 | somebody changed the record first. Read it again and send the change again |
| `parts/COMMENT_NOT_FOUND` | 404 | the part has no comment with that id |
| `parts/FILE_MISSING` | 422 | a version names a file the storage does not hold, or one the person may not read |
| `parts/FOLDER_NOT_EMPTY` | 409 | a folder that still holds a folder or a part was to be deleted |
| `parts/FOLDER_NOT_FOUND` | 404 | the catalogue holds no folder with that id |
| `parts/FULL` | 507 | a limit above is reached |
| `parts/INVALID` | 400 | a field over its limit, an empty name, a path outside the part's folder |
| `parts/INVALID_QUERY` | 400 | an id or a query parameter the module cannot read |
| `parts/NAME_TAKEN` | 409 | the folder already holds a folder of that name |
| `parts/NO_PRINCIPAL` | 403 | the server named no person for the request |
| `parts/NOT_AUTHOR` | 403 | a comment somebody else wrote was to be changed |
| `parts/PART_NOT_FOUND` | 404 | the catalogue holds no part with that id |
| `parts/STORE_FAILED` | 503 | the database or the storage could not answer, or the records of an earlier release could not be moved yet |
| `parts/VERSION_NOT_FOUND` | 404 | the part has no version with that number |

The error body follows RFC 9457, the standard format for HTTP problem details.

## The layout

| path | what is in it |
|---|---|
| `main.go` | the handshake, the method set, the wiring. No rule and no handler |
| `internal/catalog` | what a folder, a part, a version and a comment are, the rules, how each is stored, and the move out of the record store |
| `internal/settings` | the settings and the default of each |
| `internal/routes` | the HTTP handlers and the error codes |
| `internal/hostfake` | a record store and a storage in memory, so every test runs without a server |
| `internal/dbtest` | a fresh, migrated database for one test, on SQLite and on Postgres |
| `migrations/` | the database schema, one directory per engine |
| `openapi.json` | the routes, as a client generator reads them |
| `module.yaml` | the one manifest for both programs |
| `client/` | the typed client, generated from `openapi.json` |
| `ui/` | the browser bundle |

## Running the tests

```sh
cd extensions/parts
go build ./... && go vet ./... && go test -count=1 -cover ./...
cd ui && pnpm install && pnpm run check
```

The Go tests run on SQLite. They also run on Postgres when
`AUTHZ_TEST_POSTGRES_DSN` names a server, which `make test-engines` at the
repository root starts.
