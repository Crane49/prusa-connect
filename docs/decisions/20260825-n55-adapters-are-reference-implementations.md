---
title: "ADR-20260825-n55: adapters are reference implementations"
description: "Built-in implementations live in adapters/, outside internal/, and compile against the published contract alone."
date: 2026-08-25
status: accepted
tags:
  - adr
  - architecture
  - extensions
---
# ADR-20260825-n55: adapters are reference implementations

- status: accepted
- date: 2026-08-25

<!-- nav:related -->
> [!NOTE]
> Contracts: [ADR-20260824-gfr](20260824-gfr-the-extension-sdk-is-a-separate-module.md) · Tiers: [ADR-20260824-nyw](20260824-nyw-extension-mechanisms-and-the-hot-path.md) · Groups: [ADR-20260824-kkr](20260824-kkr-repository-layout-and-package-boundaries.md)

## rules

1. A built-in implementation of a published contract is an **adapter** and lives
   in `adapters/<kind>/<name>/`. `<kind>` is the contract it implements:
   `storage`, `auth`, `db`, `events`, `connector`, `camera`.
2. An adapter imports `sdk/` and the standard library. **It never imports
   `internal/`.** If an adapter needs something from `internal/`, the contract
   in `sdk/` is incomplete and that is the bug to fix.
3. `adapters/` is part of the application module. It is not a module of its own
   and not one module per adapter.
   - **Narrowed by [ADR-20260901-7t4](20260901-7t4-build-time-adapter-composition.md) rule 4.**
     This rule binds the reference adapters in this tree. A community adapter
     is its own Go module and arrives through the build configuration, never
     through this tree.
4. An adapter passes the published conformance suite for its contract. Ours and
   a community author's pass the same suite or neither ships.
5. `internal/app` wires adapters. Nothing else in `internal/` imports one, so
   removing an adapter is a change to the composition root and to nothing else.
   - **Amended by [ADR-20260901-7t4](20260901-7t4-build-time-adapter-composition.md) rules 3
     and 8.** `internal/app` wires the shipped reference set. A composed
     adapter is wired by the generated composition module. Each binary still
     has exactly one composition point, and no other `internal/` package
     imports an adapter.
6. An adapter is written to be read. It is the first thing a community author
   opens, so it is complete, commented at the contract boundary, and free of
   shortcuts taken because it is ours.
7. A new adapter is a new directory, never a branch in an existing one. Two
   backends behind one adapter with a mode flag is two adapters.

## why not internal/

Earlier research puts implementations in `internal/` and publishes only the
interface, and §4.3 ends with "default to no". That reasoning holds for a
consumer who imports. It does not hold for the population this project actually
has.

Earlier research measured the ecosystem we are entering: **404 plugins from 307
authors, about 1.3 each.** That is a crowd of one-time contributors, not a
plugin industry, and starting has to cost them almost nothing. Such an author
does not import a reference implementation and wrap it. They open the nearest
one, copy it, and edit. Telegraf's `plugins/inputs/<name>/`, Caddy's `modules/`
and Traefik's `pkg/middlewares/` are all shaped for that, and earlier research already
names Telegraf's in-tree registration as our tier 1 model.

An implementation hidden in `internal/` is readable on the web and unimportable:
the toolchain refuses an `internal/` path to every module but this one. That is
the whole cost of publishing it with none of the benefit.

## why not a module

Earlier research rejects splitting implementations into modules directly: it
"creates a module that only one consumer imports, i.e. all of the cost and none
of the benefit". §4.3 heuristic 3 is the only one that argues for it, and it
argues for promoting **one** adapter whose dependencies are genuinely unwanted,
not for splitting them all pre-emptively.

The exception is the trigger §4.2 names: when somebody ships an out-of-tree
adapter as a Go library, promote that one.

## enforcement

Rule 2 is the rule the others rest on: an adapter that compiles against `sdk/`
alone proves the contract is enough, and an adapter that imports anything else
has found a gap in the contract. `TestAdaptersCompileAgainstTheContractAlone`
makes that import a build failure instead of a quiet one.

| gate in `internal/arch/layout_test.go` | fails on |
|---|---|
| `TestAdaptersCompileAgainstTheContractAlone` | an adapter that compiles against anything but `sdk/`, `adapters/` and the standard library, and an adapter test file that reaches any other part of this repository |
| `TestOnlyTheCompositionRootWiresAdapters` | rule 5: an `internal/` package other than `internal/app` importing an adapter |

Banning only `internal/` was not enough. It left `adapters/` free to import
`legacy/`, and nothing stopped an `internal/` package importing the adapter, so
the pair was a working way around ADR-20260824-dym rule 2 in plain production code.

A test file may use a third-party assertion helper. `adapters/` is part of the
application module, so that costs a copier nothing the module does not already
carry, and it says nothing about whether the contract is enough.

The conformance suite for each contract lives at
`sdk/<kind>api/<kind>apitest/` and is run against every adapter.

## rejected

| option | why not |
|---|---|
| implementations stay in `internal/platform/` | a community author cannot import them, so "reference implementation" means "read it on GitHub". Earlier research heuristic 1 is answered differently once the audience is a one-shot copier rather than a consumer |
| one `adapters/` module | earlier research, directly: all of the cost, none of the benefit. Three modules to keep in step, for an import nobody has asked for yet |
| one module per adapter | the OpenTelemetry-contrib shape. It is correct at fifty components with different lifetimes and wrong at six |
| adapters may import `internal/kernel` | it is the thinnest possible exception and it ends the property. An adapter that may reach one internal package is an adapter a community author cannot copy, and `kernel` is the package an author would most want to import |
| a `pkg/` directory | it attracts dependencies until everything imports it. Earlier research names it the classic mistake |

## consequences

- the contract in `sdk/` is exercised by real code before a community author
  ever sees it, so a gap appears as our own adapter failing to compile rather
  than as an issue report
- `adapters/` is the answer to "where do I start", which is the question a
  one-time author actually asks
- an adapter cannot take a shortcut through an internal package, so some
  contracts will need to grow. That growth is the point, not a cost
- the application module's dependency graph is public. A consumer importing one
  adapter takes the graph of all of them, which is the price of not splitting
  into modules and the reason that exception exists
