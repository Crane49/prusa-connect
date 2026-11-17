---
title: "ADR-20261028-jae: 3MF projects are read by the threemf extension, and the core reads only their picture"
description: "The 3MF reader, the project routes and the project view live in the threemf extension. The core keeps a small reader of a project's picture as the floor, reads the objects_info labels of G-code itself, and imports nothing of 3MF."
date: 2026-10-28
status: accepted
supersedes: []
amends: [ADR-20261026-w6a]
tags:
  - adr
  - modules
  - sdk
---
# ADR-20261028-jae: 3MF projects are read by the threemf extension, and the core reads only their picture

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Amends: [ADR-20261026-w6a](20261026-w6a-a-3mf-project-is-read-by-a-core-reader-and-one-dialect-per.md) | Related: [ADR-20261027-4tq](20261027-4tq-a-3mf-project-in-a-storage-is-read-whole-and-answered-as.md), [ADR-20261027-2rq](20261027-2rq-a-3mf-selection-places-copies-of-build-items-on-one-bed.md), [ADR-20261027-wvj](20261027-wvj-a-module-extends-how-the-core-reads-a-file-and-draws-a-file.md), [ADR-20261027-e6k](20261027-e6k-a-module-depends-on-a-versioned-contract-and-not-on-another.md)

The core used two functions of `sdk/threemf`: the picture of a project in a
listing, and the labels of the objects in sliced G-code. Everything else, the
project page, the meshes and arranging, served modules and the page of one
file type. It would have added about 0.7 MiB to every core binary, the printer
build included. The extension points of ADR-20261027-wvj let a module read a
file type for the core and draw its page, so the 3MF work moves into one
first-party extension.

## rules

1. The 3MF reader is the Go package `github.com/prusa3d/threemf/threemf`
   in `extensions/threemf`. The rules of ADR-20261026-w6a and
   ADR-20261027-2rq bind that package, and the paths in them that name
   `sdk/threemf` name it. It imports the standard library only.
2. Another module that changes or reads a project links the package as a Go
   dependency of its own `go.mod` (ADR-20261027-e6k rule 7).
3. The project routes and the project page of ADR-20261027-4tq are the
   extension's, under its mount. The extension draws the page in the
   `file-view` slot for `.3mf`, and hands the same view to other modules as
   the component `project-view`.
4. The extension reads `.3mf` for the core under `file_analysis`.
5. The core keeps a reader of a project's picture alone, in
   `internal/printfile/project.go`. It is the floor when the extension is not
   installed, not serving, switched off, too slow or failing, and on the
   printer build.
6. The core reads the `objects_info` labels of G-code itself, in
   `internal/printfile`. They are not 3MF.
7. No package of the core imports a 3MF package, and `sdk/` has none.

## enforcement

| rule | gate |
|---|---|
| 1, 3, 4 | the extension's gate, `make -C extensions check EXT=threemf` |
| 5 | `TestAProjectClaimingManyEntriesIsRefusedBeforeItIsOpened` and the project fixtures of `internal/printfile` |
| 7 | `TestTheCoreImportsNo3MF` in `internal/arch` |
| 2, 6 | none. Review holds them |

## measurements

The core, `CGO_ENABLED=0 -trimpath -tags minimal -ldflags="-s -w"`, Go
1.26.10, on the branch with and without `sdk/threemf`:

| target | with | without |
|---|---|---|
| linux/arm64 minimal | 35,913,890 | 35,848,354 (-64 KiB) |
| linux/amd64 minimal | 39,510,178 | 39,461,026 (-48 KiB) |

On main the core used only the picture and the labels from the package, so
the move saves little there. The project routes of ADR-20261027-4tq would have
added about 0.7 MiB, and they never reach the core now.

## rejected

| option | why not |
|---|---|
| keep the reader in `sdk/` and only move the page | every module would build against one version of the reader through the sdk, and the core would carry its exported API |
| no 3MF reader in the core at all | the listing and the printer build would show no picture for a project when the extension is absent |
| the extension reads the file through `storage.read` | each piece is at most 512 KiB of base64 in JSON, and a project can be hundreds of megabytes |
| the core keeps the project routes and the extension only the page | the routes are most of the growth, and a module page would call the core for one file type |
