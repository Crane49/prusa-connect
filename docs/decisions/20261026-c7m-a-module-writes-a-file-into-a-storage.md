---
title: "ADR-20261026-c7m: a module writes a file into a storage"
description: "storage.write: a module sends one file to a storage on a stream of its own, the host publishes it only when every byte arrived, and the call takes write_storage like the HTTP upload."
date: 2026-10-26
status: accepted
supersedes: []
amends: []
tags:
  - adr
  - modules
  - storage
---
# ADR-20261026-c7m: a module writes a file into a storage

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Related: [ADR-20260827-7jn](20260827-7jn-the-plugin-wire-is-a-specification.md), [ADR-20260825-smx](20260825-smx-storage-confinement-and-local-file-handles.md)

A module could read a file a storage holds, with `storage.stat` and
`storage.read`, and could not put one there. A module that makes a file, such
as a slicer that turns a project into G-code, could keep its result only in
its own data directory. No printer and no page reads that directory.
`storage.write` is the missing half. A module sends one file, and the storage
holds it like a file a person uploaded.

The body of a module route is limited to 1 MiB, and one frame on the wire is
smaller than that. A G-code file is often hundreds of megabytes. So the bytes
do not travel inside the call. They follow it on the call's own stream, with
the credit the wire already has, so the host reads them as they come and
nothing holds the whole file.

## rules

### the call

1. The method is `storage.write`. A module makes it on a stream it opened, and
   writes the file's bytes on that stream as data after the call.
2. The params are `principal`, `storage`, `path` and `size`. `size` is the
   number of bytes that follow, from 1 to 4 GiB (`storageapi.MaxWriteBytes`).
   An empty file is refused.
3. The host takes the call by writing one byte on the stream. The module
   sends no byte of the file, and does not end its half of the stream, before
   it read that byte. Opening a stream tells the host of it, so an end sent
   ahead of the call could reach the host first and close the stream before
   the handler ran.
4. A stream that carries fewer bytes than `size`, or more, is
   `ErrShortWrite`, code 5005. A module that stops partway resets its stream.
5. A write made as a plain call, with no stream, is `ErrShortWrite`.
6. The answer is `{path, size, sha256}`: the file as the storage holds it, and
   the digest the storage computed while it wrote.

### what the host does

7. The host writes through the same storage plane the HTTP upload goes
   through. It refuses a path that leaves the storage and a path inside the
   folder of temporary copies, as the upload does.
8. The storage writes under a temporary name and publishes the file only when
   the body ended cleanly. A failed or short write leaves the file that was
   there before, or none.
9. A file of the same name is replaced. A folder on the way is made.

### who may write

10. The call takes `write_storage` on the storage kind, the permission the HTTP
    upload takes.
11. A call naming a person runs as that person while the host is serving their
    request, held to the scopes of that request, as `storage.read` is.
12. A call naming nobody is the module's own work. It is held to the
    instance's `permissions:` list first, so an instance without
    `write_storage` in that list writes nothing, and then to the graph for
    the instance's own principal. Approving the install writes that grant on
    the storage kind (ADR-20261027-pq5), so the operator writes no grant of their own.
13. A refusal names no storage and no path. The reason is in the server's log,
    as it is for a read.

## enforcement

| rule | gate |
|---|---|
| 1, 2, 4, 5, 6 | the tests in `sdk/storageapi/write_test.go`, over a real session |
| 3 | `TestManySmallWritesAllEnd`, two hundred small writes on one session, which hung without the byte |
| 8 | `TestAModuleWriteThatFailsPartwayKeepsTheOldFile` in `internal/app` |
| 10, 12 | `TestAModuleWritesOnlyWhatItWasGranted` in `internal/app`, in both directions |
| 11 | `TestAWriteInAPersonsNameNeedsTheirOpenRequest` in `internal/app/resourcehost` |

## rejected

| option | why not |
|---|---|
| the bytes inside the call, in pieces of 512 KiB like a read | the host would hold a half-written file between calls, with no rule for when to drop it |
| the module writes into its own data directory and a person copies the file out | no printer and no page reads a module's data directory, and the copy is a second upload of the same bytes |
| a size of zero for a file of unknown length | the host then cannot tell a stream that ended early from a short file, and a short file would be published |
| a separate permission for a module's writes | the person or the instance is writing a file into a storage, which is what `write_storage` already names |

## open items

- a write cannot ask to keep an existing file. A module that must not replace
  one reads `storage.stat` first, and two writers can still race between the
  two calls
