---
title: The Fractal link
description: The link resource kind, the module that speaks the protocol, what the core asks a link for, the routes a person reaches it through, and the events the core writes about a decision.
sidebar:
  order: 8
audience: build
rewrite:
  describes: current
  implementedBy:
    - internal/app/links.go
    - internal/app/linkreading.go
    - internal/transport/http/httpapi/links.go
    - sdk/linkapi
---

Fractal is Prusa's protocol for a tree of servers. Each server joins one server
above it and accepts servers under it. A server sends the state of its printers
up and takes commands down. This page says what the core does about that, which
is the resource kind, the API and the events. The protocol itself is not in the
core.

## The kind and the module

The resource kind is `link`. It is supervised, and a deployment declares at most
one instance of it, because a server has one connection to the network above it.
A second `link:` block fails every link block in the file rather than starting
the one that sorted first, and the resource API refuses a second link the same
way. The rule is `Kind.SoleInstance` in `internal/kernel/ref/ref.go`.

The core carries no link implementation. A link is a module: one program in a
process of its own, installed with `prusa-connect module add` and declared
under `resources.link` with `runtime: service`. The Fractal module is
`packages/fractal`, and its README says what each of its settings does. A
declaration of kind `link` that names a type with a namespace and asks for
`runtime: builtin` is refused when the file loads, and the message says to write
`runtime: service` instead.

## What the core asks a link for

The contract is `sdk/linkapi`. It has four calls, and the core makes every one
of them over the session the extension host opened to the module process:

- `link.status` returns the whole reading. It says what this server is called
  in the network, where it sits in the tree and what its public key is. It also
  says which server is above it, which listeners it opened, and which servers
  are under it.
- `link.approve` admits one server that is waiting for a decision.
- `link.reject` removes one server that is waiting, and frees the name it asked
  under.
- `link.revoke` takes the admission away from a server that already has one.

`internal/app/linkreading.go` wraps a supervised instance as a caller of those
four. `internal/app/links.go` turns what comes back into the body the API
serves. A target with no `Call` method answers none of this, and such a link is
reported as running with nothing under it.

A call goes through the supervision plane, so it counts against the instance's
concurrent calls and ends at the instance's call budget. A module that never
answers fails the one request and holds up nothing else.

## What a person sees

`GET /api/v1/links` is every link the caller may see, and
`GET /api/v1/links/{name}` is one of them. Both take `view_resource` on the link
instance. A link the caller holds no grant on answers the same refusal as a
name no link carries. The listing gives away nothing about a link the caller
may not read.

Each link carries whether it is enabled and whether it is running. A running
one adds what it does on the wire and how it admits servers. It also adds its
own name in the network, its public key, the addresses it listens on and the
server above it. Everything only a running link can answer is absent while it
is not running.

Under each link is every server it has met. One of them carries its name in the
network, whether it is admitted or waiting, whether it is connected, when it was
last seen, and the round trip in milliseconds. A reading nobody has taken is
absent rather than zero, because a latency of zero and no latency are different
answers.

A person decides about a waiting server through
`POST /api/v1/links/{name}/nodes/{id}/approve`,
`/reject` or `/revoke`. All three take `operate_resource` on the link instance,
which is the level above reading it. Each answers with the link as it stands
after the module confirmed the decision.

## The events

The core publishes the decision a person made, on the route the person called,
because the person is known there and not in the module. `link.node.approved`
and `link.node.revoked` carry the link, the server and who decided. A rejection
publishes nothing: the module's own reading says which servers are waiting, and
this server has no event type for a name that was never admitted. The types are
in `sdk/event/vocabulary.go` and the bodies in `sdk/event/linknode.go`.

A module cannot publish those two types, so nothing below the core can write a
record naming a person who decided nothing. Everything else a link reports,
including `link.state.changed`, the module publishes itself through the event
stream the extension host hands it.

## Seeing it work

`make mockup-up` starts four servers in containers, each with the Fractal module
installed, and joins them into one network. `make mockup-check` drives the whole
sequence and fails with a plain sentence when a step does not happen.
`make mockup-down` removes the containers and everything they wrote.
`docker/mockup/README.md` says what each server is for.
