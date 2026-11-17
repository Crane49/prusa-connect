---
title: Architecture
description: What the core binds, how a printer's data reaches an API client, which package holds each step, and what a minimal build leaves out.
audience: build
rewrite:
  describes: current
  implementedBy:
    - internal/app
    - internal/connector/xbuddy
    - internal/transport/http
    - internal/transport/stream
---

Prusa Connect is one Go binary. A printer dials it and speaks the xBuddy
websocket protocol. On the other side the binary serves an HTTP API, an event
stream a browser reads, and event sinks that push the same events to a broker.
This page follows that path once and names the package behind each step.

To find the file that handles a particular thing, read [where things
live](./onboarding/where-things-live.md) instead.

## The two listeners

`internal/app/config` reads one YAML file and the core binds at most two
sockets.

| listener | key | serves |
|---|---|---|
| API | `core.api.addr` | the HTTP API, the event stream at `/api/v1/events`, the interactive API reference at `/docs` |
| printer | `core.connectors.<name>.config.listener.addr` | the wires a device dials |

The connector's `listener.addr` is empty by default, and then printers dial the
API listener like every other client. Set it when the two clients need different
certificates. A printer verifies one CA certificate loaded from a USB stick and
speaks TLS 1.2 with a single ECDSA cipher suite. A browser verifies against its
own trust store and should be held to TLS 1.3. One socket cannot do both: the
printer's floor becomes everybody's floor.

See [configuration](./config.md) for every key.

## How a reading reaches a client

A reading arrives as a frame on the websocket.

1. `internal/connector/xbuddy` decodes the frame
2. it writes the reading into the device state tree, which is `internal/kernel`
   code with no network in it
3. the hub in `internal/transport/stream` fans the change out once
4. every open event stream gets it, and so does every sink an operator declared,
   such as `adapters/events/mqtt`

A command travels the other way. `internal/transport/http` takes the request,
the same connector frames it, and the printer answers on a frame carrying the
command's id.

`internal/authz` decides who may see or do each of those. It holds relation
tuples rather than roles, so one graph answers for people, service accounts,
devices and sinks.

Detail for each step:

- [the application core](./internals/the-application-core.md)
- [transport and the stream](./internals/transport-and-the-stream.md)
- [printers and drivers](./internals/printers-and-drivers.md)
- [the authorization graph](./internals/authorization.md)

## The wire a printer speaks

Each frame the server sends is a type letter, an eight-digit hex id, then the
payload.

| Type | Wire format | Example |
|------|-------------|---------|
| High-level (JSON) | `J{id:08x}{json}` | `J00000001{"command":"START_PRINT","kwargs":{...}}` |
| Low-level (G-code) | `G{id:08x}{gcode}` | `G00000001G90` |
| File transfer | `T{id:08x}{binary}` | binary data |

One command is in flight per printer. The next frame goes out after the printer
answers the last one with a matching id. A file offset and a file id are both
`uint32` on the wire, so a file of 4 GiB or more is not expressible.

The full reference is [the xBuddy websocket
protocol](./prusa-xbuddy-websocket-protocol.md). Which model accepts which
command is [capabilities](./capabilities.md).

## Build variants

| target | builds |
|---|---|
| `make build` | the binary, plus the web UI if the configuration file asks for it |
| `make build-full` | every optional feature at once, with the web UI |
| `make build-minimal` | the smallest binary: `CGO_ENABLED=0`, the `minimal` tag, and no Node.js step |

A minimal build links no Postgres, no MQTT client and no observability exporter,
so it is the easiest one to cross-compile.

A configuration file never names a build tag. `adapters/tags.json` is the one
place a declared type or a config condition maps onto one, and
`cmd/detect-build-tags` reads it during `make build`. See [build tags a
configuration implies](./config.md#build-tags-a-configuration-implies).
