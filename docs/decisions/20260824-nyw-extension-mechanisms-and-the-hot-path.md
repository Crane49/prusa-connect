---
title: "ADR-20260824-nyw: extensions and the hot path"
description: "Three extension tiers, and the rule that nothing third-party sits between a printer frame and the state update."
date: 2026-08-24
status: accepted
tags:
  - adr
  - extensions
  - performance
---
# ADR-20260824-nyw: extensions and the hot path

- status: accepted
- date: 2026-08-24

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md)

## tiers

| tier | mechanism | cost | for |
|---|---|---|---|
| 1 | compile-time: blank import, `init()`, `Register` | none; direct call | connectors, storage, identity providers |
| 2 | out-of-process over gRPC on a local socket | tens to hundreds of microseconds per call, crash-isolated | event sinks, added routes, non-Go implementations |
| 3 | WebAssembly, sandboxed | a memory copy at the boundary | user-authored rules and transforms |

## rules

1. Tier 1 is the default.
2. Nothing on the printer frame path is extensible at runtime. Decode, state update and fan-out call concrete types. Tiers 2 and 3 attach after the state update, never between the frame and the state.
3. The host wraps every third-party implementation before calling it: bounded queue, explicit drop policy with a counter for what was dropped, per-call timeout, and `recover()` at the goroutine boundary.
4. A contract never requires of an implementer what the host cannot verify. Because of rule 3, an extension may block.
5. Interfaces at the edges, concrete types inside a domain. An interface with one implementation that is not a port is removed in review.
6. Fan-out serialises an event once and gives every subscriber the same payload. Per-subscriber marshalling of identical data is a defect, not a tuning opportunity.
7. A tier 3 runtime must be pure Go with no cgo, so a sandbox does not cost cross-compilation to a Raspberry Pi.
8. Benchmarks live beside the code they measure. `make bench` runs them; CI compares against the base commit and fails a regression beyond the recorded threshold.
9. Every latency or throughput number in the documentation cites a benchmark in the repository. No benchmark, no claim.

## rejected

| option | why not |
|---|---|
| Go's `plugin` package | Linux and macOS only; needs a byte-identical toolchain and identical dependency versions. Cannot ship one cross-compiled static binary |
| tier 2 as the default | a serialisation boundary on a path that runs per printer frame |
| isolation as a documented requirement on the implementer | unenforceable, and the implementer is the side that cannot verify it |

## open items

- `eventsapi.Processor` still documents "MUST NOT block (buffer internally) and MUST NOT panic" with nothing enforcing it, on the path that updates printer state. Build the supervisor from rule 3, then relax the contract under rule 4.
- `internal/printer/events` holds the active processor in a package global read under `sync.RWMutex` on every emit. Rule 7 of ADR-20260824-kkr applies.
- The repository has no `Benchmark` functions. Rules 8 and 9 are unmet, and the README's latency claim is unverified.
