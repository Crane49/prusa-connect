---
paths:
  - "**/*.go"
---

# Go standards

Write the plain, obvious version. Prefer clear code to clever code, composition to inheritance, and explicit to implicit. A zero value, what a variable holds before anything is assigned to it, should be usable as it is. These are the house rules on top of `gofmt`, `go vet` and `.golangci.yml`, which is the linter's configuration.

The package layout rules are in [ADR-20260824-kkr](../../docs/decisions/20260824-kkr-repository-layout-and-package-boundaries.md) and `internal/arch/layout_test.go` enforces them. This file covers what goes inside a package.

## The sdk module

`sdk/` is a separate module that third parties compile against. It has rules the rest of the tree does not.

- no import outside the standard library. Not one, and not a small chosen set
- no `map[string]any` and no bare `string` where a typed identifier is meant
- every exported symbol carries a doc comment
- after v1 the set of exported symbols only grows. Adding a method to a published interface is a breaking change, because every implementation outside this tree stops compiling
- every contract ships a conformance suite in `sdk/plugintest` and a compiling example under `examples/`. A conformance suite is a set of tests any implementation must pass

## Errors

- check every error. A discarded error needs `_ =` and a comment saying why
- wrap with context using `%w`: `fmt.Errorf("load config: %w", err)`. A caller inspects the chain with `errors.Is` and `errors.As`
- a sentinel error, one fixed value, for a known condition. A typed error when the error must carry data
- `errors.Join` to combine independent failures
- handle an error once, at the top of the stack. Do not log it at every layer and return it too
- never let internal detail (queries, stack, paths, tokens) reach a client response. Log the detail, return a safe payload
- an error that reaches a client, an event or a command result is a `fault` value carrying a registered code
- registering a code requires documenting it: what went wrong, what the caller does about it, whether a retry can help, and the parameters it fills in. A registration without them does not compile
- a code the server returns that the registry does not know fails the build. So does a code that vanished since the last release, because somebody is still handling it
- the published error reference is generated from the registry. Nobody edits it directly

## Context

- `context.Context` is the first parameter of any function that does I/O, blocks, or starts work on another goroutine. Pass the incoming one on; do not create `context.Background()` in the middle of a call
- never store a context in a struct field
- a context carries cancellation, deadlines and values that belong to one request (trace id, principal). It does not carry optional arguments
- honour `ctx.Done()` in loops and around blocking work, and pass `ctx` to every query and outbound request

## Logging

- `log/slog`, injected. Never `fmt.Println` or `log.Printf` for application logs, and never a package-level logger
- a resource receives `*slog.Logger` through its host handle and imports nothing of ours to log
- log once, at the boundary
- attach stable keys: trace id, printer id, resource name
- never log secrets, tokens, full auth headers or personal data

## Concurrency

- do not start a goroutine without knowing how it ends. A goroutine with no stop condition is a leak
- `golang.org/x/sync/errgroup` for structured concurrency, meaning a group of goroutines that start together and are waited for together
- every goroutine takes a `ctx` and returns on cancellation
- guard shared state with a mutex, or confine it to one goroutine and pass messages. Do not do both
- `defer` the unlock or the close right after the lock or the open
- no package-level mutable state outside `legacy/app`. A pointer a hot path swaps is an `atomic.Pointer[T]`, never a global guarded by a mutex

## Hot paths

The hot path is the code that runs for every message a printer sends, from the printer frame to the output each subscriber receives. Inside it:

- concrete types, not interfaces. A call through an interface is a dynamic call the compiler cannot inline
- serialise once and share the bytes. Marshalling identical data once per subscriber is a defect
- no allocation that grows with the number of subscribers
- no third-party code. Extensions attach after the state update

## Interfaces and design

- accept interfaces, return concrete structs
- define the interface where it is used, not where the implementation lives, and keep it to the methods that caller needs
- do not create an interface before something needs it. One implementation and no port role means a reviewer removes it
- compose with embedding

## HTTP

- routes are registered through the typed constructor. A handler that is not in the generated spec fails the build
- anything every handler needs (auth, logging, limits) is middleware at a named phase, never copied into handlers
- set server timeouts. `ReadHeaderTimeout` at minimum, because the zero value means no timeout
- validate and bound request bodies. Nothing may allocate in proportion to the body size
- never trust an identity header the client set

## Database

Both SQLite and Postgres are first class. Every query, migration and test runs against both.

- parameterised queries only. Never build SQL with `fmt.Sprintf` or string concatenation
- `defer rows.Close()` and check `rows.Err()` after the scan loop
- select the columns you need, not `SELECT *`
- keep transactions short, `defer tx.Rollback(ctx)`, commit on the happy path
- a list endpoint resolves permissions inside the query. Fetching rows and then filtering them in Go is a defect. It leaks through page counts, totals and timing even when the returned rows are correct
- a number measured on one engine is not evidence for the other

## Building

A compiled binary goes to `bin/`, which git ignores. `go build ./cmd/thing`
writes `thing.exe` into whatever directory it was run from.

- to run a tool once, `go run ./cmd/thing`
- to keep the binary, `go build -o bin/thing ./cmd/thing`
- to check something compiles, `go build ./...`, which writes nothing

`.gitignore` covers `*.exe`, so a stray binary never reaches a commit, but it
stays in the working tree where nobody notices it.
`TestTheRepositoryRootCarriesNoBuildArtefacts` refuses one.

## Testing

- table-driven with `t.Run` subtests, each case named
- `t.Parallel()` where the case has no shared mutable state
- no global state between tests. `t.Context()` and `t.TempDir()`
- `testing/synctest` for anything time-dependent: backoff, breakers, lease expiry, reconnection. A test that sleeps is a test that fails at random
- test exported behaviour and error paths. Every fixed bug gets a regression case, a test that fails if the bug comes back
- an authorization rule is tested in both directions. A permit test without its refusal test is half a test

## Comments

Public code. See `writing-style.md` for the full rule. In short: comment why,
never what; one or two lines; link an ADR instead of restating it; and never
write a comment that only makes sense to somebody who read the request that
produced the code.

Doc comments on exported symbols are required in `sdk/` and expected elsewhere.
They describe the contract.

## Style

- guard clauses and early returns over deep nesting. Keep the happy path at the left margin
- no naked returns except in very short functions
- short names in small scopes, descriptive names for exported identifiers, no stutter (`printer.PrinterState` repeats itself; `printer.State` does not)
- keep functions small and single-purpose
- dependency changes go through `go.mod` and `go mod tidy`, never an ad hoc `go get`
