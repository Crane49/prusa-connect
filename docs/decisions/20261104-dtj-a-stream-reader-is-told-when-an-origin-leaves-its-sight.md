---
title: "ADR-20261104-dtj: a stream reader is told when an origin leaves its sight"
description: "The event stream sends one com.prusa.connect.stream.origin.withdrawn frame per origin a reader loses, so a reader stops showing what it may no longer see."
date: 2026-11-04
status: accepted
amends: [ADR-20261023-a52]
tags:
  - adr
  - events
  - sdk
  - frontend
---
# ADR-20261104-dtj: a stream reader is told when an origin leaves its sight

- status: accepted
- date: 2026-11-04

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Amends [ADR-20261023-a52](20261023-a52-the-tabs-of-a-browser-share-one-event-stream.md) rule 7

When a grant is removed while a stream is open, the hub detaches the origin
and drops its queued events. The reader hears nothing more from that origin and
cannot tell it from a quiet one. A removed extension, a removed printer or a
lost dashboard stays on screen until a reload.

## rules

1. The hub sends `com.prusa.connect.stream.origin.withdrawn` once for each
   origin a reader loses mid-stream. Its payload names the origin.
2. It also sends one for each origin the reader's resume cursor names and the
   reader may no longer see. This applies only when the reader did not name
   `origins` itself. A reader that named `origins` is refused at open or closed
   mid-stream instead.
3. A resume across a server restart gets `continuity.lost` and a resync, not
   withdrawals: the cursor does not belong to this server's numbering. After a
   restart, a reader following no origins gets neither.
4. Never for an origin the reader neither held nor named. An origin the cursor
   names but the reader never held gets the same frame as one that never
   existed, so the frame says nothing about whether the origin exists.
5. It is sent only when the reader's `types` admit it.
6. Its `id` is the batch cursor, which no longer names the withdrawn origin, so
   a reconnect does not repeat the frame.
7. On a shared tab stream (ADR-20261023-a52) a subscription matches this frame
   by the origin the payload names, not by the origin of the frame's position.
   This amends a52 rule 7: that rule's origin match applies to events, and this
   frame is matched by its payload.
8. A reader that sees the frame stops showing that origin's state and says it
   is not here. It does not redirect, and it does not wipe the stores of other
   origins.

## enforcement

The stream tests under `internal/transport/stream/` cover rules 1 to 6. The
TypeScript SDK and `frontend/src/lib/stores/live-updates.svelte.ts` test rule 7.
`make event-types-check` holds the frame's type.

## rejected

| option | why not |
|---|---|
| tell the reader nothing and let each screen poll | a removed item shows until a reload, and every screen writes its own check |
| send the frame for any cursor origin, even to a reader that named `origins` | such a reader is already told by the stream closing, and two signals disagree |
| a frame that says why the origin left | it would tell a reader whether an origin exists, which a reader that may not see it must not learn |
