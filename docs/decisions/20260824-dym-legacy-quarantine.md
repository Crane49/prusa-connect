---
title: "ADR-20260824-dym: legacy quarantine"
description: "legacy/ holds the implementation that predates the proposal: it ships, nothing imports it from internal/, and it is not a model for anything."
date: 2026-08-24
status: accepted
tags:
  - adr
  - architecture
  - migration
---
# ADR-20260824-dym: legacy quarantine

- status: accepted
- date: 2026-08-24

<!-- nav:related -->
> [!NOTE]
> Package groups it defers to: [ADR-20260824-kkr](20260824-kkr-repository-layout-and-package-boundaries.md) · Index: [decisions](README.md)

## rules

1. `legacy/` holds the implementation that predates the architecture in
   an earlier design proposal. It compiles, it ships, and it is not a model for
   anything.
2. Nothing in `internal/` or `sdk/` imports `legacy/`. The dependency runs one
   way: `legacy/` may import `internal/kernel` and `sdk/`, never the reverse.
3. New code is never added to `legacy/`. A change there is a bug fix, a security
   fix, or a deletion.
4. Work moves out of `legacy/` by being rewritten against the current
   architecture, reviewed, and landing in `internal/`. The code is never moved
   across unchanged.
5. A package leaves `legacy/` only when nothing imports its legacy path.
6. `legacy/` is exempt from the file-length and complexity gates, and is frozen
   in the import graph. It is not exempt from the security gates.
7. A reader looking for how this codebase does something looks in `internal/`.
   `legacy/` answers what it used to do, which is a different question.

## enforcement

`internal/arch/layout_test.go` fails on a route from `internal/` or `sdk/` to
`legacy/`, in a production file or a test file alike.

- it asks whether `legacy/` is reachable, not whether it is imported directly.
  A direct-edge check was satisfied by one hop through `adapters/`, which is
  plain production code, so `internal/` reached `legacy/` with every gate green
- the graph is read once per tag set and platform the build recipes use, and
  unioned. A file behind `//go:build sqlite` compiles into the image; an
  untagged `go list` cannot see it
- `TestTestdataDoesNotHideALegacyImport` walks the tree itself, because the
  toolchain never enters a `testdata` directory. `legacyReadersUnderTestdata`
  is the reviewed allow-list, and a stale entry there fails too

`.golangci.yml` excludes `legacy/` from the length and complexity rules, which
would otherwise report several hundred pre-existing findings and train everyone
to ignore the output.

## rejected

| option | why not |
|---|---|
| delete it and rebuild from nothing | the binary stops working for months, and the behaviour it encodes is the specification for the replacement |
| leave it in `internal/` and rely on review | an agent or a contributor reads the nearest example and copies it. The nearest example is what teaches, not a rule in a file they did not open |
| a branch instead of a directory | the two must build together while the replacement is written, and a long-lived branch would diverge |
| mark it with comments | a comment in a file nobody opened does not stop the file being read as a pattern |

## consequences

- the build stays green throughout the rewrite, so there is a working binary at
  every commit
- the import graph makes progress measurable: `legacy/` shrinking is the metric
- a reader can tell current from historical by path alone, which no amount of
  documentation achieves
- two implementations of some concepts coexist for a while, and the legacy one
  is the one that runs until its replacement is reviewed
- moving a package is a large mechanical diff. It is done in one commit per
  move, with no behaviour change, so the diff is reviewable as a rename

## open items

- `legacy/` is presently most of the application. The status table in
  `docs/map.md` tracks what has left it
