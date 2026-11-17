---
title: One frame end to end
description: A telemetry sample from the printer's websocket to a browser and to an event sink, then a command from the API back to the printer, with the file behind every hop.
sidebar:
  order: 4
audience: build
rewrite:
  describes: current
  implementedBy:
    - internal/connector/xbuddy
    - internal/app
    - internal/transport/stream
    - internal/transport/http/httpapi
---

Two journeys. Inbound is a nozzle temperature reaching a browser and a
[sink](../glossary.md#sink). Outbound is a pause command reaching the machine.
Every hop names its file, and the two share a middle: the
[device plane](../glossary.md#device-plane) and the
[event hub](../glossary.md#event-hub).

## Inbound: a telemetry sample

```
  printer                        firmware sends text JSON at about 4 Hz
     |
     | websocket text frame
     v
 [1] xbuddy.Session.read              internal/connector/xbuddy/inbound.go:72
     |    one goroutine per connection. Reading in one goroutine is what
     |    keeps the printer's own ordering
     v
 [2] classify, then dispatch         internal/connector/xbuddy/inbound.go:107
     |    telemetry, an event, or a block request
     v
 [3] dispatchTelemetry               internal/connector/xbuddy/inbound.go:157
     |    each known key becomes one StatePatch on a fixed path. Keys are
     |    emitted sorted, and each value is passed through as the printer
     |    encoded it: re-encoding a number here would change it
     v
 [4] Session.report                  internal/connector/xbuddy/session.go:799
     |    a bounded inbox. It never blocks: it drops the oldest and counts
     |    it, because a patch that waited for the host would also hold up
     |    the event carrying a command's result. A dialog path is the
     |    exception and holds a slot the backlog cannot evict, because no
     |    later frame repeats what a dialog said
     v
 [5] Session.patchPump, State()      internal/connector/xbuddy/session.go:774
     |    the only writer to the channel, and the only thing that closes it
     v
 [6] devices.pump                                internal/app/devices.go:436
     |    one goroutine per printer. It records the patch on the state tree
     |    and publishes it
     |       entry.record(patch)                 internal/app/devices.go:653
     |       d.publish, telemetry.sampled        internal/app/devices.go:454
     v
 [7] Hub.Publish                        internal/transport/stream/hub.go:321
     |    mints the envelope, marshals it once, and hands the same bytes to
     |    every subscriber of that origin. It never blocks and takes no
     |    context
     v
 [8] channel.subscribers, then Subscription.deliver
     |         internal/transport/stream/subscription.go:181
     |    telemetry goes to the conflating queue, keyed by origin and path
     v
 [9] Subscription.pump
     |         internal/transport/stream/subscription.go:213
     |    wakes on the coalescing window, and drains both queues into one
     |    Batch
     v
     +---------------------------------+
     |                                 |
     v                                 v
[10a] eventStream.writeWindow    [10b] sinkFeed.drain
      one SSE write per window         forwardToSink translates the frame
      to one browser                   into the domain fact a sink takes

      internal/transport/http/httpapi/events.go:328
      internal/app/sinks.go:250, :293
```

### What happens at each hop

**[1] to [3]** are the dialect's business. The telemetry key to state path
table is `internal/connector/xbuddy/inbound.go:49`. A key that table does not
carry is not emitted, because inventing a path in a driver would put a second
copy of the state vocabulary there. A frame the dialect cannot read at all is
dropped and counted (`Session.refuse`,
`internal/connector/xbuddy/inbound.go:446`) rather than hung up on.

**[4]** is the one place backpressure is decided, and it decides by asking
whether the printer will say the thing again. Telemetry is a repeating snapshot,
so the oldest patch is dropped, the newest frame carries what it said, and
`Dropped()` counts it. A dialog is written when it moves and repeated by
nothing, so each of the five dialog paths holds a slot the backlog cannot evict
and `Dropped()` never counts one.

**[6]** is where the domain starts. `devices.pump` reads the state
[channel](../glossary.md#channel) and the reachability channel on the same
[goroutine](../glossary.md#goroutine), because the two are the same printer's
story and a second goroutine would have to be ordered against this one to tell
it.

**[7]** is the hot path, and the rules are strict. Nothing here allocates per
subscriber: the [envelope](../glossary.md#envelope) is marshalled once into a
`Message`
(`internal/transport/stream/encoding.go:148`) and every subscriber on an
encoding gets the same bytes (`internal/transport/stream/encoding.go:189`). The
channel registry lookup and the subscriber slice read are atomic pointer loads,
which take no lock and so cannot wait on another goroutine.
Minting an event id and appending to the replay window each take that
[origin](../glossary.md#origin)'s own mutex, uncontended because one goroutine
publishes for one origin.

Telemetry takes no [offset](../glossary.md#offset) and is not appended to the
replay window
(`internal/transport/stream/hub.go:352`). An offset counts an origin's durable
events, and a reader that never receives one must not be handed a number that
moves when other origins publish.

**[8] and [9]** are the two queue classes. Telemetry conflates: a slow reader
is given the newest sample rather than every sample it missed. Events are FIFO
and durable, and a reader that overflows is told so with a continuity-lost
frame rather than quietly given part of a stream. The coalescing window is
bounded to between 100 ms and 10 s (`internal/transport/stream/hub.go:37`),
defaulting to 200 ms, and is what makes write syscalls follow the window rather
than the event count.

## Where the browser reads

```
   GET /api/v1/events        Accept: text/event-stream
     |                       Last-Event-ID: <a position or a cursor>
     v
   middlewareStack           internal/transport/http/httpapi/server.go:407
     |   head, list-header folding, trace, recover, body limit
     v
   principalMiddleware       internal/transport/http/httpapi/middleware.go
     |                                                              :194
     |   resolves the caller. What is public is read from the document's
     |   own empty security requirement, not from a list beside the router
     v
   registerEvents handler    internal/transport/http/httpapi/events.go:102
     |   resumeFrom, a position or a whole cursor                    :205
     |   originsFrom, ?origins=kind:id,...                           :162
     v
   Hub.Subscribe             internal/transport/stream/hub.go:366
     |   reserve the per-principal slot, ask the Authorizer once, attach
     |   one channel per visible origin, replay, start the pump
     v
   eventStream.run           internal/transport/http/httpapi/events.go:251
     |   text/event-stream, no-cache, X-Accel-Buffering: no, flush, then
     |   the retry directive
     v
   eventStream.pump          internal/transport/http/httpapi/events.go:291
         a window, a heartbeat comment, the listener draining, or the
         client going away
```

The authorization decision is taken **once**, at subscribe. Channel membership
is the decision afterwards, so no event costs a permission check. The
`Authorizer` is `visibility` in the composition root
(`internal/app/stream.go:90`), which renders `authz.Store.Visible` as the
origins the hub routes on. It lives there because `internal/authz` may not
import a transport and `internal/transport/stream` may not import a domain's
policy. Neither side can hold the mapping, so the thing that builds
both does.

A [grant](../glossary.md#grant) withdrawn while the stream is open narrows it
live. `Subscription.reauthorize`
(`internal/transport/stream/subscription.go:617`) compares the graph's
[epoch](../glossary.md#epoch) against the one stamped on the subscription: a
flowing stream re-reads on every window, and an idle one re-reads on a
one-second ticker (`internal/transport/stream/hub.go:62`). The epoch is
read **before** the rows inside `Visible`, so a write landing in between leaves the
answer stamped older than the graph and the reader re-authorizes once too
often. Stamping it after would leave it newer than the rows it describes, and a
revocation would never be noticed.

## Where a sink reads

A sink is a hub subscriber like any other reader. It is not fed from the
publish path: the hub already owns fan-out, the two queue classes, coalescing
and the authorization filter, and a second fan-out beside it would be a second
copy of all four.

```
   supervisor status hook -> sinkFeed.observe        internal/app/sinks.go:99
     |    a non-blocking wake, because the hook runs on the goroutine a
     |    shutdown is waiting on
     v
   sinkFeed.reconcile                                internal/app/sinks.go:171
     |    compares the attached set against host.Supervising(ref.KindSink)
     |    rather than acting on the transition it was woken by
     v
   sinkFeed.attach                                   internal/app/sinks.go:215
     |    supervised.AttachSink(runner)
     |    hub.Subscribe as sink:<instance name>
     v
   sinkFeed.drain                                    internal/app/sinks.go:250
     |    the only goroutine that touches this sink. Every call goes through
     |    the instance's bounded queue, so a slow sink slows nothing but itself
     v
   forwardToSink                                     internal/app/sinks.go:293
        device.telemetry.sampled at /state -> PrinterStateChanged
```

One pair maps. `device.telemetry.sampled` at `/state` becoming
`PrinterStateChanged` is the only overlap between the domain facts
`sdk/eventsapi` carries and the event types the hub carries
(`internal/app/sinks.go:68`, where the type's doc comment lists what does not
map and why). Nothing else is forwarded. Inventing a domain fact to make the table look full would put a
meaning on the wire that nothing produces.

## Outbound: a command reaches the printer

```
   POST /api/v1/devices/{device_id}/commands
   {"verb": "core.command.pause_print", "args": {}}
     |
     v
 [1] registerSendCommand
                               internal/transport/http/httpapi/devices.go:929
     |    the handler holds no policy. It hands the resolved principal, the
     |    id and the command to the fleet
     v
 [2] devices.Command                         internal/app/devices.go:367
     |    authz.ActionForVerb(kind, cmd.Verb)  the action comes from the
     |    device's kind and the verb, and is never named here. This is the
     |    one place every transport passes through, so a site naming
     |    operate directly would hand the raw gcode verbs to anybody who
     |    may pause a print
     v
 [3] devices.reach -> reachHolding       internal/app/deviceaccess.go:23
     |    ParseSubject, graph.ObjectActions(who, object), then the entry
     |    a printer the caller may not act on is reported exactly as one that
     |    is not there (notFound, internal/app/devicerender.go:376)
     v
 [4] capability.Session.Do
                                    internal/printer/capability/session.go:130
     |    Capabilities.Validate(cmd): the verb is in the resolved set, every
     |    argument matches its descriptor, defaults are filled in. A refusal
     |    here never reaches the driver
     v
 [5] xbuddy.Session.Do
                                      internal/connector/xbuddy/session.go:414
     |    builds the frame, puts it on the queue, waits for the event that
     |    carries its id. A command offered while the printer is away waits
     |    for it rather than failing: the caller's context bounds the wait
     v
 [6] Session.pump -> deliver
                                      internal/connector/xbuddy/session.go:651
     |    waitLink blocks until a connection is live, then writes. A write
     |    that fails means the frame never reached the printer, so the next
     |    connection gets it
     v
 [7] encodeFrame
                                          internal/connector/xbuddy/wire.go:53
     |    nine bytes of header: one type letter and eight hex digits of a
     |    uint32 id, then the payload
     |      J<id><json>    a high-level command
     |      G<id><gcode>   raw gcode
     |    the framing is frozen: it is what firmware in the field speaks
     v
   printer
     |
     | text JSON event carrying command_id
     v
 [8] Session.read -> dispatchEvent
                                      internal/connector/xbuddy/inbound.go:159
     |    what the event says about the printer is read first and whether it
     |    answers a command second, because the two are independent
     v
 [9] Session.answer -> call.settle
                                      internal/connector/xbuddy/session.go:729
     |    the waiting Do returns. A printer that answers the same id twice, or
     |    answers after the caller gave up, changes nothing
     v
     the HTTP response: {"ok": true, "ignored": [...], "substituted": {...}}
```

### Why the check is above the driver

An earlier design decision. A driver that re-checks an argument it declared is writing the
branch the descriptor exists to delete, and a driver reached by an argument
nothing checked is a driver reachable from an API route. Putting the check in
`internal/printer/capability` means one implementation instead of one per
dialect, and it is what makes a new firmware revision a profile edit.

### What the verb is not

Many xBuddy [verbs](../glossary.md#verb) never reach the socket under the name
the [capability](../glossary.md#capability) vocabulary gives them. Some become
`G` frames of gcode and some become a single `SET_VALUE`. That mapping is
`internal/connector/xbuddy/verbs.go`, and a driver generated from the profile
data alone would get it wrong
(`internal/connector/xbuddy/xbuddy.go:28`).

## How a printer gets here in the first place

The command path assumes a printer already exists. It arrives like this.

```
   POST /p/register  {sn, fingerprint, printer_type, firmware}
     |    the driver issues a Code and the printer polls GET /p/register
     |    202 while nobody has approved it, 200 and a Token when they have
     v
   GET /p/ws   Fingerprint, Token, Sec-WebSocket-Protocol: prusa-connect
     v
   Driver.handleWebsocket
                                       internal/connector/xbuddy/xbuddy.go:426
     |  authenticate, then build the identity from the handshake headers
     v
   arrivals.Admit
                                       internal/connector/xbuddy/xbuddy.go:437
     |                                     -> internal/app/devices.go:319
     |    resolver.Arrive: baseline, narrowed by profile, narrowed by probe.
     |    The resolution is parked for the session about to arrive
     v
   already holding a session for this fingerprint?
     |  same identity  -> reattach: the new connection goes under the session
     |                    that is already there, which drains what queued
     |  different      -> close it. A command composed against one firmware's
     |                    capability set is not one to carry out on another's
     v
   Driver.join
                                       internal/connector/xbuddy/xbuddy.go:462
     |    newSession with baseline INTERSECT profile
     v
   arrivals.Arrived
                                       internal/connector/xbuddy/xbuddy.go:499
     |                                     -> internal/app/devices.go:383
     |    claim the parked resolution, register the printer in the graph and
     |    place it in org:local (internal/app/devices.go:509), wrap the session,
     |    file the entry, publish device.arrived, start the pump goroutine
     v
   session.attach
                                       internal/connector/xbuddy/xbuddy.go:506
        the socket goes under the session and the read loop starts
```

`register` (`internal/app/devices.go:997`) matters more than it looks. The
graph's first grant deliberately writes nothing on any printer, so a printer is
invisible until whatever registered it says where it belongs. Without the `org`
tuple, a printer that dialled in successfully would be visible to nobody.

## The two rules the hot path lives by

Everything above is arranged around two constraints, and a change that breaks either is a defect
whatever else it does.

1. **no per-subscriber allocation in the printer frame path.** One marshal per
   event, shared bytes, bounded queues that drop rather than grow. The
   benchmarks in `internal/transport/stream/bench_test.go` are gated on
   allocations rather than wall time
2. **no third-party code in the printer frame path.** A sink is downstream of
   the hub, behind a supervised call with its own queue and timeout, so a slow
   or panicking sink never delays a printer frame reaching a browser
