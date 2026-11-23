---
title: "ADR-20261123-vvb: the core deletes a file a module hands it"
description: "A module that prints a file of its print folder may ask the core to delete the file once the print has settled. A module may move a file of that folder into a storage with storage.move. Either way the module keeps no timer and sends no byte."
date: 2026-11-23
status: proposed
supersedes: []
amends: [ADR-20261023-sns]
tags:
  - adr
  - modules
  - storage
---
# ADR-20261123-vvb: the core deletes a file a module hands it

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Builds on [ADR-20261023-sns](20261023-sns-a-module-lists-a-storage-and-owns-one-the-core-prints-from.md), [ADR-20261120-xys](20261120-xys-a-module-route-may-take-no-caller-or-a-large-body-written.md)

A module that takes a large upload on a staged route (ADR-20261120-xys) gets
the file in its data folder. To print it, the module moves it into `print/`
and calls `device.print` with the storage `module:data`. The call answers when
the transfer has started. The module learns when it ended only from the event
stream, so a module that wants its folder clean has to watch events or keep a
timer. The first slicer modules kept a timer: every file stayed for a day.

The core already knows when a transfer ended. It deletes a temporary copy a
person did not keep at that moment, by the rule in `sendSettled`. This record
lets a module ask for the same rule on a file of its print folder.

A module that keeps an upload in a storage had to read the file and send every
byte back over `storage.write` (ADR-20261026-c7m). The host already holds the
file on its own disk, so `storage.move` lets the host take it from there.

## rules

1. `device.print` takes `delete_source`. It needs the storage `module:data`.
   On any other storage the call is refused with `ErrInvalidQuery`.
2. With `delete_source`, the core deletes the source file from `print/` once
   the print leaves nothing to send again. That is when the file landed on
   the printer, the printer already held the same file, or a person stopped
   the transfer. A temporary copy is deleted by the same rule.
3. A send that failed keeps the file, so the module can print it again. The
   module deletes such a file itself.
4. The core resolves the name under the folder, as it does to read it
   (ADR-20261023-sns rule 8). A name that leaves `print/` deletes nothing.
5. A module does not name the same file in a second call before the first
   print has settled. The core counts no users of a file of `print/`.

### storage.move

6. `storage.move` takes `principal`, `source`, `storage` and `path`. `source`
   is relative to `print/`. The answer is `{path, size, sha256}`, as for
   `storage.write`.
7. The host publishes the file through the storage plane, under the
   permission and the confinement of `storage.write` (ADR-20261026-c7m rules 7
   to 12). Then it deletes the source from `print/`.
8. A move that fails publishes nothing and leaves the source in `print/`.
9. A source that is not a regular file in `print/`, or that leaves it, is
   `ErrNoFile`. A module without a data directory is refused with `ErrDenied`.
10. A module names `capability: storage.move` in `requires`.

These rules amend ADR-20261023-sns rules 9 and 10. The core still reads
`print/` only for that module, and now also deletes from it, but only the file
that module named.

## enforcement

| rule | gate |
|---|---|
| 1 | `TestAPrintNamingHalfASourceIsRefused` and `TestAWholePrintRequestPasses` in `sdk/deviceapi` |
| 2 | `TestAPrintThatGivesItsFileAwayDeletesItOnceSent` in `internal/app` |
| 2, 3, 4 | `TestOnlyASettledPrintThatGaveItsFileAwayDeletesIt` in `internal/app` |
| 5 | none. The field's documentation states it |
| 6 | `TestAMoveCrossesTheWireAndIsHeldToTheBounds` in `sdk/storageapi` |
| 7 | `TestAModuleMovesAFileOfItsPrintFolderIntoAStorage` in `internal/app` |
| 7, 8 | `TestAMoveThatFailsLeavesTheSource` in `internal/app` |
| 9 | `TestAMoveTakesOnlyAFileOfThePrintFolder` in `internal/app` |
| 10 | `Capabilities` in `internal/app/resourcehost/requires.go` lists the method |

## rejected

| option | why not |
|---|---|
| the module watches `device.transfer.settled` and deletes the file itself | see [the module watches the events](#the-module-watches-the-events) |
| the core deletes the file after every print from `module:data` | a module that prints one file to many printers would lose it after the first |
| the core deletes the file after a failed send too | a module would have to take the upload again to print again |
| a rename of the file into a storage on the same disk | see [a rename](#a-rename) |
| the host writes a staged body into the core's folder of temporary copies | see [the core keeps the staged body](#the-core-keeps-the-staged-body) |

### the module watches the events

Every module repeats the same work. A module that restarts during a transfer
misses the event and keeps the file for ever.

### a rename

A rename writes no byte again, but the storage plane publishes a file through
`Writer`, which computes the digest and writes the sidecar as it goes. A
rename needs a new optional interface on the storage provider, and a copy when
the storage is on another disk. The host copies for now, so a large file is on
the disk twice while it is moved. The bytes still never reach the module.

### the core keeps the staged body

A staged route would then serve only printing, and ADR-20261120-xys keeps it
general. A module that reads the bytes, such as one that unpacks a form, needs
them in its own folder.
