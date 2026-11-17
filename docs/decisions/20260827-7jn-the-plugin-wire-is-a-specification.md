---
title: "ADR-20260827-7jn: the plugin wire is a specification and the Go is one implementation"
description: "The frame an out-of-process extension speaks is published as a page and a vector file. sdk/wire is held to them, and so is every other language."
date: 2026-08-27
status: accepted
tags:
  - adr
  - extensions
  - sdk
---
# ADR-20260827-7jn: the plugin wire is a specification and the Go is one implementation

- status: accepted
- date: 2026-08-27

<!-- nav:related -->
> [!NOTE]
> Builds on [ADR-20260824-nyw](20260824-nyw-extension-mechanisms-and-the-hot-path.md), [ADR-20260824-gfr](20260824-gfr-the-extension-sdk-is-a-separate-module.md) | Index: [decisions](README.md)

The community writes drivers we do not maintain. Earlier research chose the
wire and drew the frame in a findings document. A diagram inside a research
note is not something a stranger implements against. So the contract was Go,
and the `sdk` module was a Go library with a wire hidden inside it.

## rules

1. The published page is the contract. `docs/src/content/docs/plugin-wire-protocol.md`
   is normative, and `sdk/wire` is one implementation of it. Where the two
   disagree the page is right and the Go is a defect.
2. A frame is a nine-byte header and a payload: a payload length, a stream id
   and a kind. The two integers are big-endian.
3. The five kinds are the closed set of wire major 1. Kind 0 is not a kind, so
   a header of nothing but zeroes is refused.
4. Stream 0 is not a stream. An odd id was opened by the host and an even id by
   the child, which is HTTP/2's parity rule, so neither side asks before it
   opens one.
5. Every size is bounded and the page states each bound. A reader checks the
   length field against its cap before it allocates, because a cap applied
   after the read is a report and not a bound.
6. A refusal is final. The bytes behind a header the reader declined belong to
   a frame it never parsed, so it stops and the host closes the channel.
7. A control payload is one JSON-RPC 2.0 object and never a batch. The frame
   already multiplexes, and two multiplexers disagree about ordering.
8. Bulk bytes travel in a kind of their own. The same bytes inside a JSON
   string measured 1,023 to 1,069 microseconds at 64 KiB against 50.1.
9. Back pressure is an explicit credit frame, and only a data frame spends
   credit. A receiver that stops granting stops the sender.
10. The child prints one JSON line before its first frame. A member this
    version does not define is ignored, which is what lets one child serve a
    host older than the version it was built against.
11. Two peers with different majors exchange no frame. The minor is the newest
    both implement, which is Terraform's rule.
12. Every conformance vector is bytes a person wrote down. A vector a codec
    produced proves only that the codec agrees with itself.
13. The specification ships with an implementation in a second language, and a
    gate runs it against the published vectors. That run is the evidence that
    the page is enough to implement from.
14. `sdk/wire` frames bytes and does nothing else. Launching a child, dialling
    a socket, encoding JSON-RPC and keeping a credit balance are each a policy
    the host owns.
15. The package is `wire` and not `plugin`. `plugin` is a standard library
    package name, and ADR-20260824-nyw rejects Go's `plugin` package, so the import
    would name the mechanism this project refused.
16. The frame kinds are not a vocabulary under
    [ADR-20260825-bhe](20260825-bhe-every-vocabulary-is-a-gated-published-resource.md) rule 10.
    A peer learns what the other carries from the handshake, so the endpoint
    that rule asks for is the negotiation itself.
17. The three policies rule 14 keeps out of the codec live in one package,
    `sdk/wirerpc`, and both sides use it. A host and a child that each wrote
    their own would disagree about credit, and only one of them would be right.
18. A stream the peer opens is opened by a control request or by a grant. A
    stream carries a method call, and the side that opens one may make room
    before it speaks. A data frame that opens one is refused.
19. A sender past its balance ends the connection. The balance is state each
    side keeps rather than something a header states, so this is not a frame
    refusal and it carries no vector.
20. This repository publishes one method set, the lifecycle set: `plugin.info`
    and `plugin.shutdown`. Every child answers both, whatever contract it
    serves, and a contract publishes its own methods beside them.
21. A handler runs behind a recover. A panic answers the call rather than ending
    the session, which is what
    [ADR-20260824-nyw](20260824-nyw-extension-mechanisms-and-the-hot-path.md) rule 3 asks for. A
    handler is code the host did not write.

## rejected

| option | why not |
|---|---|
| gRPC | 7.31 MiB bare, against 5.81 to 6.61 MiB of headroom, and 5 to 7 times the round trip of a plain frame |
| generated bindings from a schema | LSP and MCP both have large communities and neither generates them. An afternoon implements this whole wire |
| JSON for bulk bytes | 20 times the cost at 64 KiB, measured in earlier research |
| a JSON-RPC batch | the frame multiplexes already. Two orderings over one channel is a defect an author cannot debug |
| generating the vectors from `sdk/wire` | it would check the codec against itself, which is the shape seven gates in this tree were already found in |
| publishing the kinds as a vocabulary with an endpoint | a peer that cannot reach the endpoint is exactly the peer that needs the answer. The handshake carries it |
| naming the package `plugin`, as earlier research does | the standard library holds that name, and ADR-20260824-nyw rejects the package it belongs to |
| a shared memory ring | no portable Windows path, and a peer that scribbles in shared memory corrupts the host with nothing to catch it |

## open items

- `runtime: service` reaches no host. The mode parses, validates and reaches
  the resource spec, and `resourcehost` still builds every instance in process.
  `internal/extension/wirehost` is what it will be handed to. The old backlog carries the
  work.
- No contract publishes a method set. Rule 20's lifecycle set is what a host
  calls, and a printer driver and an event sink each still need their own.
- A write on a starved stream waits for the connection to end, and no deadline
  bounds it. `Stream.Call` takes a context and `Stream.Write` does not.
- The read loop writes a grant, so a peer that stops reading can block it. The
  channel a host hands a child is a pipe pair with a kernel buffer. The frames
  that loop writes are small enough not to fill one.
- The rule that a stream id is used once per connection is stated and not
  enforced. A session releases a stream both sides have ended. A frame naming a
  released id is dropped, and a peer that re-opens one in its own half opens a
  second stream on it.
- Wire major 1 has one minor, which is 0. Rule 11 is exercised by vectors and
  by no release.
- The second implementation is Python and `make wire-check` runs it, so that
  target needs `python3` the way `make docs-check` needs node. A machine with
  neither fails the target rather than skipping it.
