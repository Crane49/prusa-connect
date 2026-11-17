---
title: "ADR-20260921-wkf: Fractal is a module and the protocol is in the SDK"
description: "sdk/fractal is the Go implementation of the Fractal specification on the standard library, the NATS transport and the bridge to this server's printers are the module packages/fractal, the core carries no link, and a command from the server above runs under the principal command_principal names."
date: 2026-09-21
status: accepted
amends: [ADR-20260829-t7d]
tags:
  - adr
  - federation
  - fractal
  - extensions
  - security
---
# ADR-20260921-wkf: Fractal is a module and the protocol is in the SDK

- status: accepted
- date: 2026-09-21

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Supersedes in part [ADR-20260829-t7d](20260829-t7d-a-child-dials-one-parent-and-may-only-narrow-it.md) | Builds on [ADR-20260827-wmv](20260827-wmv-a-module-is-mounted-and-an-adapter-is-constructed.md), [ADR-20260910-74m](20260910-74m-a-module-declares-what-it-depends-on.md), [ADR-20260910-cqq](20260910-cqq-graphql-and-the-old-schemas-are-modules.md)

Fractal is Prusa's protocol for a tree of servers. Each server joins one server
above it, accepts servers under it, sends the state of its printers up and takes
commands down. This record says where the protocol lives, where the connection
lives, and what the core keeps of both.

The short answer is that the core keeps a resource kind, an API and a pair of
events. Everything else is a module the operator can disable.

## rules

### what a link is

1. A link carries the state of this server's printers up to the server above
   it, and the commands of that server down. A print file travels by URL, and
   the node that needs the file fetches it.
2. The core carries no link implementation. A link is a module: one process of
   its own, declared under `resources.link` with `runtime: service`. A
   declaration of kind `link` asking for `runtime: builtin` is refused when the
   configuration file loads, and the message says to write `runtime: service`.
3. The Fractal module is `packages/fractal`. Its `module.yaml` declares
   `kind: link`, and `packages/fractal/README.md` states what each setting
   does.

### where the protocol lives

4. `sdk/fractal` is the Go implementation of the Fractal specification. The
   Fractal team writes and owns that document. Where the package and the
   document disagree, the document is right and the package has a defect.
5. `sdk/fractal` imports the standard library alone. It does no input and no
   output, starts no goroutine and reads no clock, so any node or tool speaks
   the protocol without this server.
6. A comment in `sdk/fractal` cites the section of the specification its rule
   comes from. A conformance table pairs each requirement of the specification
   with the test that holds it.
7. The transport is separate from the protocol. `packages/fractal/natsbind`
   carries Fractal messages over NATS, and `packages/fractal/bridge` maps this
   server's printers onto Fractal interfaces.
8. `packages/fractal` is a Go module of its own, so no NATS server, no NATS
   client and no JWT library enters the core's dependencies.

### what the core asks a link for

9. The contract is `sdk/linkapi`: the reading of the link, and the three
   decisions a person takes about a server below. The core calls them over the
   session the extension host opened to the module process.
10. Every action on a link is reachable over HTTP. `GET /api/v1/links` and
    `GET /api/v1/links/{name}` take `view_resource` on the instance. Approve,
    reject and revoke take `operate_resource`.
11. A server holds at most one link instance, because a server joins one server
    above it. A second declaration of kind `link` is refused when the
    configuration file loads, and the resource API refuses one the same way.
12. The core publishes the decision a person took, on the route that person
    called, because the person is known there and not in the module. A module
    cannot publish those event types, so nothing below the core writes a record
    naming a person who decided nothing.

### who a command runs as

13. A command from the server above runs under the permissions of the principal
    `command_principal` names. The default `node` names the server above, as
    `node:<its Node ID>`. The value `self` names the module. The module refuses
    every other value before it starts.
14. The module checks no permission itself. The host checks the named principal
    against its own authorization graph, and a call the graph refuses is
    answered as an ordinary refusal with nothing run.
15. The `permissions` list of a manifest is an upper limit on what the module
    may do as itself, and it grants nothing. `module add -approve` maps each
    permission onto the smallest role on the organization that reaches it and
    writes those grants. A permission needing a role above viewer is named and
    left to the operator, who writes it on each printer.

### ports, keys and admission

16. A module may listen on a port of its own for a protocol of its own. The
    Fractal module opens the addresses its `accept` block names, and the
    defaults reach only programs on the same machine. The links page shows
    every port the module opened.
17. The module keeps its private key in the directory its settings name. An
    operator may supply a key instead. The key appears in no log and in no API
    answer.
18. TLS is a setting of the module, with the certificate of this server and the
    authorities it trusts. Without that setting nothing on the wire is
    encrypted.
19. A server joins the server above it through the join exchange of the
    specification. With `ask_first` a server that asks to join waits for a
    person to approve it. The person approves it on the links page of the
    server above. Revoking takes the access off the open connection and
    blocks that name on this server.

### testing

20. A test network of servers in containers is part of the tree.
    `make mockup-up` starts them and joins them into one network,
    `make mockup-check` drives the sequence and fails with a plain sentence,
    and `make mockup-down` removes what they wrote.

## enforcement

| rule | gate | what it refuses |
|---|---|---|
| 2, 11 | `go test ./internal/app/config/install/` | a link declared as builtin, and a second link instance |
| 5 | `make sdk-check` | an import outside the standard library in `sdk/`, test files counted |
| 6 | `go test ./sdk/fractal/...` | a conformance row naming a test that does not exist |
| 8 | `make side-modules-check` | a module beside the core that does not build in its own folder |
| 13 | `go test ./packages/fractal/` | a `command_principal` value the module does not accept |
| 16, 19 | `make mockup-check` | a join, an approval or a revocation that does not happen |
| 15 | the install path's own tests | an install that records a permission and writes no grant |

Rules 1, 9, 12, 14, 17 and 18 have no gate and are held by review.

## consequences

- the core gains a resource kind, a links API and two event types, and nothing
  of the protocol
- the Fractal module ships enabled in the default configuration, and an
  operator can disable it
- the protocol is published under `sdk/`, so a change to a message, a field or
  a signature input is a change to `sdk/` under
  [ADR-20260827-qvk](20260827-qvk-a-published-version-is-refused-or-it-is-not-published.md)
- ADR-20260829-t7d rules 8 to 11 give way to the join exchange of the specification
- ADR-20260829-t7d rules 12 to 14 and 39 do not apply, because the wire is the
  specification's

## rejected

### the link compiled into the core

The core stays small, and a connection to a network outside the building is a
capability an operator chooses. A link in the binary charges every deployment
for code most of them never start. It also puts a NATS server and a JWT library
in the dependency list of a server that talks to printers on one desk.

### a protocol of this project's own under the Fractal name

The Fractal team owns the specification, and a second protocol under that name
splits the network. `sdk/fractal` implements their document and cites it, so a
disagreement is a defect here and never a variant of the protocol.

### a private Go module for the protocol

A build that links it needs a deploy token for a private repository. The
credential then lands in two Dockerfiles, a compose secret, the race lane and
thirteen CI jobs, and an outside contributor cannot build the full binary at
all.

### the protocol under `internal/`

A node is not this server. A printer farm's own tool, a test harness and a
second implementation all have to speak the protocol, and `internal/` is
reachable by this module alone.

### a specification document with no Go package

Nothing compiles against a page, so two implementations of one page drift. The
package is one implementation of the specification, which is the arrangement
`sdk/wire` already has.

### the transport inside `sdk/fractal`

The SDK is what an outside author compiles against, and a NATS server costs
about 10 MB in every binary that imports it. Keeping the transport in
`packages/fractal` lets a second transport exist without touching the protocol.

## what is not decided

- which end ships first: this server as a node under Prusa's cloud, or as the
  server above other nodes
- whether trust on first use is allowed on a self-hosted network
- whether a person's identity ever crosses the link for audit
- what a server above holds on this server after it admits it: the viewer role,
  or the operator role
