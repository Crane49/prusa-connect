---
title: "ADR-20260824-gfr: the extension sdk"
description: "sdk/ is its own Go module, imports nothing outside the standard library, and is additive-only after v1."
date: 2026-08-24
status: accepted
tags:
  - adr
  - sdk
  - modules
---
# ADR-20260824-gfr: the extension sdk

- status: accepted
- date: 2026-08-24

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md)

## rules

1. Public contracts live under `sdk/`, which is its own Go module.
2. `sdk/` imports nothing outside the standard library.
3. A contract never exposes an application-internal type. What an implementer receives is an interface `sdk/` owns, listing the methods that implementer actually calls.
4. `sdk/` is tagged `sdk/vN.N.N`, independent of the application tag. A release tags both.
5. After `sdk/v1`, the exported surface is additive-only. CI runs an exported-API diff against the last tag; a breaking change fails the build.
6. Every port carries a conformance suite in a subdirectory (`sdk/authapi/authapitest`) and a working implementation under `examples/` that CI compiles.
7. The application resolves the sdk with `replace => ./sdk` until the sdk is tagged. No `go.work`: one resolution mechanism, identical for a developer, CI and the Docker build.
8. Every per-module tool runs once per module. `go test ./...`, `go mod download`, `go mod verify` and golangci-lint each see one module per invocation. A Makefile target or CI job that adds one and forgets the second silently stops covering `sdk/`.
9. `sdk/**` is in the CI path filter, so a contract-only change still runs the backend jobs.

## enforcement

`TestTheSdkImportsOnlyTheStandardLibrary` in `internal/arch/layout_test.go`
reads the sdk module's own package list and fails on rule 2.

- test imports count. Rule 2 is not qualified, and the cost it names, the module
  graph an implementer inherits, comes from a test dependency exactly as from a
  production one: both land in `sdk/go.mod`
- the standard library is the toolchain's own list. The heuristic this replaced
  read any import path whose first segment had no dot as standard library
- `sdkThirdPartyExceptions` is the allow-list. An entry is a reviewed exception
  with a reason, and a stale entry fails

## rejected

| option | why not |
|---|---|
| contracts at the application module root | an author implementing one inherits 114 modules to get `errors`, `io` and `strings`, and cannot be given a contract version independent of the application |
| contracts in a separate repository | a contract and its in-tree implementation could no longer change in one commit |
| a curated allowance of third-party types in `sdk/` | every such type is a version every extension must agree with forever |

## open items

- `routesapi` exposes `gin`, which pins every extension author to this application's web framework. Express it in `net/http` terms. Blocks `sdk/v1`. **Blocked on a decision, not on the work.** `RouteDeps.Router` is the only mount point a plugin has, `legacy/app/app.go:1504` fills it with the `*gin.Engine` from `buildAPIRouter`, and no field type both drops gin and leaves that call site compiling and mounting. Measured: replacing the field with a `[]Route` return breaks the build at `legacy/app/app.go:1504: unknown field Router in struct literal of type routesapi.RouteDeps`, and that one line is the entire break. ADR-20260824-dym quarantines `legacy/` and the retirement of that server owns the order, so the change waits on that order rather than on a design. The shape it takes is `httpapi.Mount`: a pattern, an `http.Handler` and a reason. The callback returns them and the host mounts them under the namespace the registration is qualified with, which is what makes a design requirement implementable at all.
- `testify` is no longer a requirement of `sdk/go.mod`. The assertions in `sdk/event` are seven helpers over `reflect.DeepEqual` in `sdk/event/assert_test.go` and `sdk/routesapi` compares in place. It satisfies rule 2 without cutting the cost rule 2 names: `go list -m all` in the sdk module reports 40 modules before and after, because `gin` requires `testify` itself. The graph shrinks when `gin` goes, and not before.
- `connectorapi` is in `sdk/` and an out-of-tree connector is possible: a driver is `Baseline()` plus one direction, the host validates arguments before `Do`, and `Register` selects one from configuration the way `authapi` and `eventsapi` do. What the old note described is in `legacy/connector` and is not the contract.
- The `replace` in rule 7 blocks `go install <module>/cmd/...@version`. Drop it once the sdk is tagged.
- The `enforcement` section names one gate and there are now two. `TestAnOutsideModuleCompilesAgainstTheSdk` in `internal/arch/sdkconsumer_test.go` builds a throwaway module against the published import path with no `replace`, and `make sdk-check` runs both. Rule 7 means no other target in this repository ever resolves the module rather than the directory.
