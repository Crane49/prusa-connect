---
title: The sdk
description: Why sdk/ is a separate Go module, what an extension author imports, what stable means here, and the smallest working sink.
sidebar:
  order: 5
audience: build
rewrite:
  describes: current
  implementedBy:
    - sdk
    - adapters
---

`sdk/` is a Go [module](../glossary.md#module) of its own. That one fact
explains most of its shape. This page shows what it means in practice.

## Why a second module

Go resolves dependencies one module at a time: everything a module requires,
every consumer of that module requires too, whether or not it calls any of it.
An author implementing one contract at the application module root would
therefore inherit the whole application's dependency graph in order to reach
`errors`, `io` and `strings`. `go.mod` in this repository requires huma, chi,
gorilla, paho, prometheus, pion, an expression evaluator and both SQL drivers,
and behind them everything those require. None of that is anything a
[sink](../glossary.md#sink) author needs.

`sdk/` imports nothing outside the standard library.
`TestTheSdkImportsOnlyTheStandardLibrary` in `internal/arch/layout_test.go`
reads the sdk module's package list and fails on a third-party import,
including a test-only one, because the cost the rule names is the graph an
implementer inherits and a test dependency lands in `sdk/go.mod` exactly as a
production one does. `sdk/go.mod` requires nothing at all.

The second module also lets the contracts reach a stable major version on their
own schedule. A host release and a contract release are separate tags: one for
the sdk module and one for the application. Until the sdk module is tagged, the application
resolves it with `replace github.com/prusa3d/prusa-connect/sdk => ./sdk`
(`go.mod:65`). One resolution mechanism, identical for a developer, for CI and
for the Docker build, and no `go.work`.

## What is in it

```
   sdk/
     |
     +-- extension/      the registration rule, the namespace, the descriptor
     |                   every other package here is a typed wrapper over it
     |
     +-- fault/          the error taxonomy: domain/REASON codes, RFC 9457
     +-- event/          the envelope, the origin, the cursor, the event types
     |
     +-- connectorapi/   a printer dialect      -> connectorapitest
     +-- storageapi/     a storage backend      -> storageapitest
     +-- eventsapi/      an event sink          -> eventsapitest
     +-- authapi/        an identity provider   -> authapitest
     +-- historyapi/     a store for the past the hub has forgotten
     +-- cameraapi/      a camera source        -> cameraapitest
     +-- routesapi/      a module's extra HTTP routes
     +-- stateapi/       a module's own records
     +-- scriptapi/      the environment a script sink is evaluated in
     +-- linkapi/        the link to the server above this one
     +-- manifest/       what a module declares about itself
```

The five contracts with a `test` suffix each ship a conformance suite in a
subdirectory. That is the point of publishing them: the suite is run by this
project against its own implementation and by a community author against
theirs. Both pass the same suite or neither ships.

## What every contract package has

Look at any of `connectorapi`, `storageapi`, `eventsapi` and `authapi` and you
find the same parts. Learning one teaches the others.

| thing | example in `sdk/eventsapi` |
|---|---|
| the interface an author implements | `Processor` (`sdk/eventsapi/provider.go:106`) |
| the spec it is built from | `ProviderSpec` (`sdk/eventsapi/provider.go:120`) |
| the factory signature | `ProviderFactory` (`sdk/eventsapi/provider.go:126`) |
| the contract version | `Contract = extension.Version("1.1.0")` (`sdk/eventsapi/provider.go:156`) |
| `Register`, `TryRegister`, `Declare`, `Drain` | the registration surface |

`ProviderSpec.Config` is an open `map[string]any` rather than a typed struct on
purpose: a broker URL, an exchange, credentials and an organization id are
provider-private, and the host must not name them.

## What stable means

Three separate things carry a version, and conflating them is the usual
mistake.

| version | is | changes when |
|---|---|---|
| the sdk module tag | the contract package surface | a contract changes |
| `Contract` in each package | the contract an implementation compiled against | that one contract changes |
| `Doc.Version` in a descriptor | the implementation's own version | the implementation changes |

`Contract` is checked at registration (`extension.Version.CompatibleWith`,
`sdk/extension/descriptor.go:168`), so an incompatible extension is refused
before anything is built rather than at first use.

Once the sdk module carries a `v1` tag, its exported surface is additive only. CI runs
an exported-API diff against the last tag and a breaking change fails the build. One open
item stands before that tag: dropping the `replace` directive.

## The smallest real extension

An event sink is the shortest complete example, and
`adapters/events/webhook/webhook.go` is a working one meant to be copied. Four
pieces:

```go
// 1. the type name an operator writes in configuration
const Type = "webhook"

// 2. registration, from init, which is the one thing that has to happen
//    before main. A distribution build reaches it with a blank import.
func init() { eventsapi.Register(Type, doc(), New) }

// 3. the descriptor. Summary, Description, Version, Contract and Stability
//    are mandatory: a missing one refuses the registration rather than
//    shipping an empty string as documentation.
func doc() extension.Doc {
    return extension.Doc{
        Summary:     "Posts each domain event to one HTTP endpoint as JSON.",
        Description: "One endpoint, one POST per event, no retry.",
        Version:     "1.0.0",
        Contract:    eventsapi.Contract,
        Stability:   extension.StabilityStable,
        Emits:       []extension.Emission{ /* ... */ },
        Options:     []extension.Option{ /* ... */ },
    }
}

// 4. the factory and the Processor it returns
func New(spec eventsapi.ProviderSpec) (eventsapi.Processor, error) { /* ... */ }
```

An operator then declares an instance of it:

```yaml
resources:
  sink:
    audit:
      type: webhook
      enabled: true
      config:
        url: https://audit.example.com/events
```

And grants it something, because a sink reads the bus as `sink:audit` and a
principal granted nothing receives nothing. One printer:

```
POST /api/v1/grants/device/{device_id}
{"relation": "viewer", "subject": "sink:audit"}
```

The whole fleet, including a printer admitted tomorrow, is one tuple on the
organization instead:

```
POST /api/v1/grants/org/local
{"relation": "viewer", "subject": "sink:audit"}
```

That works because the org edge is written by the same function that admits a
device. A fleet of four hundred then needs nobody at a keyboard for the four
hundred and first machine. A sink is the one non-human principal an org role
accepts, and only at viewer.

The grant takes effect on a running sink within a second, with no restart.

### What the contract asks of the implementation

`Processor`'s methods are fire and forget: an implementation must not block and
must not panic (`sdk/eventsapi/provider.go:52`). The webhook
[adapter](../glossary.md#adapter) keeps that promise on its own with a bounded
queue drained by one sender [goroutine](../glossary.md#goroutine), and a full
queue drops, counts, and says so at `Close`.

The host does not trust it either way. Every call goes through the
[supervisor](../glossary.md#supervisor) in `internal/extension/supervisor`,
which owns the queue, the drop policy, the per-call deadline, panic recovery and
the circuit breaker. Dropping is the
correct degradation for a sink: a dead endpoint must not stall the event bus.

There are two things the host cannot do, and
`internal/extension/supervisor/instance.go:1` names both. A timeout is a
deadline on a [context](../glossary.md#context), and a context only stops work
that reads it, so a resource that ignores the one it is given blocks its caller.
A `recover` only catches a panic on the goroutine that deferred it, so a
resource that panics on a goroutine it spawned itself takes the process down.
Containment for code that cannot be trusted that far is tier 2 and tier 3 of
the [plugin architecture](../plugin-architecture.md). Both are built: `internal/extension/wirehost` launches and speaks to
a child process for `runtime: service`, and `internal/extension/script`
compiles and evaluates an expression for `runtime: script`, which only a sink
may declare.

## Where the reference implementations are

`adapters/` holds one directory per reference implementation, each
conformance-tested. They are reference implementations first and
shipped features second. One of them is deliberately trivial for that reason.

| adapter | kind |
|---|---|
| `adapters/auth/oidc`, `adapters/auth/prusaaccount` | identity |
| `adapters/events/mqtt`, `adapters/events/webhook`, `adapters/events/amqp`, `adapters/events/kafka` | sink |
| `adapters/camera/mjpeg`, `adapters/camera/push`, `adapters/camera/rtsp` | camera |
| `adapters/db/sqlite`, `adapters/db/postgres` | record backend and history store |
| `adapters/connector/echo` | driver |
| `adapters/storage/local`, `adapters/storage/temporary` | storage |

The shipping artefact links every one of them, and
`internal/app/builtin/adapters.go` names each qualified type the resource plane
admits. `echo` is mounted only where `core.connectors` names it, because it
authenticates nothing.

`adapters/storage/diskstore` is the directory-on-disk implementation both
storage adapters are built from. It registers no name of its own, because
`local` and `temporary` each register one and a second registration of one
qualified name is a panic before `main`.

The blank imports are in `internal/app/builtin/adapters.go`, and the adoption
lists beside them are what the resource plane lets an operator declare.
`TestTheShippingArtefactLinksTheAdaptersItPublishes` in
`cmd/prusa-connect/link_test.go` holds both directions: an adapter the
composition root admits and the artefact does not link fails, and so does one
deliberately left out that gets linked.

## The rules sdk/ follows

1. public contracts live under `sdk/`, which is its own module
2. `sdk/` imports nothing outside the standard library
3. a contract never exposes an application-internal type. What an implementer
   receives is an [interface](../glossary.md#interface) `sdk/` owns, listing the
   methods that implementer actually calls
4. `sdk/` is tagged independently of the application. A release tags both
5. after the sdk module's `v1` tag the exported surface is additive only
6. every port carries a conformance suite in a subdirectory and a working
   implementation under `adapters/` or `examples/` that CI compiles
7. the application resolves the sdk with `replace` until it is tagged
8. every per-module tool runs once per module. `go test ./...`, `go mod
   download`, `go mod verify` and golangci-lint each see one module per
   invocation
9. `sdk/**` is in the CI path filter, so a contract-only change still runs the
   backend jobs
