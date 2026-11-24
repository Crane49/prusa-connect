---
title: "ADR-20261124-t6f: a module lands a file on a printer without printing it"
description: "device.send copies a file from a storage onto a printer and sends no start command. It is a method of its own, so a core that does not serve it refuses the call instead of printing."
date: 2026-11-24
status: proposed
supersedes: []
amends: [ADR-20261023-sns, ADR-20261123-vvb]
tags:
  - adr
  - modules
---
# ADR-20261124-t6f: a module lands a file on a printer without printing it

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Builds on [ADR-20261123-vvb](20261123-vvb-the-core-deletes-a-file-a-module-hands-it.md), [ADR-20261023-sns](20261023-sns-a-module-lists-a-storage-and-owns-one-the-core-prints-from.md)

A module could put a file on a printer only with `device.print`, which also
starts it. PrusaLink's Upload, which the PrusaSlicer 2 Connector answers, puts
a file on the printer and starts nothing. The core already lands a file without
a print for its own pages. This record lets a module ask for the same.

## rules

1. `device.send` takes the fields of `device.print`, and `storage` is
   required. It lands the file at `path` and sends no start command.
2. It takes what a print from a storage takes: `operate` on the printer and
   `read_storage`, held to the module's ceiling, and `module:data` only for a
   module granted a data directory.
3. It answers when the transfer has started, with `transfer_id` and `path`. A
   printer that already holds the same file answers with `path` alone.
4. `keep_both` and `delete_source` mean what they mean for `device.print`
   (ADR-20261123-vvb rules 1 to 5).
5. A module names `core-device.send: "*"` under `require`.

These rules amend ADR-20261023-sns rule 9: the core reads `print/` for
`device.send` from that module too, and for nothing else. They amend
ADR-20261123-vvb rules 1 and 2, which now hold for `device.send` as for
`device.print`.

## enforcement

| rule | gate |
|---|---|
| 1 | `TestASendNamingNoStorageIsRefused` in `sdk/deviceapi`, and `TestAModuleSendLandsTheFileAndStartsNothing` in `internal/app` |
| 1 | `TestASendNamingNoStorageNeverStartsAFile` in `internal/app` |
| 2 | `TestAModuleSendTakesReadStorage` and `TestAModulesOwnSendIsHeldToItsCeiling` in `internal/app` |
| 2 | `TestAModuleSendForAPersonIsDecidedForThatPerson` in `internal/app` |
| 2 | `TestAModuleSendFromAPrintFolderItWasNotGrantedIsRefused` in `internal/app` |
| 3 | `TestTheHostReadsASendAsItWasWritten` in `sdk/deviceapi` |
| 4 | `TestAModuleSendLandsTheFileAndStartsNothing` in `internal/app` |
| 5 | `Capabilities` in `internal/app/resourcehost/requires.go` lists the method |

## rejected

| option | why not |
|---|---|
| a field on `device.print` that skips the start command | a core that does not know the field ignores it and prints the file |
| the module writes the file into a storage instead | the person who picked a printer in the slicer finds the file on the server, not on the printer |
