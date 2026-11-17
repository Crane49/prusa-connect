---
title: Tracing
description: The core emits no spans. This page holds the span names, attributes and causal chain a rewritten command queue is expected to emit.
audience: run
rewrite:
  describes: legacy
  implementedBy:
    - internal/platform/observability
---

**The core emits no traces.** There is no tracing key under `core:`, no
exporter, and nothing to turn on. What the core does publish about its own
behaviour is the Prometheus exposition at `/metrics`, including a request
duration histogram by route and a command duration histogram by driver. See
[Metrics](./metrics.md).

The span shape below is a specification and not a report. Nothing in
an earlier design proposal replaces it, so a command queue that starts emitting
spans is expected to use these names and these attributes. Read it before you
invent your own.

## The causal chain a command produces

The one relationship worth preserving. A command's span tree links the HTTP
request to the frame the printer answered with, through the queue.

```
POST /api/v1/devices/{device_id}/commands
  queue.add
    queue.process_next
      queue.mark_sent
      command.process            (the frame goes out on the websocket)
        websocket.message        (the printer's answer comes back)
          queue.mark_completed
            queue.process_next   (the next command)
```

A printer's connection is one long-lived span, `websocket.connection`, carrying
a `printer.connected` event, one `websocket.message` child per frame, and a
`printer.disconnected` event.

## The spans and their attributes

| Span | Attributes |
|---|---|
| the HTTP route, for example `/api/v1/devices/{device_id}` | `http.method`, `http.url`, `http.route`, `http.status_code`, `http.response.size`, `error.type` |
| `websocket.connection` | `printer.id`, `printer.model`, `printer.firmware`, `auth.authorized`, `error.type` |
| `websocket.message` | `printer.id`, `message.type`, `message.size`, `error.type` |
| `command.process` | `printer.id`, `command.type`, `command.name`, `command.id`, `message.size` |
| `queue.add` | `printer.id`, `command.id`, `command.name`, `command.type` |
| `queue.process_next` | `printer.id`, `queue.waiting`, `queue.empty`, `queue.sent`, `command.id` |
| `queue.mark_sent` | `printer.id`, `command.id` |
| `queue.mark_completed` | `printer.id`, `command.id`, `command.duration_ms` |
| `queue.mark_failed` | `printer.id`, `command.id`, `failure.reason` |

`error.type` takes one of five values.

| Value | Means |
|---|---|
| `decode_error` | a frame from the printer would not decode |
| `send_failed` | a command did not reach the printer |
| `ping_failed` | a websocket ping timed out |
| `authentication_failed` | the server refused the printer |
| `upgrade_failed` | the websocket upgrade did not complete |
