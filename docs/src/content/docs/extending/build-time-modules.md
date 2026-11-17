---
title: Build-time modules
description: An adapter is a Go module of its own, outside this repository's module, and it is in a binary because one build configuration names it.
sidebar:
  order: 6
audience: build
rewrite:
  describes: current
  implementedBy:
    - cmd/compose-build
    - examples/compose
    - examples/adapters
    - test/compose
    - sdk
---

An adapter is a Go module of its own. It compiles against `sdk/` and nothing
else, it lives wherever its author keeps it, and it is in a binary because one
build configuration named it. There is no runtime loading, no plugin directory,
no build tag and no list inside this repository to add a line to.

Two worked examples ship in `examples/adapters/`, and each is a different kind
of thing. `storage-memory` is a storage provider, and `moonraker` is a printer
dialect for somebody else's machines. Both used to be, or could have been, in
the core tree. Neither is.

## The build configuration

One file names the core and every adapter. This is `examples/compose/build.yaml`:

```yaml
core:
  path: ../..

adapters:
  - module: example.com/prusa/storage-memory
    path: ../adapters/storage-memory
  - module: example.com/prusa/moonraker
    path: ../adapters/moonraker

config: ../../config/config.yaml
variant: full
output: ../../bin/server-composed
```

Every path is read against the file's own directory, so the configuration means
the same thing wherever it is run from. `path` is development mode and pins
nothing; a released adapter is named by `version:` instead, and that is the mode
where `go.sum` records what was pulled.

## The module

An adapter's `go.mod` requires the sdk and nothing of the core:

```
module example.com/prusa/storage-memory

go 1.26.7

require github.com/prusa3d/prusa-connect/sdk v0.0.0

replace github.com/prusa3d/prusa-connect/sdk => ../../../sdk
```

The `replace` is what a development build does while the sdk carries no
published version. A released adapter requires a tagged sdk and carries none.

The nested `go.mod` is what makes the directory not part of the core module: Go
excludes it from `./...` here, and the compiler refuses an `internal/` import
across the boundary. The rule that an adapter sees only the published contract
is enforced by the toolchain rather than by review.

## The entry point

Registration happens in an `init` function, which the generated composition
module reaches through a blank import. A storage provider:

```go
//nolint:gochecknoinits // registration has to happen before main.
func init() { storageapi.Register(Type, Doc(), New) }
```

A printer dialect is `connectorapi.Register`, and a device class with verbs of
its own adds `connectorapi.RegisterKind`. Nothing else is needed and nothing
else is read. The host derives the adapter's namespace from its module path and
writes it in front of every name the adapter chose, so one author cannot claim
another's.

## The configuration

An operator selects the adapter by the name it registered. A storage provider is
a resource:

```yaml
resources:
  storage:
    scratch:
      type: memory
      enabled: true
```

A dialect is a connector, and its endpoint is where the machine is:

```yaml
core:
  connectors:
    klipper:
      type: moonraker
      enabled: true
      endpoints:
        - address: voron.local:7125
```

A binary composed without the module does not know the name at all, and says so
at startup rather than at the first request.

## Building it

```
make compose-build CONFIG=examples/compose/build.yaml
```

The tool generates a throwaway module in a scratch directory, requires the core
and every adapter the configuration names, and builds one binary. The core
checkout it ran in is unchanged afterwards, which is the property the gate in
`test/compose` measures rather than asserts.

Ask a finished binary what it carries:

```
go version -m ./bin/server-composed | grep '^\sdep'
```

## Writing one

`go run ./cmd/scaffold-driver -name acme -module example.com/prusa-driver-acme`
writes a module that compiles, registers and passes the published conformance
suite, with a `build.yaml` beside it. Add `-kind acme-machine` for a device
class of its own, or `-storage` for a storage provider instead of a dialect.
