---
title: "ADR-20261025-gve: a server answers to one id on every address"
description: "A random instance id, written once into the host database, read without a credential from GET /api/v1/health and announced as the id key in the mDNS record, so a client knows a server again at a new address."
date: 2026-10-25
status: accepted
amends: [ADR-20260916-b3x]
tags:
  - adr
  - discovery
  - mdns
  - api
---
# ADR-20261025-gve: a server answers to one id on every address

- status: accepted
- supersedes in part [ADR-20260916-b3x](20260916-b3x-instances-announce-themselves-on-the-network.md)
  rules 4 and 17. The TXT record has a fifth key, `id`. Rule 5 stands: the id
  is not a secret and names no account
- date: 2026-10-25

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Builds on [ADR-20260916-b3x](20260916-b3x-instances-announce-themselves-on-the-network.md), [ADR-20261016-pmj](20261016-pmj-a-table-schema-lives-beside-its-code.md)

A client finds this server on the network, or a person types its address.
Later the address changes: a new DHCP lease, a second network, a move to
another machine. The client then sees a server at a new address and cannot
tell whether it is the one it already knows or a second one. The title is
not an answer, because a person can change it and two servers can share it.
The host name is not an answer either, because the responder renames it on a
conflict. This record gives the server an id that does not change.

## rules

### what the id is

1. Every server has one instance id. It is a version 4 UUID, which is 122
   random bits, written as the usual 36 characters in lower case.
2. The id is random. It is derived from no secret, no host name, no address,
   no serial number and no configuration key, so it tells a reader nothing
   about the machine.

### where it is kept

3. The id is a row in the table `server_instance` in the host database. The
   package `internal/platform/instanceid` owns the table under ADR-20261016-pmj rule 1.
   The table holds one row, and a CHECK refuses a second one.
4. The server reads the row at every start, after the migration. When the
   table is empty it writes a new id first. The write is
   `INSERT ... ON CONFLICT DO NOTHING` followed by a read, so two processes
   that start at once against one Postgres database read one id.
5. The id never changes once written. A backup of the database carries it,
   so a server restored from a backup answers to its old id. Nothing in the
   API or the configuration sets it or replaces it.

### where it is read

6. `GET /api/v1/health` returns it as `instance_id`, always present. The
   route takes no credential, so a client reads the id before it signs in.
7. Every service record the core announces over mDNS carries the id as the
   TXT key `id`. The record then holds the four keys of ADR-20260916-b3x rule 4 and
   this one, and no other key. A record a module adds through
   `sdk/discoveryapi` carries no id, because that path is not built yet
   (ADR-20260916-b3x open items). When it is built, it takes the same key.

## enforcement

Tests in `internal/platform/instanceid` run on SQLite, and on Postgres under
`make test-engines`.

| rule | gate |
|---|---|
| 1, 2, 4 | `TestTheIDIsWrittenOnceAndReadBackAfterARestart` in `internal/platform/instanceid` |
| 2 | `TestTwoDatabasesHaveTwoIDs` in `internal/platform/instanceid` |
| 3 | `TestEveryCheckConstraintRefusesTheRowItWasWrittenFor` in `internal/app/hostschema` |
| 3 | the embed list in `internal/arch/migrations_test.go` |
| 5, 6 | `TestTheServerKeepsItsIDAcrossARestart` in `internal/app` |
| 6 | `TestHealthAnswersWithoutAnIdentity` in `httpapi` |
| 7 | `TestTheRecordCarriesTheFiveKeysAndNoOther` in `internal/discovery` |
| 7 | `TestTheAnnouncedRecordIsBrowsedBack`, which reads the record off the wire |

## consequences

- one more owner of the host schema, with one table and one row
- a client keys a server by the id and updates the address it stores when
  the same id appears at another one
- a database copied to start a second server gives both servers one id. The
  copy is a restore as far as the server can tell. A person who clones a
  server that way deletes the row in `server_instance` before the first start

## rejected

| option | why not |
|---|---|
| a file in the server's own `config.d` directory | a backup moves the database. Two processes on one Postgres would write two files |
| an id derived from the host name, a MAC address or the TLS key | it changes when the machine or the certificate changes, and it tells the network something about the machine |
| an id derived from a secret, such as the site token or a hash of it | a value sent to every machine on the link must not be a function of a secret |
| the id only behind a credential | a client needs it before it signs in, to know which stored server it is talking to |
| keep the id out of the TXT record | a client would ask every address it hears over HTTP to tell two records apart |
| `instance_id` as the TXT key | RFC 6763 section 6.4 asks for keys of at most nine characters, and every other key in the record is short |
| a UUID version 7 | it carries the time of the first start, which is a fact about the install the id does not need to tell |

## open items

- ADR-20261016-pmj rule 2 lists the owners of the host database. `internal/platform/instanceid` is the fifth.
- The list of printers nearby merges the records of one server by host name.
  It could merge them by the `id` key instead, and show the id to a client.
  That is a change to ADR-20260925-jne and is not made here.
