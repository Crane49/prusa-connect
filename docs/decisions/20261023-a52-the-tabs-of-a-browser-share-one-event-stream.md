---
title: "ADR-20261023-a52: the tabs of a browser share one event stream"
description: "All the tabs of one browser that talk to one server as one person read a single GET /api/v1/events stream: one tab reads it and passes each event to the others over a BroadcastChannel."
date: 2026-10-23
status: accepted
tags:
  - adr
  - events
  - frontend
  - sdk
---
# ADR-20261023-a52: the tabs of a browser share one event stream

- status: accepted
- date: 2026-10-23

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Builds on [ADR-20260909-c7x](20260909-c7x-a-client-sdk-is-generated-from-the-core.md), [ADR-20260911-4p6](20260911-4p6-a-module-extends-the-ui-through-one-library-and-four-slots.md)

A browser opens at most six connections to one host over HTTP/1.1. A server
on a LAN is reached over plain HTTP, and plain HTTP is HTTP/1.1. The event
stream keeps its connection for as long as the page is open, and each tab of
the web app read a stream of its own. With six tabs open, the six streams held
every connection. Any other request of any tab then waited for ever, and the
whole web app stopped. ADR-20260911-4p6 rule 23 already asks for one stream per page.
This record asks for one stream per browser.

## rules

1. The web app builds its client with `shareEvents: true`. A module page reads
   events through the client the host hands it, so it shares the same stream.
2. With `shareEvents`, the TypeScript client serves every `events()` call of
   every tab from one stream for each server and person. One tab reads the
   stream and posts each event on a BroadcastChannel, and the other tabs
   listen there.
3. The channel name holds the person's subject, such as `user:ada`, and the
   server's address when the client names one. A client of the page's own
   server names none, and needs none: a BroadcastChannel reaches only the
   pages of one origin. A subscription that names no subject asks
   `GET /api/v1/principal` first. Tabs of two people never use one channel.
4. The tab that opens the stream asks `GET /api/v1/principal` first. When the
   cookie belongs to another person by then, it opens nothing, and every
   subscription ends with a 401 and no code. A stream the server ends ends
   every subscription in every tab in the same way. So does a refusal of the
   union, such as a window out of range that one subscription asked for.
   Each consumer then acts as it does on a stream of its own.
5. The tabs choose the reading tab over the same channel. A tab that joins
   asks who reads. With no answer, it claims the role, and of the tabs that
   claim it at one time the smallest id takes it. The reading tab gives up the
   role when it stops and when its page goes away. A listening tab asks again
   every five seconds and claims the role when nobody answers. Two reading
   tabs that hear each other leave the one with the smaller id.
6. A tab that takes over opens the stream from the last place it heard, with
   `Last-Event-ID`. That place is the whole cursor the server closes each
   window with, and not one event's id, so every origin resumes. The reading
   tab passes it on with each event and again when it gives up the role.
   When the server no longer holds that place, it sends
   `com.prusa.connect.stream.continuity.lost`, and each consumer reads its
   state again, as after a reconnection.
7. The reading tab opens the stream for the union of what every subscription
   asks for. That is every type any of them names, or no type filter if one
   names none. It is every origin the person may see, and the shortest window. Each
   subscription keeps only the types and origins it asked for. The reading
   tab opens the stream again when a subscription needs more than the open
   stream carries, and not when one leaves.
8. An origin the person may not observe matches nothing on a shared stream.
   It is not refused.
9. A browser without BroadcastChannel reads one stream per tab, for all the
   subscriptions of that tab.
10. The event stream is the only request the web app keeps open on its own.
    A request that stays open only while a person looks at one thing, such as
    a camera's live view, is counted in the open items. A new request that
    stays open for the life of a page needs a record of its own.

## enforcement

| gate | what it holds |
|---|---|
| tests in `packages/typescript/test/shared.test.ts` | rules 2 to 9 |
| `frontend/test/journey/many-tabs.test.ts`, run by `make journey-check` | rules 1, 2 and 5 in Chromium against a running core |

The unit tests play several tabs in one process. They cover ten tabs on one
stream, the union and the filtering, and a handover from the last place. They
also cover a reading tab that stops answering, two people, a stream the server
ends, and a browser with no BroadcastChannel.

The browser test opens ten tabs over plain HTTP, on an address that is not a
secure context. Every tab loads, one tab holds the stream, and every tab hears
a change. When the reading tab closes, another opens the stream, and the next
change reaches every tab still open.

Rule 10 has no gate. It is held in review.

## rejected

| option | why not |
|---|---|
| Web Locks to choose the reading tab | only a secure context has `navigator.locks`, and a LAN address over plain HTTP is not one |
| a SharedWorker that reads the stream | Safari had none from version 7 to 15, Chrome on Android has none, and it needs a script of its own |
| close the stream in a hidden tab | it helps, but six visible windows still take every connection |
| switch the stream to a WebSocket | a WebSocket has a much higher limit, but the protocol, the server and every client would change |
| HTTP/2 | a LAN server has no certificate, and a browser speaks HTTP/2 only over TLS |
| one shared stream for each set of filters | ten tabs on ten printers would still open ten telemetry streams |

## open items

- the live view of a camera, `GET /api/v1/cameras/{camera}/stream`, is a
  multipart image that stays open while its dialog is open, one per tab. It
  counts against the six connections while it is shown
