---
title: "ADR-20260901-7t4: build-time adapter composition"
description: "How a build names and links an adapter without editing core source: a build configuration generates a composition module that requires the core and the adapter modules, one public façade package carries the entry point, presence in the binary is admission, composed adapters take no build tags, and nothing moves out of internal/."
date: 2026-09-01
status: accepted
amends: [ADR-20260825-n55]
tags:
  - adr
  - extensibility
  - adapters
  - modules
  - build
---
# ADR-20260901-7t4: build-time adapter composition

- status: proposed. Every gate is owed
- date: 2026-09-01

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Amends [ADR-20260825-n55](20260825-n55-adapters-are-reference-implementations.md) rules 3 and 5 | Settles [ADR-20260901-rf2](20260901-rf2-pluggable-database-adapters.md)'s allowlist open item and discharges the composition record [ADR-20260901-b2z](20260901-b2z-driver-declared-permissions.md)'s open items owe | Upholds [ADR-20260824-gfr](20260824-gfr-the-extension-sdk-is-a-separate-module.md), [ADR-20260825-n55](20260825-n55-adapters-are-reference-implementations.md) rules 2 and 4, [ADR-20260825-rre](20260825-rre-one-registration-rule-and-one-descriptor.md), [ADR-20260826-gtv](20260826-gtv-build-tooling.md) rule 1, and ADR-20260901-b2z rule 12 | Reads with ADR-20260901-rf2 rule 5 and ADR-20260901-b2z rules 1, 12 and 24

An adapter joins a binary because a build configuration names its Go module.
A small tool reads that configuration and generates a disposable composition
module. That module is a `go.mod` requiring the core and the adapters, and a
`main.go` of blank imports calling one public entry function. Then it runs
`go build`. The
result is one static binary: the core exactly as published, plus the adapters'
`init()` registrations. Nothing under `internal/` is edited, promoted or moved;
the adapter compiles against `sdk/` alone, as ADR-20260825-n55 rule 2 has always
required. This is the xcaddy shape, chosen because the owner's sentence, pull,
build, hook in like other surfaces, is that shape described.

## the rules this record amends

Each names the rule, why it blocks the requirement, what it protected, and what
protects that now. Sub-bullets are placed on the old records at merge.

**ADR-20260825-n55 rule 3.** *"`adapters/` is part of the application module. It is not
a module of its own and not one module per adapter."* Taken as a rule about
every adapter, it forbids the directive outright. A community adapter must be a
module of its own, because Go's `internal/` rule and the main module boundary
are the only walls there are. It was protecting the reference implementations
from module sprawl and version skew against the core they ship with. That
protection survives narrowed: it binds the `adapters/` tree, the shipped
reference set, and says nothing about adapters that arrive by composition.
Sub-bullet:

> - **Narrowed by [ADR-20260901-7t4](20260901-7t4-build-time-adapter-composition.md) rule 4.**
>   This rule binds the reference adapters in this tree. A community adapter is
>   its own Go module and arrives through the build configuration, never
>   through this tree.

**ADR-20260825-n55 rule 5.** *"`internal/app` wires adapters. Nothing else in
`internal/` imports one, so removing an adapter is a change to the composition
root and to nothing else."* Its second half is the point and stands. Its first
half, taken as "only `internal/app` may wire an adapter", is the nine-files
problem in one sentence. It was protecting a single composition point; there is
still exactly one per binary, the composition module's `main.go` for a
composed build, `internal/app/builtin` for the shipped one. Sub-bullet:

> - **Amended by [ADR-20260901-7t4](20260901-7t4-build-time-adapter-composition.md) rules 3
>   and 8.** `internal/app` wires the shipped reference set. A composed
>   adapter is wired by the generated composition module. Each binary still
>   has exactly one composition point, and no other `internal/` package
>   imports an adapter.

**ADR-20260901-rf2, open item "the allowlist".** That record left to this one whether a
build-composed adapter is admitted without a line in
`internal/app/builtin/adapters.go`. Settled by rules 8 and 9: it is, by
descriptor-preserving adoption, and the lists stay as the shipped build's
inventory.

## rules

### the requirement

1. An adapter is added by writing a build configuration, never by editing core
   source. "Core source" is everything in this repository: `internal/`, `cmd/`,
   `sdk/`, the Makefile, CI, lint configuration and the gates. The measure is
   mechanical: composing an adapter into a binary leaves `git status` clean in
   a core checkout.
2. Every adapter is compiled in. There is no live loading, no runtime
   admission, no plugin file, for any kind. Out-of-process execution stays
   deferred whole (ADR-20260901-rf2 rule 5, ADR-20260901-b2z rule 14). "No core patching"
   means rule 1, not dynamic loading.

### the composition module

3. A composed binary is built from a generated **composition module**. It has
   a `go.mod` requiring the core module and each adapter module at a pinned
   version. Its `main.go` contains one blank import per adapter and one
   call into the composition façade (rule 6). The module is generated into a
   scratch directory by the build tool (rule 5), is never committed to this
   repository, and is disposable. The build configuration and the binary are
   the artefacts, and the generated `go.sum` is the record of what was pulled
   **by version**. A path-replaced module (rule 15) is
   taken from a directory and leaves no `go.sum` entry at all.
4. An adapter is a Go module. It imports `sdk/` and the standard library and
   never `internal/`, ADR-20260825-n55 rule 2 unchanged, now enforced by the compiler
   itself, since Go refuses `internal/` imports across the module boundary. It
   registers from `init()` through the published contract registries
   (`sdk/connectorapi`, `sdk/eventsapi`, `sdk/authapi`, `sdk/cameraapi`,
   `sdk/storageapi`, and `sdk/historyapi` and `sdk/stateapi` once ADR-20260901-rf2
   rule 3 lands), under ADR-20260825-rre's one registration rule. It passes the
   published conformance suite for its contract (ADR-20260825-n55 rule 4) in its own
   CI, before any build pulls it.
5. The build tool is a command in this repository (working name
   `cmd/compose-build`), run through one Makefile target (ADR-20260826-gtv rule 1).
   It reads one configuration file. That file names the core (by version, or
   by path to a checkout) and the adapter modules (each by version, or by
   path). A path becomes a `replace` in the generated module. The file also
   names the server configuration file and the variant. It generates the
   composition module and **copies every `replace` directive from the
   resolved core's `go.mod` into it**, the way xcaddy carries the main
   module's replaces. It re-points the relative `./sdk` target at the
   resolved core's location. Without the copy the generated module does not
   resolve in either mode. A `replace` binds only in the main module, so the
   core's own replaces (the sdk's, and rule 15's fractal one) are ignored the
   moment the core is a dependency. Then `go mod tidy` fails. It then
   resolves modules with `go mod tidy`, runs `detect-build-tags` and
   `detect-build-feature` from the resolved core exactly as `make build`
   does, and builds. With zero adapters it must produce a binary the shipped
   test suite cannot tell apart from `make build`'s. The gate is exactly
   that, the zero-adapter compose
   passing the shipped suite in CI, and it is claimed as what it is rather
   than as behavioural identity.
6. The core exposes one composition façade: a public package in the core
   module (working name `compose/`) whose exported surface is one entry
   function carrying what `cmd/prusa-connect/main.go` does today. The
   shipped main becomes its first caller. It reads its version from
   `debug.ReadBuildInfo`, not from a main-package ldflags stamp, so a composed
   binary reports itself correctly, in version mode. A path-mode build
   reports `(devel)` and a directory, which is Go's honest answer for an
   unpinned source (rule 15). This façade is the only new public API this
   record mints, and nothing under `internal/` moves out: an adapter needs the
   sdk, and the composition module needs one function.

### identity and admission

7. The built-ins keep the `core` namespace in every build. `sdk/extension`
   derives `CoreNamespace` from the core module's path, a constant it already
   holds, never from the build's main module. Without this, a composed build
   silently renames `core/amqp` and every other shipped qualified name, which
   is the one way this design corrupts existing configuration.
8. A registration from a module other than the core is adopted onto the
   resource plane under its **own** descriptor, the namespace
   `sdk/extension` derived from its module path. The adoption path never
   re-mints a name. The existing path, which re-declares and refuses a
   mismatch (`ErrForeignNamespace`), stays for the `core/*` inventory. The
   forgery it prevents is still prevented, because the descriptor a foreign
   registration carries was derived by the host from the build graph. The
   author cannot choose it (ADR-20260825-rre rule 4).
9. Presence in the binary is admission. A composed adapter needs no entry in
   `adaptedSinks` or its siblings. Those lists are the shipped reference
   build's inventory and nothing more, which is what they always were
   (earlier research, the lists gate only names the artefact already carries).
   The trust model is ADR-20260901-b2z rule 12, unchanged. The build is the trust
   act, `go.sum` pins what was built, the module path names who wrote it,
   and that sentence is honest only in version mode. A path-replaced module
   (rule 15) has no sum, no version and no hash anywhere. In path mode the
   trust act is the builder vouching for a checkout, recorded as a path. The
   pinning and attribution guarantees are version-mode properties, and
   version mode is the release path (rule 15).
10. What a composed binary carries is readable, not remembered. The build
    graph the linker embeds (`go version -m`, `debug.ReadBuildInfo`) names
    every version-resolved module with its path, version and hash. A
    path-replaced module renders as `=> /local/path (devel)`, a path, no
    version, no hash. So the inventory states which of the two it holds per
    module rather than implying hashes it does not have. The served
    vocabulary and extension inventories (ADR-20260901-b2z rule 24;
    `make generate-extensions`'s runtime equivalent) are computed from the
    registries the binary actually holds. The committed `docs/extensions/*`
    artefacts document the shipped reference build only, and say so.

### build tags

11. A composed adapter takes no build tag, ever. A tag can only gate code
    already in the module graph; the build configuration already decides
    presence, so a tag would be a second switch for one fact. Build tags
    remain what they are for the core: trimming optional core features. The
    airlock (`adapters/tags.json`) therefore requires an entry only for a type
    that carries a tag; an untagged in-tree adapter adds no row, and a
    composed adapter never appears in it.
12. Consequently the Makefile's `FULL_TAGS`, the CI tag lists, the release
    image's tag list and `.golangci.yml`'s `build-tags` change only when the
    core grows or loses a feature. That is a core edit for a core change, and
    never per adapter.

### the shipped tree

13. The reference adapters stay in `adapters/`, inside the core module, wired
    by `internal/app/builtin`, ADR-20260825-n55 as amended above. Adding one is a
    product decision about what the reference artefact carries, and it may
    touch the shipped inventory files. That is not the friction this record
    removes, and it is bounded. It is one blank import, one adoption entry
    where the kind has a list, and one airlock row where there is a tag.
14. The second import list dies as a per-adapter touch.
    `cmd/generate-extensions` imports the composition root's registration
    surface (`internal/app/builtin`, which already blank-imports every
    `adapters/` package) instead of each adapter, so the generator and the
    binary cannot disagree about the shipped set. The two drivers still under
    `internal/connector/` keep their explicit imports until they move.
15. Fetching is Go's. Version-mode composition needs three prerequisites.
    The first two land with this record's first cut rather than waiting,
    nothing has shipped, so each is a rename or a tag
    with nothing to break:
    - the core module path becomes fetchable: `go.mod` declares
      `github.com/prusa3d/prusa-connect` and the remote is
      `prusa3d/prusa-connect`; the path is fixed before this
      record's tool is built;
    - the sdk takes nested-module tags (`sdk/vX.Y.Z`), retiring the core's
      `require …/sdk v0.0.0` + local `replace`, which no downstream module
      inherits because a `replace` binds only in the main module.

    The third is not dissolved by decision 11. The core requires
    the Fractal module from a private Git host through a `replace` onto
    its `.git` path, because that host's go-get discovery truncates
    three-segment subgroup paths. The un-replaced path
    cannot be fetched at all, in either mode. Until that dependency moves to
    a fetchable path or is vendored, rule 5's replace-copying is what makes
    the generated module resolve. Where fetching it needs credentials,
    composing a build needs exactly the credentials `make build` needs.

    **Version mode is therefore the first supported cut and the release
    path**. Path mode, a generated `replace` at a checkout, for the core,
    its sdk and its copied replaces, remains for development and on-premise
    builds. It is the mode where a replaced module has no `go.sum` entry,
    no version and no hash. Rules 3, 9 and 10 say so where each guarantee is
    claimed.

## enforcement

Every gate is owed; each is watched failing on a deliberate mutation before it
is trusted green, per the house rule.

| rule | gate | state |
|---|---|---|
| 1, 3, 8 | ADR-20260901-b2z rule 1's joint gate: a `test/compose/` fixture composes, dispatches and grants; the core checkout stays clean | owed |
| 2 | no call to `plugin.Open` and no dlopen-shaped dependency in the tree; the static-binary check (`binary-check`) unchanged | owed (the check exists; the assertion is added) |
| 5 | zero-adapter compose reproduces `make build`: same test suite passes against both binaries in CI | owed |
| 5, 15 | the generated module carries every core `replace`, `./sdk` re-pointed, and tidies; watched failing without the fractal copy | owed |
| 6 | an arch case that `compose/` exports one entry function and nothing else, and that `cmd/prusa-connect` calls it | owed |
| 7 | a registrant built under a foreign main module: core packages still render `core`, a foreign module its derived namespace | owed |
| 8 | a foreign registration is adopted under its own qualified name; one claiming a `core/*` name is still refused | owed |
| 11 | airlock narrowed: an entry required where `tag != ""`, refused where nothing registers the type; a composed type absent | owed |
| 14 | `cmd/generate-extensions` imports no `adapters/` package directly, asserted by the import-graph gates | owed |

## rejected

| option | why not |
|---|---|
| live or dynamic loading (`plugin`, IPC at runtime) | `plugin` forbids static linking, pins the toolchain, and cannot be unloaded |
| a generated imports file and `go.mod` churn inside the core checkout | workable, but it mutates "the core as it was": a permanently dirty tree and an import graph the arch gates never saw |
| build tags over a fixed in-tree set, tidied | a tag cannot conjure code into the module graph, so it never admits an unseen adapter; and it multiplies tag lists |
| promote `internal/extension/registry` (or more of `internal/`) as public API | adapters compile against `sdk/` alone and composition needs one function; a permanent public surface is the worse trade |
| keep the allowlists as the admission mechanism, extended per composed adapter | the lists gate names the artefact carries; as admission they would be the core edit rule 1 forbids |
| an adapter registry service or marketplace doing the pulling | `go mod` already fetches, pins and records; a second fetcher would re-answer a solved question with new failure modes |
| vendoring as the primary mode | the air-gapped form of the same mechanism, free via `go mod vendor` in the composition module; primary would buy nothing |

## open items

- **The rename question, smaller than it was.** Moving a shipped
  adapter out of the tree changes its qualified name. `core/moonraker`
  becomes a namespace derived from its new module path. Nothing has
  shipped, so the configurations that name would break are deployed nowhere,
  and the choice is purely product, not migration. If moonraker moves out, it
  moves out directly, no copy-first step, no deprecation machinery, and
  the move doubles as the composition proof. Owed at release, not now: once
  configurations exist in the field, a qualified-name change takes ADR-20260825-zkq's
  deprecation cycle. xBuddy stays in-tree until its sdk contract port merges.
  On this branch it imports four `internal/` packages and cannot leave.
- **Module path and sdk tagging: scheduled into the first cut** (rule 15),
  a `sed` and a `git tag` while nothing has shipped. What stays open is the
  fractal dependency's unfetchable path (rule 15's third prerequisite): fix
  the path, vendor it, or carry the copied replace indefinitely.
- The composition configuration's schema (rule 5) is minted with the tool and
  published as an example, not gated until it stabilises.
- Whether the shipped release images are themselves produced by the compose
  tool with zero adapters is open (one build path instead of two). It is
  decided when rule 5's equivalence gate has run green for a while.
- The conformance suites a composed adapter must pass (rule 4) run in the
  author's CI, not ours. Whether the compose tool optionally runs
  `go test` over pulled adapter modules before linking is left to the tool's
  first users.
