---
title: SSE overview
slug: sse
description: The core's event stream at /api/v1/events. How to open it, how to narrow it, the frames a reader has to handle, and how it refuses.
audience: run
rewrite:
  describes: current
  implementedBy:
    - internal/transport/http/httpapi/events.go
    - internal/transport/stream
---

The core serves one event stream, at `GET /api/v1/events`. Every frame is a
CloudEvents 1.0 envelope under the `com.prusa.connect.` type namespace, and
[Event types](./event-types.md) is the full list. This page is how you open the
stream, narrow it, and read what comes back.

## Reading the stream

One authorization is taken at subscribe, and publications are coalesced into
windows rather than written one per event.

The stream takes the same credential as every other operation: the session
cookie a browser carries, or an api credential minted at
`POST /api/v1/auth/credentials` in `Authorization: Bearer`. `EventSource`
sends the cookie and cannot set a header, and a fetch-based reader such as the
client SDK sends the header. There is no token query parameter: a token in the
URL lands in every access log and proxy log on the way. The credential is
re-checked on the keep-alive, capped at 30 seconds, so a token revoked or
expired under an open stream ends it within that.

| parameter | where | default | what it does |
|---|---|---|---|
| `Last-Event-ID` | header | none | resume from a place this server issued. `EventSource` sends it on its own |
| `origins` | query | every origin the caller may observe | narrow the stream, each origin as `kind:id`, comma separated |
| `interval_ms` | query | 200 ms | the coalescing window in milliseconds, between 100 and 10000 |

| key | default | what it does |
|---|---|---|
| `core.events.heartbeat` | `15s` | how often an idle stream writes a comment frame |
| `core.events.retry` | `2s` | the reconnection delay the stream hands its reader |
| `core.events.max_streams_per_principal` | `64` | how many streams one principal may hold |

### Frames a reader has to handle

1. an event frame carries the envelope in `data`, the envelope's type in
   `event`, and that event's place in `id`. Telemetry carries no `id`: it is
   conflated rather than replayed
2. a comment frame, `: heartbeat`, carries nothing and moves nothing
3. a frame carrying `retry:` and no data sets the reconnection delay. It
   dispatches no event
4. a frame carrying `id:` and no data stamps the whole cursor. It dispatches no
   event, and it is written after a window that moved the reader on more than
   one origin. Without it a browser replays one event's id, which names one
   origin, and every other origin resumes wherever the server puts one it was
   handed no place in
5. `com.prusa.connect.stream.continuity.lost` says the reader's place is gone.
   Its `id` is the cursor to carry on from and its payload repeats it. Refetch
   state from the API: there is no partial recovery
6. `com.prusa.connect.stream.origin.withdrawn` says the reader may no longer
   see one origin, which its payload names. The server sends it once for an
   origin lost while the stream is open. It sends it once for an origin the
   resume cursor names and the reader may not see, unless the reader named
   `origins` itself: that reader is refused or closed instead. It is sent only when
   `types` admit it. It says nothing about whether the origin exists, and an
   origin the reader never held gets none. Its `id` is the batch cursor, so a
   reconnect does not repeat it. A resume across a server restart gets frame 5
   instead; after a restart, a reader following no origins gets neither. Stop
   showing that origin's state and say it is not here.
7. `prusa-connect.auth.expired` says the credential that opened the
   stream is no longer accepted, revoked or expired, and the server closes the
   stream right after it. A reader that sees it was logged out rather than
   disconnected: log in or mint again rather than reconnecting on the same
   token

A client written against `EventSource` needs code for 1, 5, 6 and 7 only. Frames
2, 3 and 4 are handled by the browser, which is why they carry no data member.

### Refusals

| status | code | when |
|---|---|---|
| 401 | `core.auth/TOKEN_INVALID` | no credential, or one this server does not accept: unknown, expired, revoked or a broker credential. It says not which |
| 403 | `core.auth/ACCESS_DENIED` | the graph grants this caller nothing, or `origins` names something they may not observe. It names no resource either way |
| 422 | `core.validation/REQUEST_INVALID` | `Last-Event-ID`, `origins` or `interval_ms` is not something this server issued or accepts |
| 429 | `core.validation/TOO_MANY_STREAMS` | this caller already holds `core.events.max_streams_per_principal` streams. The payload carries the bound |

## SSE and MQTT

| | SSE | MQTT |
|---|---|---|
| Best for | discrete events: connect, state change | high-frequency telemetry: temperatures, speeds |
| Frequency | low, seconds to minutes | high, every second |
| Setup | none, a standard web API | needs a broker |
| Direction | server to client only | bidirectional pub and sub |
