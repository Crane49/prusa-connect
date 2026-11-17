---
title: Adapters, modules and runtimes
description: Which kind of extension you are writing, where its code lives, what each runtime costs you, and what a module can and cannot do today.
audience: extend
rewrite:
  describes: current
  implementedBy:
    - sdk
    - internal/app/config/install
    - internal/app/resourcehost
---

An extension is an implementation behind a resource instance an operator declares. The operator writes a kind, a name and a type in the `resources:` block, and the host builds, supervises and authorizes the instance. Before writing one, answer two questions in order: is it an adapter or a module, and which runtime carries it. This page answers both.

## Links

| what | where |
| --- | --- |
| every registered extension in this build | [extensions](./extensions.md) |
| the guide for a printer driver | [writing a printer driver](./writing-a-driver.md) |
| the guide for a service module | [writing a service module](./writing-a-module.md) |
| how names and namespaces are derived | [registries and names](./onboarding/registries-and-names.md) |
| the wire a service process speaks | [the plugin wire protocol](./plugin-wire-protocol.md) |

## Adapter or module

An adapter fills a contract the core wrote. A module brings a noun the core has no schema for. That is the whole line, and three questions apply it:

1. Does the thing need a database row the core has no schema for?
2. Does it need a screen of its own?
3. Does it need a permission the core does not define?

One yes makes it a module. Three noes make it an adapter.

| you are writing | it is | the contract |
| --- | --- | --- |
| a printer dialect | an adapter | `sdk/connectorapi`, and [its own guide](./writing-a-driver.md) |
| a storage backend | an adapter | `sdk/storageapi` |
| an event destination | an adapter | `sdk/eventsapi` |
| an identity provider | an adapter | `sdk/authapi` |
| a camera source | an adapter | `sdk/cameraapi` |
| a spool inventory, a farm dashboard, a compatibility shim | a module | none. The core has no schema for your noun, which is the point |

A printer driver is always an adapter and never a module, because `connectorapi.Set` is a contract the core wrote.

## The three runtimes

`runtime:` in a declaration says how the type is reached. There are three values, and they cost an author very different things.

| runtime | your code is | needs a rebuild of the host | runs |
| --- | --- | --- | --- |
| `builtin` | a Go package compiled into the host binary | yes | inside the host process |
| `service` | a binary of your own, installed beside the host | no | as a supervised child process |
| `script` | an expression written in the configuration file | no | compiled and run by the host, per event |

### builtin is the core's own

Every adapter this repository ships runs as `builtin`. The code is compiled in and registers itself from `init`, under a namespace derived from its module path. An operator selects it by type name. Shipping your own type this way means your code is linked into the server binary, which means building that binary from a checkout with your package imported.

A module may not take this runtime at all. A type outside the `core` namespace declared as `builtin` is refused at load, and the message says why:

```
level=WARN msg="a declared resource instance will not load and is not running"
  instance=resources.sink.spools error="... runtime is \"builtin\" and type
  \"com.github.aux.acme.spool-tracker/spool-tracker\" is not part of the core,
  so this declares a module this binary would run inside itself. A module runs
  as \"service\", in a process of its own, because nothing that shares this
  address space can be stopped without stopping the server with it. ..."
```

The instance fails alone and the server keeps serving. The reasoning is containment: code in the host's address space cannot be stopped without stopping the host.

### service is the customer's route

A `service` type is a separate executable. The operator's declaration names the directory it is installed in, the sha256 of the file, and the wire major it speaks. The host verifies the digest before every launch, starts the process, and speaks the [plugin wire protocol](./plugin-wire-protocol.md) to it over standard input and output.

What a running service module gets from the host:

| surface | what it is |
| --- | --- |
| an HTTP mount | the module's routes at `/api/v1/modules/<namespace>/`, authorized like every other API path |
| a record store | a key/value store under the module's own namespace, in the host's database. No other module reaches it |
| supervision | a start budget, a restart ladder, a call deadline, a circuit breaker. A dead module costs its own instance |

The wire is a specification, so the language is yours. A Go author imports `sdk/wire`, `sdk/wirerpc`, `sdk/routesapi` and `sdk/stateapi` and writes an ordinary `net/http` handler. Another language implements the protocol from [the wire page](./plugin-wire-protocol.md); `examples/wire-protocol/child.js` is that, in JavaScript for Node. Either way the artefact is one native executable per platform, named `<name>_<GOOS>_<GOARCH>`.

[Writing a service module](./writing-a-module.md) builds one end to end.

### script is an expression in the file

A `script` is not installed at all. The operator writes an expression in the declaration itself, the host compiles it once, and runs it over every event the instance is given. The language has no loop, no assignment and no function definition, so a script maps one event onto zero or more messages and can do nothing else. Only a sink offers this runtime, and a source over 8 KiB is refused before the parser sees it.

```yaml
resources:
  sink:
    audit:
      type: script
      runtime: script
      script: |
        msg("main", event.kind, event.state)
      config:
        endpoints:
          main: http://127.0.0.1:9090/publish
```

A script is the right shape for routing and reshaping events. Anything that holds a connection, keeps state or answers a request needs one of the other two runtimes.

## Which one you are writing

| your situation | write |
| --- | --- |
| a printer dialect | an adapter against `sdk/connectorapi`, compiled in. [Writing a printer driver](./writing-a-driver.md) |
| a feature with its own data, routes or screens | a module, as `runtime: service`. [Writing a service module](./writing-a-module.md) |
| forwarding or reshaping events, no state | a `script` sink in the configuration file |
| a storage, identity or camera implementation the core should ship | an adapter in this repository, `builtin`, through the contract's conformance suite |

## What a module cannot do yet

The sdk module is unpublished, so a Go author builds against a checkout with a `replace` directive; [writing a service module](./writing-a-module.md) shows the line.
