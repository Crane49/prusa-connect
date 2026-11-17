---
title: Developer view
description: The page at /dev that shows the connectors this build carries, the resource instances, the printers that have dialled in, one printer in detail and the event stream as it arrives.
audience: extend
rewrite:
  describes: current
  implementedBy:
    - internal/transport/devtools
    - internal/transport/http/httpapi/devtools.go
---

`GET /dev` is a page for somebody with a real printer on the bench and a
question about what the server is actually doing. It is served from the binary,
needs no build step and no second process, and reaches nothing outside the
origin. Everything on it is either build metadata or a rendering of what the
caller already reaches through `/api/v1/devices`, `/api/v1/readiness` and
`/api/v1/events`, so it holds no authority of its own.

## Links

| what | where |
|---|---|
| the page | `/dev` on the server's own origin |
| the connector inventory | `/api/v1/dev/connectors` |
| one printer's command log | `/api/v1/dev/devices/{device_id}/commands` |
| the OpenAPI document | `/openapi.json`, tag `developer` |

## Panels

| panel | what it answers | where the data comes from |
|---|---|---|
| connectors | which dialects this build carries, whether their routes are served, how large each baseline is | `/api/v1/dev/connectors` |
| routes | every path this origin answers that the document does not publish, and why | `/api/v1/dev/connectors` |
| resource instances | which instances are declared, their supervision state, and whether the process is ready | `/api/v1/readiness` |
| printers | every printer that has dialled in, its identity, session, capability count and epoch | `/api/v1/devices` |
| selected printer | the full resolved capability set, the last state values, and the commands this process carried | `/api/v1/devices/{id}` and `/api/v1/dev/devices/{id}/commands` |
| live wire | every frame the event hub delivered to this reader, filtered to the selected printer | `/api/v1/events` |

## Authorization

The page and the connector inventory take the same identity every other
operation takes: the bearer credential this server issued, or the session cookie
a browser carries. There is no exception beside the router: the operations
register through `huma.Register` like every other route, so they are
in the published document and the route-and-document diff covers them.

Each panel is narrowed by the operation behind it. `/api/v1/devices` resolves
`observe` inside the graph's own query, `/api/v1/readiness` names an
instance only to a caller who may see it, and `/api/v1/events` authorizes once
at subscribe and attaches the reader to the origins they may observe. The
command log is reached through the same printer lookup the printer operations
use, so a printer the caller may not act on and one that is not there answer the
same refusal.

The page, the connector inventory and the command log also take
`develop_device` on the driver resource kind, which an org administrator holds
from the first start. What the inventory carries is build metadata: which
dialects were compiled in, which paths this origin serves and why, and how many
capabilities each driver's baseline declares.

## The live wire

The wire panel is the event hub's own stream, read the way any other subscriber
reads it. There is no second mechanism, no tap on the frame path and no lock
added anywhere a frame passes: the hub's bounded queues and its drop-with-a-
counter policy are what apply, so a page nobody is looking at costs a printer
nothing.

Two things follow from that, and both matter when reading the panel:

- frames are coalesced into a window before they leave the hub, so two updates
  to one telemetry path inside one window arrive as one
- a telemetry frame carries no id, because telemetry is conflated rather than
  replayed, so there is no place in one to resume from

The page reads the stream with `fetch` and parses the frames itself rather than
using `EventSource`. `EventSource` delivers a named event only to a listener
registered for that name, and the frame worth seeing on a bench is the one
nobody expected.

## Content security

The document carries one script and one stylesheet, each named in the
`Content-Security-Policy` by the hash of its bytes, under
`default-src 'none'`. `connect-src 'self'` is what the fetches and the event
stream need. A page served from a binary whose script was edited without a
rebuild will not run that script, and
`TestThePolicyNamesTheScriptAndStyleThePageActuallyCarries` is the gate that
fails first.

## What it does not show

- raw dialect frames. What the wire panel shows is the host's own event stream,
  which for a printer is one frame per state patch the driver produced. The
  bytes on the xBuddy websocket are not published anywhere a reader can reach
- commands that did not go through this process. The log is what this binary
  sent while it has been running, bounded to the newest 256 across the whole
  fleet
- timestamps on a state value the printer reported before the page was opened.
  The `seen` column is when this page saw the frame carrying that path
