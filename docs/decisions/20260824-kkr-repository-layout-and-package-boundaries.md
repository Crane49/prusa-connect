---
title: "ADR-20260824-kkr: package layout"
description: "The package groups under internal/, the import edges allowed between them, and the gate that enforces them."
date: 2026-08-24
status: accepted
tags:
  - adr
  - architecture
  - modules
---
# ADR-20260824-kkr: package layout

- status: accepted
- date: 2026-08-24

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md)

## groups

Every package under `internal/` belongs to exactly one group.

| group | holds | may import |
|---|---|---|
| `kernel/` | ids, enums, errors, clock, logging, version | nothing internal |
| `arch/` | the dependency law as data, and the gates that enforce it | nothing internal |
| `<domain>/` | business rules and state, one directory per bounded context | `kernel`, `sdk` |
| `connector/` | printer-facing adapters, one directory per vendor dialect | `kernel`, `sdk`, domain |
| `transport/` | inbound adapters: HTTP, SSE, MQTT | `kernel`, `sdk`, domain |
| `platform/` | outbound adapters: persistence, cache, storage, files, auth backends, observability | `kernel`, `sdk`, domain |
| `extension/` | the extension host of [ADR-20260824-nyw](20260824-nyw-extension-mechanisms-and-the-hot-path.md): registry, supervisor, supervised process plumbing | `kernel`, `sdk` |
| `app/` | composition root: config, wiring, lifecycle | everything |

```
cmd -> app -> {transport, connector, platform} -> <domain> -> kernel
           -> extension -------------------------------------> kernel
                          \-> sdk <-/
```

## rules

1. `kernel` imports nothing internal.
2. `transport` and `platform` never import each other. They meet in `app`.
3. Only `app` imports everything.
4. Name a domain for what it is: `internal/printer`, not `internal/domain/printer`. A second bounded context is a sibling directory, not a child.
5. `cmd/<name>/main.go` parses flags, installs signal handling and calls `app.Run`. Nothing else lives there.
6. Configuration types, defaults and validation belong to `internal/app/config`.
7. No package-level mutable state outside `app`. Construct in `app` and inject. A pointer a hot path swaps is an `atomic.Pointer[T]`, never a mutex-guarded global.
8. A package name says what the package holds. `util`, `common`, `helpers`, `types`, `model` and `tools` are rejected in review.
9. A directory's name matches the package it declares.
10. A new top-level directory, or a change to this table, needs an ADR.
11. A domain does not import a sibling domain. Two contexts that need each other meet in `app`; a concept both own belongs in `kernel`.
12. An import that leaves `internal/` and returns is the edge it ends at. Routing through `adapters/`, `examples/`, `tools/` or `test/` does not make it legal.

## enforcement

The group table above is data in `internal/arch/layout_test.go`. A new cross-group edge fails `make test-unit` until the table names it, so widening the graph is a reviewable one-line diff next to this ADR.

| gate | reads |
|---|---|
| `TestInternalImportsFollowTheDependencyLaw` | the group table, against the union of the import graphs of every tag set and platform the build recipes use |
| `TestKernelImportsNothingInternal` | rule 1 alone, by parsing the files under `internal/kernel` rather than by asking the toolchain, so it fails independently of the table and of any build constraint |
| `TestTheGroupTableMatchesTheADR` | the table against the rows above, both ways |

The graph is read once per configuration and unioned. A single untagged `go list` would check the one configuration this project never ships. A file behind `//go:build sqlite`, or named `_windows.go`, compiles into a binary while staying invisible to a gate that asks for neither. Test imports count, including the `XTestImports` of a `package foo_test` file.

`frozenLawViolations` in that file is the list of edges that broke the table when the gate was tightened to match this ADR. It is empty: the tree obeyed the table already, and what the earlier gate could not see is why nobody could say so. Shrinking the list is roadmap; adding to it needs a reason in the pull request.

## rejected

| option | why not |
|---|---|
| flat `internal/` | fine to ~20 packages; past that a name no longer says whether a package is core, an adapter or wiring |
| `domain/`, `usecase/`, `infrastructure/` directories | a segment carrying no information; forces `domain.PrinterState` where `printer.State` is available |
| wiring in `package cmd` | cannot be imported under its conventional name, cannot be tested without building a binary |
| enforcement by review alone | structure without a gate decays at the rate contributors join |

## open items

- `internal/integration` has no group. It is a test harness; where it belongs is a question for the test layout.
- Files over ~1000 lines that this layout does not fix: `transport/httpapi/handlers.go`, `printer/state/printer_state.go`, `app/app.go`, `app/config.go`, `connector/xbuddy/connection.go`.
