---
title: Writing an extension
description: The three tiers an extension can run at, what crosses the process boundary, how one is installed and verified, and what the host enforces.
tags:
  - overview
  - extensibility
  - plugins
audience: extend
rewrite:
  describes: current
  implementedBy:
    - internal/extension/registry
    - internal/extension/script
    - internal/app/config/install
    - sdk/extension
---

Read this before deciding to write an extension. It says which tier your code
runs at, what the host does to it, and what is not built yet.

[Adapters, modules and runtimes](./extension-concepts.md) is the next page: it
picks the kind for you. [Writing a printer driver](./writing-a-driver.md) and
[Writing a service module](./writing-a-module.md) are the two worked guides.

Every number below is measured and names its source.

## The three tiers

```mermaid
flowchart TB
  subgraph t1["tier 1, compiled in"]
    direction TB
    A1["a Go package, built into the binary"]
    A2["fastest, and the largest blast radius"]
    A3["ours, and anything an operator<br/>deliberately compiles in"]
  end

  subgraph t2["tier 2, a separate process"]
    direction TB
    B1["a binary the host launches<br/>and supervises"]
    B2["killable, and the only tier that<br/>satisfies a design requirement"]
    B3["where a community printer driver lives"]
  end

  subgraph t3["tier 3, a script"]
    direction TB
    C1["one file, no build step"]
    C2["glue: reshape an event,<br/>publish it somewhere"]
    C3["expr, hosted by<br/>internal/extension/script"]
  end

  t1 --> t2 --> t3
```

A community printer driver belongs at tier 2. A design requirement says a third party
cannot block, crash or slow the frame path, and compiled-in code cannot deliver
that: nothing preempts it and it reads every secret in the address space. A
separate process delivers it by construction.

The cost was measured and it is small. A one-way stream message crosses the
boundary in 648 ns to 2.26 µs, against 66 to 71 µs for a unary round trip, and
`Session.State()` in `sdk/connectorapi` is already `<-chan StatePatch`. Four
hundred printers spend 0.021 % to 0.27 % of a core there.

Wasm was a fourth tier. It is declared with a named trigger, built on by
nothing, and left out of the author documentation. Its one unique in-process
property is an enforceable budget, and the script runtime gives that for a
third of the size.

## What crosses the boundary

```mermaid
sequenceDiagram
  participant PL as plugin process
  participant HO as host
  participant HB as event hub

  Note over PL,HO: one length-prefixed frame,<br/>carrying a stream id
  HO->>PL: control call, JSON-RPC 2.0
  PL-->>HO: result
  Note over HO,PL: 15 to 25 µs per round trip

  PL->>HO: telemetry, pushed on a long-lived stream
  Note over PL,HO: 1.34 µs unbatched,<br/>52 to 62 ns coalesced
  HO->>HB: publish, addressed by resource
  Note over HB: from here it is the core's own<br/>hot path, with no third-party code on it
```

Control frames are JSON-RPC 2.0, which a design requirement already set for every
socket boundary, so the product ships one RPC vocabulary for both. Bulk frames
are raw bytes: JSON-RPC at 64 KiB measured 1,023 to 1,069 µs against 50.1 µs
binary. Every byte of this is specified in [the plugin wire
protocol](./plugin-wire-protocol.md), so an implementation in Python, Rust or
TypeScript needs no Go.

Not gRPC. The three `full` targets carry 5.81 to 6.61 MiB of headroom, a bare
gRPC server plus client measures 7.31 MiB and a fuller stack 8.68 MiB. Both are
over on every target, before the SQL driver. Refusing it has one cost, stated
here openly: this project writes its own flow control, which gRPC ships.
LSP and MCP took the same trade, and neither generates bindings, because an
afternoon implements the whole wire.

## How an extension gets installed

```mermaid
flowchart LR
  CAT["catalogue<br/>one JSON index,<br/>one git repo, PR to add"]
  GH["a GitHub release"]
  OCI["an OCI artifact"]
  DIR["a local directory"]

  CAT -.describes.-> GH
  CAT -.describes.-> OCI
  GH --> DIR
  OCI --> DIR
  DIR --> VER{"checksum<br/>and signature"}
  VER -->|fails| REF["refused, and nothing executed"]
  VER -->|passes| NEG{"contract_major<br/>matches?"}
  NEG -->|yes| RUN["launched and supervised"]
  NEG -->|no| REF2["refused before launch"]
```

The local directory is the only real path and the others are fetchers in front
of it. That is what makes an air-gapped install a `cp` rather than a special
mode. Terraform needed a filesystem mirror precisely because its default is the
network.

The checksum is re-verified before every launch and not only at install. An
absent `contract_major` is a load error and never a default, which is
OctoPrint's stranding problem inverted.

## The two states an operator picks

```mermaid
stateDiagram-v2
  [*] --> disabled: installed
  disabled --> runtime: enable
  runtime --> disabled: disable
  runtime --> failed: crashes, loops,<br/>or exceeds what it declared
  failed --> runtime: retried on backoff
  failed --> disabled: operator gives up
```

There is no build state, and it is not an omission. A cold build of this binary
peaks at 1.42 to 1.48 GiB of RAM and needs 1.35 GiB of disk, so the 1 GB
Raspberry Pi that a design requirement names cannot link it. Shipping the matrix
instead is 194 tag sets and 776 binaries, roughly 29 GB per release, doubling
with each compiled-in plugin. A remote build service is the one path an air gap
cannot reach.

So build stays a `make` target for somebody who wants a minimal binary. It is a
property of the artefact an operator downloaded, which the UI reads and never
sets.

## What stops a bad plugin

```mermaid
mindmap
  root((blast radius))
    impossible across a process boundary
      reads our secrets
      corrupts our heap
      holds our locks
      panics our process
      never returns
    the host owns, at every tier
      admission
      timeout
      circuit breaker
      panic recovery
      bounded queue
    declared, then enforced
      concurrency
      call rate
    watched, then killed
      memory over its budget
    not enforced
      egress
```

Five of the eleven ways a plugin can hurt the host are unprotectable in tier 1
and impossible in tier 2. That asymmetry is the argument for the separate
process, and the latency is not.

The honest limit: killing a watched child is the only enforcement available on
Linux and on Windows, without root and without a delegated cgroup. The host
reads each module's memory every two seconds. A module over
its budget has every process the host finds killed, unless the operator chose to
only warn. That is a watched limit, not a kernel limit.

A process that leaves the group with `setsid` is no longer counted once
its parent has exited. Memory in shared memory or in a `memfd` is not counted
either. On Linux the last bound is the kernel's own out-of-memory killer,
which the host sets to pick a module before the server. Nothing limits which network addresses a module
reaches.

## Every registration carries a descriptor

An entry in the registry carries its identity, the namespace it came from, a
stability level, its documentation, the event types it emits with their
versions, the config keys it accepts with their types and defaults, and two
versions of its own. Two, because one is the implementation's and one is the
contract it compiles against, so a host refuses an incompatible plugin before
loading it.

```mermaid
flowchart LR
  REG["the registry<br/>every entry carries<br/>a standardized descriptor"]
  REG --> DOC["author reference"]
  REG --> CAT["catalogue page"]
  REG --> FORM["the settings form in the UI"]
  REG --> MTX["compatibility matrix"]
  REG --> GATE["a gate that refuses<br/>an undocumented entry"]
```

The config keys are what let the UI render a plugin's settings form without one
hand-written form per plugin, which is the thing that does not scale to a
hundred connectors. [Extensions](./extensions.md) is that reference, generated
from the registry, so it cannot drift from what the binary holds.

Three rules hold the registry, and each has a gate:

| rule | where it is enforced |
|---|---|
| a qualified name registered twice is refused, and the message names both modules | `ErrDuplicateType` in `internal/extension/registry` |
| every registration carries a descriptor | `TestOnlyLegacyRegistersWithoutADescriptor` in `internal/arch` |
| the published reference agrees with the registry | `make extensions-check` |

A registration without a descriptor reaches an operator's configuration and
shows in the published reference as a blank row: no summary, no version, no
config keys, no stability level. No contract package offers an entry point that
takes one. `Register` is the one path: it takes the descriptor, it panics on a
duplicate, and it never replaces one silently. The gate refuses a
descriptor-less entry point coming back, whether it is called directly or bound
to a name and called elsewhere.

## What is built and what is not

| | state |
|---|---|
| registries seal, refuse duplicates, carry descriptors | built. `internal/extension/registry`, gated by `make extensions-check` |
| the script runtime | built. `expr`, hosted by `internal/extension/script`. Only a sink may declare it |
| out-of-process connectors are viable | decided, on two independent measurements |
| the wire: our framing, JSON-RPC control | decided |
| three tiers, wasm declared and unbuilt | decided |
| build mode leaves the UI | decided |
| two states, disabled and runtime | decided |
| inbound tier 2 | open. Undesigned and unmeasured. Outbound ships first |
| a community driver we do not maintain, proved end to end | not built |
