---
title: The shape of the system
description: What Prusa Connect is, the two Go modules it is built from, and a map of every major part with the package that holds it.
sidebar:
  order: 1
audience: build
rewrite:
  describes: current
  implementedBy:
    - cmd/prusa-connect
    - internal/app
    - sdk
---

The pages that take a developer from nothing to knowing where to look. Read
this one first: it names every part once, and every other page is one of those
parts in detail.

These pages use a lot of words as terms of art, and assume a reader who writes
Go. The [glossary](../glossary.md) holds all of them, plus the Go concepts and
the outside standards, each with a short definition and the file that defines
it. A term is linked there the first time a page uses it.

Everything here describes the core, which is the packages under `internal/` and
`sdk/` and what the binary runs with no subcommand. The implementation that
came before it is deleted. What that server knew about printers is kept as
test data under `internal/arch/testdata/`.

## What the product is

A self-hosted server that gives 3D printers one API over HTTP and
[server-sent events](../glossary.md#server-sent-events), with no cloud
dependency. One static binary, one printer or four
hundred, a Raspberry Pi or a rack server.

A printer is not configured. It dials in, identifies itself, and the server
admits it. What that printer can do is resolved from three sources and offered
as a fixed set of [verbs](../glossary.md#verb). Who may read it or command it is
a graph of [relation tuples](../glossary.md#relation-tuple) in a local SQLite
file. Everything that varies, a storage backend, an event
[sink](../glossary.md#sink), an identity provider, a printer dialect, sits
behind a contract in `sdk/` and is declared in one configuration file.

## The two modules

| module | path | who compiles against it |
|---|---|---|
| the application | `github.com/prusa3d/prusa-connect` | this repository |
| the contracts | `github.com/prusa3d/prusa-connect/sdk` | this repository and every extension author |

`sdk/` is a Go [module](../glossary.md#module) of its own so an extension author
inherits the standard library and nothing else. A Go build pulls in everything
its dependencies depend on, so sharing a module means sharing that whole graph;
splitting it is what keeps a sink author's build from acquiring this
application's web framework, its SQL drivers and its metrics library.
`go.mod:65` resolves it with a `replace` directive until it is tagged.
[The sdk page](./the-sdk.md) gives the reasoning.

## The whole system in one diagram

The two listener addresses are `core.api.addr` and
`core.connectors.<name>.config.listener.addr`.

```
   people, scripts, browsers                          printers
             |                                            |
             | HTTP, and SSE at /api/v1/events            | websocket
             v                                            v
   +---------------------------+          +---------------------------+
   | API listener              |          | printer listener          |
   | core.api.addr, always     |          | the connector's           |
   | bound                     |          | listener.addr, when set   |
   +-------------+-------------+          +-------------+-------------+
                 |                                      |
                 |    one sealed router, one origin     |
                 +------------------+-------------------+
                                    |
                    internal/transport/http/httpapi
                                    |
    +--------------+----------------+---------------+---------------+
    |              |                |               |               |
    v              v                v               v               v
 printers      resources         grants          events        device mounts
 /api/v1/      /api/v1/          /api/v1/        /api/v1/      /p/register
 devices       resources         grants          events        /p/ws
    |              |                |               |               |
    v              v                v               v               v
 +---------+  +-----------+  +-----------+  +-----------+  +-------------+
 | device  |  | resource  |  | authz     |  | event hub |  | connector   |
 | plane   |  | store and |  | graph     |  |           |  | drivers     |
 |         |  | plane     |  |           |  |           |  |             |
 +----+----+  +-----+-----+  +-----+-----+  +-----+-----+  +------+------+
      |             |              ^              ^               |
      |             |              |              |               |
      +-------------+--------------+--------------+---------------+
                                   |
                  everything is constructed and joined in
                  internal/app, the composition root
```

Read the diagram top to bottom for a request and bottom to top for an event.
The five boxes on the bottom row are `internal/app/devices.go`,
`internal/app/resource` with `internal/app/resourcehost`, `internal/authz`,
`internal/transport/stream` and `internal/connector/xbuddy`.

## Every major part in one sentence

| part | package | one sentence |
|---|---|---|
| the command | `cmd/prusa-connect` | parses flags, installs the signal handler, calls `app.Run` |
| the composition root | `internal/app` | reads config, builds every part in dependency order, serves, stops in reverse |
| configuration | `internal/app/config` | the `core:` block, its defaults, and the validation that runs before a port is bound |
| the resource store | `internal/app/resource` | the operator's file plus the machine-owned overlay, merged into one inventory |
| the supervision plane | `internal/app/resourcehost` | carries out a reconcile plan by starting, stopping and replacing instances |
| the supervisor | `internal/extension/supervisor` | one wrapper per running instance: queue, timeout, panic recovery, circuit breaker |
| the type registry | `internal/extension/registry` | compile-time registration of resource types, sealed when the host drains it |
| the device plane | `internal/app/devices.go` | admits an arriving printer, registers it, holds its state tree and its session |
| printer drivers | `internal/connector/xbuddy` | one directory per dialect, speaking the wire a printer's firmware already speaks |
| capability resolution | `internal/printer/capability` | narrows a driver's baseline by a profile and a probe, and checks every command |
| capability profiles | `internal/printer/profile` | the embedded firmware matrix, compiled in and reached through `profile.Builtin()` |
| the authorization graph | `internal/authz` | relation tuples in SQLite, and the one place that answers who may do what |
| the event hub | `internal/transport/stream` | one channel per origin, two queue classes per subscriber, one marshal for all |
| the HTTP surface | `internal/transport/http/httpapi` | the sealed router, every operation, and the OpenAPI document it must agree with |
| the developer view | `internal/transport/devtools` | one page at `/dev` showing connectors, printers and the live event stream |
| identity | `internal/platform/auth` | OAuth 2.1 with PKCE against an OpenID Connect issuer, credentials in memory |
| the contracts | `sdk/` | what an extension implements: driver, storage, sink, identity, events, errors |
| the reference adapters | `adapters/` | working implementations of those contracts, all linked into the shipping artefact |
| the dependency law | `internal/arch` | the allowed imports as data, and the tests that hold the real import graph to them |

## The three things that never become pluggable

The event [envelope](../glossary.md#envelope), the registry, and the
authorization graph. They are the
[vocabulary](../glossary.md#vocabulary) every resource speaks. Making any of
them configurable breaks the property the product exists for: that any printer,
any storage, any event is addressed the same way from the API, the frontend and
a federated peer.

A device's state set is declared by its adapter and placed by the host.

## Where to go next

| you want | read |
|---|---|
| how the process starts and stops | [from main to a serving process](./the-entry-point.md) |
| what a printer, a grant, a capability actually is | [the nouns](./the-model.md) |
| how a temperature reaches a browser | [one frame end to end](./the-data-flow.md) |
| what `sdk/` is for | [the sdk](./the-sdk.md) |
| how a thing gets registered and named | [registries and names](./registries-and-names.md) |
| which directory holds what | [where things live](./where-things-live.md) |
| what happens when something breaks | [the failure paths](./when-things-go-wrong.md) |
