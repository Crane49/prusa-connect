---
title: "ADR-20261023-sns: a module lists a storage and owns one the core prints from"
description: "A module may list a folder of a server storage with storage.list, under the same permission and confinement as storage.read. A module with a data directory may print a file from its print/ folder by naming the storage module:data in device.print. Nobody else reads that folder."
date: 2026-10-23
status: accepted
supersedes: []
amends: []
tags:
  - adr
  - modules
  - storage
---
# ADR-20261023-sns: a module lists a storage and owns one the core prints from

- status: accepted
- date: 2026-10-23

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Builds on [ADR-20261019-3ns](20261019-3ns-a-module-declares-its-ports-and-gets-a-data-directory.md), [ADR-20260825-smx](20260825-smx-storage-confinement-and-local-file-handles.md) | Needed by [ADR-20261023-7r3](20261023-7r3-fractal-files-are-shared-folders-fetched-over-https.md)

A module can read one file of a server storage, with `storage.stat` and
`storage.read`, and can ask the core to print a file a storage holds, with
`device.print`. It cannot list a folder, and it cannot print a file it
produced itself, because it can write to no storage. A module that publishes
the files of a folder needs the first. A module that receives a file from
elsewhere and prints it needs the second. Fractal file distribution needs
both, but neither change names Fractal.

## rules

### listing a folder

1. `storage.list` answers the entries of one folder of a server storage. An
   empty path is the root of the storage. For each entry it gives the name,
   whether it is a file or a folder, the size and the time it last changed. It
   also gives the SHA-256 the storage keeps for it, under rule 3. A link and a
   special file are not listed.
2. It runs under the permission and the confinement of `storage.read`. A call
   runs as the person it names, or as the module's instance inside the ceiling
   the operator wrote. The storage plane decides `read_storage`.
3. `storage.list` and `storage.stat` give a stored SHA-256 only when it is not
   older than the file. The disk store compares the time of its `.pcl-meta`
   file with the time of the file. A file changed in place, by a copy or an
   editor, then reports no digest until the store hashes it again.
4. It answers at most `MaxListEntries` entries, a constant of `sdk/storageapi`
   set to 1000, in the order of their names, and a cursor for the rest. A
   module lists a large folder in pages.
5. A module names `capability: storage.list` in `requires`. A host that does
   not serve the method does not start the module, as for every capability,
   and the operator approves the module with its list.

### the folder a module prints from

6. A module whose data directory was granted (ADR-20261019-3ns rule 3) may print a
   file from the folder `print/` of that directory. It names the storage
   `module:data` in `device.print`, and the source path is relative to
   `print/`. The call needs `read_storage`, as a print from any storage does.
7. `module:data` names the calling module's own folder, and no other. No
   storage resource can take that name, because a resource name is a DNS
   label (`internal/kernel/ref`) and holds no colon. A module without a data
   directory is refused.
8. The core reads the folder confined to it, as it reads a storage of type
   `local` (ADR-20260825-smx). A link that leaves the folder is refused.
9. The core only reads it, for `device.print` from that module. The storage
   API, the page of storages, a person and other modules do not see it.
10. The module writes and deletes the files in `print/` itself, on its own
    disk. The core keeps no digest and no index of them.

### a name the printer already holds

11. A print from `module:data` sends nothing and starts the printer's copy
    when this process sent the same file of `print/` to that path and neither
    side changed since. A print from any storage does the same. The version of the
    file is the module's data directory, its time of change and its length, so
    a file of another module's folder is never taken for it. After a restart of
    the server nothing is remembered.
12. In every other case, `device.print` onto a path where the printer
    already holds a file or a folder sends nothing. It answers
    `deviceapi.ErrNameTaken`, code 3009, whatever storage it names. A module
    can tell this refusal from a failure of the host and print the file under
    another name.

## enforcement

| rule | gate | state | what it refuses |
|---|---|---|---|
| 1, 2, 4 | a test of the module storage wire in `go test ./internal/app/` | exists | a listing past the bound, out of order, outside the storage, or as a person without `read_storage` |
| 3 | a test of the disk store in `go test ./adapters/storage/diskstore/` | exists | a digest older than its file |
| 5 | a test of the capabilities the host serves, in `go test ./internal/app/resourcehost/` | exists | a host that does not list `storage.list` among them |
| 6 to 9 | a test of module prints in `go test ./internal/app/` | exists | a print from `module:data` without a data directory, a path outside `print/`, or another module's folder |
| 9 | a test of the storage plane and of a print over HTTP in `go test ./internal/app/` | exists | `module:data` in the list of storages, and a person who prints from it |
| 10 | none. Review checks that the core keeps no digest or index of `print/` | | |
| 11 | `TestAModulePrintOfItsOwnFileAgainStartsTheHeldCopy`, in `go test ./internal/app/` | exists | a second print of an unchanged file of `print/` that sends it again or is refused |
| 11 | `TestAModulePrintNeverStartsAnotherModulesCopy`, in `go test ./internal/app/` | exists | a copy one module sent that the core starts for another module |
| 12 | `TestAModulePrintOntoAHeldNameIsNameTaken`, in `go test ./internal/app/` | exists | a print onto a held name that a module cannot tell from a failure of the host |

## consequences

- a module that lists a storage can learn every name in it that its ceiling
  allows, as it can read every file already
- a module can print a file the operator never placed in a storage. The
  operator approved the data directory and `device.print`, and both are on
  the install's list of requests
- a file in `print/` takes disk space no storage reports. The module that
  writes it bounds it
- `os.Root` does not detect a hard link. The core reads a hard link in
  `print/` to a file outside it. This matters only when the module runs as
  another user of the operating system than the core

## rejected

### a write to a storage of type `temporary`

Smaller, and the core already prints from any storage. A file a module put
there shows as a file of the server in the storage API and its page, which is
wrong for a file that came from elsewhere.

### a new key in `needs` for the folder

ADR-20261019-3ns rule 1 allows only `listen` and `data` in `needs`, and an accepted
record is not edited. The data directory already says the module keeps files,
so a fixed folder in it needs no new grant.

### streaming the file from the module to the printer

A printer asks for ranges and asks again, so the module would have to hold the
file anyway. The core's transfer reads a file, and this keeps it so.

## open items

- a file replaced by a copy that keeps its old time of change, such as one
  made by `rsync -a`, keeps its old digest. The `.pcl-meta` file records no
  size to compare
