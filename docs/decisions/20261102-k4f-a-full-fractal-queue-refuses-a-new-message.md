---
title: "ADR-20261102-k4f: a full Fractal queue refuses a new message"
description: "A Fractal queue of method invocations or results that has reached its byte limit refuses a new message and keeps every message it holds. A node that cannot forward a method invocation into a full queue below does not acknowledge it, so later method invocations wait behind it."
date: 2026-11-02
status: accepted
supersedes: []
amends: []
tags:
  - adr
  - federation
  - fractal
---
# ADR-20261102-k4f: a full Fractal queue refuses a new message

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md)

A Fractal node keeps the method invocations for each node below, and the
results for the node above, in NATS JetStream streams. A stream is a queue on
disk, and `stream_max_bytes` limits its size. By default a full stream deletes
its oldest message to make room for a new one. The Fractal specification of
2026-10-20 says a pending method invocation MUST NOT be lost or deleted
(fractal-requirements.md, Method Invocation Guarantees). So a full queue refuses
the new message instead.

## rules

1. A queue of method invocations or results refuses a new message when it is
   full, and keeps every message it holds. The sender gets the error. A node
   that forwards a method invocation into the full queue of a node below does
   not acknowledge it. Its own queue from the node above gives it one message
   at a time. So every later method invocation this node receives, for itself
   or for any node below it, waits behind that one. The wait ends when the
   queue below drains, or when the waiting method invocation expires and is
   answered with `ERR_EXPIRED`.
2. A queue that an older version created with the default policy is changed to
   refuse a new message at the next start. Only the policy changes, and the
   messages that wait in it stay.

## enforcement

| rule | gate | state |
|---|---|---|
| 1, a queue of results | `TestAFullQueueRefusesTheNextMessage`, subtest `results`, in `cd packages/fractal && go test ./natsbind/` | exists |
| 1, a queue of method invocations | `TestAFullQueueRefusesTheNextMessage`, subtest `method invocations`, in `cd packages/fractal && go test ./natsbind/` | exists |
| 1, no acknowledgement of a refused method invocation | `TestARequestIsNotAcknowledgedWhenTheTransportRefuses` in `cd sdk && go test ./fractal/node/` | exists |
| 2 | `TestAStreamThatDiscardsOldIsTurnedToRefuseNew` in `cd packages/fractal && go test ./natsbind/` | exists |

## rejected

| option | why not |
|---|---|
| keep the default, which deletes the oldest message | it deletes a pending method invocation, which fractal-requirements.md forbids |
| a larger default limit | a node that stays away fills any limit, and then the default deletes again |
| answer a method invocation that does not fit with an error and acknowledge it | the caller gets an error for a node that is only away, and fractal.md has no code for a full queue |
