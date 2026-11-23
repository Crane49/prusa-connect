---
title: The nouns
description: Printer, resource instance, origin, principal, grant, event, capability and verb. What each one is, where it is defined, and how they relate.
sidebar:
  order: 3
audience: build
rewrite:
  describes: current
  implementedBy:
    - internal/kernel/ref
    - internal/authz
    - internal/printer/capability
    - sdk/event
    - sdk/connectorapi
---

A handful of words carry the whole system. Getting them straight is most of
what makes the code readable, because every package is named after one of them
and every API path addresses one.

This page is the long form of each. The
[glossary](../glossary.md) is the short form, and also holds the terms these
pages use without defining: the Go concepts and the outside standards.

## How they fit together

```
                      +-----------------------+
                      |        origin         |  kind:id, the address of
                      |   defined in sdk/event|  anything that emits events
                      +-----------+-----------+
                                  |
                is the address of |
                +-----------------+------------------+
                |                                    |
      +---------v---------+                +---------v----------+
      |      printer      |                | resource instance  |
      | dials in, is      |                | declared in config,|
      | admitted, never   |                | supervised, has a  |
      | declared          |                | kind and a name    |
      +---------+---------+                +---------+----------+
                |                                    |
    resolves to |                            has an  |
      +---------v---------+                          |
      |    capability     |                          |
      |  a set of verbs   |                          |
      +---------+---------+                          |
                |                                    |
                |         both are objects a         |
                +----------+  grant is written  +----+
                           |    on or about     |
                           v                    v
                    +--------------------------------+
                    |             grant              |
                    |   object#relation@subject      |
                    |   defined in internal/authz    |
                    +---------------+----------------+
                                    |
                             names  |
                        +-----------v-----------+
                        |       principal       |
                        | user, group,          |
                        | service_account, sink,|
                        | history, node,        |
                        | device_credential     |
                        +-----------------------+

           every state change above becomes an  event
           an envelope with an origin, a type and a class,
           defined in sdk/event
```

## Printer

One physical machine. It is **intrinsic**: nothing in configuration names it. A
printer registers with a connector, an operator approves it, it dials the
websocket, and the device plane admits it.

- the id is a random version 4 UUID, minted by `mintDeviceID`
  (`internal/app/deviceidentity.go:149`) when the printer's registration is
  approved, and bound to the printer's unit UID so the same machine comes back
  under the same id. The registration is the identity: a printer that changes
  owner is registered again and given a second id, and the first one keeps the
  previous owner's records. The canonical UUID form is a
  [DNS-1123](../glossary.md#dns-1123) label, so the id is legal as an
  authorization key, a URL segment and an event subject at once
- the state entry is `device` (`internal/app/devices.go:234`): the session the
  host commands through, and a map of path to raw JSON that the driver's
  patches build up
- a printer exists in the table for as long as its driver session does, not for
  as long as its socket does. A printer that is switched off keeps its session,
  which is what lets a command queue for it
- `connected` reports whether a socket is attached right now
  (`capability.Session.Connected`,
  `internal/printer/capability/session.go:173`)

The addressable state tree is a flat map of path to value. The paths a
connector writes are its own; xBuddy's table is
`internal/connector/xbuddy/inbound.go:147`, which maps `state`, `temp_bed`,
`axis_x`, `progress` and eight others onto `/state`, `/temperature/bed`,
`/axis/x`, `/job/progress`. The nozzle and the material belong to a head, so
they land in the tool tree under `/tools/<id>`. A null value removes the node
(`internal/app/devices.go:653`), which is how "the printer stopped reporting a
chamber" is told apart from "the chamber reads zero".

## Resource instance

One configured, running copy of a resource type. A storage root, an event sink,
an identity provider, a printer driver. Several instances of one type is the
normal case.

```yaml
resources:
  storage:
    local:              # the instance name
      type: filesystem  # the registered type, fixed at creation
      enabled: true
      required: false
      config:
        root: ./storage
```

- the declaration lives in the operator's file or in the machine-owned overlay,
  and `internal/app/resource` merges the two. The operator's file wins
  (`internal/app/resource/model.go`, package doc)
- `ref.Ref` (`internal/kernel/ref/ref.go`) is `kind:name`, the operator-chosen
  identity. `ref.UID` is the host-minted one, so renaming an instance does not
  orphan the rows that reference it
- `internal/app/resourcehost` turns a change into one of six outcomes: none,
  in-place, restart, replace, start, stop
  (`internal/app/resource/reconcile.go:17`)
- each running instance is wrapped by `internal/extension/supervisor`, which
  owns the queue, the drop policy, the per-call timeout, panic recovery and the
  circuit breaker

## Kind

The closed set of resource kinds, `internal/kernel/ref/ref.go:51`. There is no
registration hook: a new kind is a change to that file.

| kind | configurable | supervised | what it is |
|---|---|---|---|
| `storage` | yes | yes | where uploaded files and job artefacts live |
| `auth` | yes | yes | an identity provider |
| `sink` | yes | yes | an outbound destination events are delivered to |
| `driver` | yes | yes | a printer connector |
| `db` | yes | yes | a database engine the server keeps its own state in |
| `camera` | yes | yes | a source of stills or video for a printer |
| `history` | yes | yes | a store for the measurements and events the hub has forgotten |
| `link` | yes | yes | this server's connection to the servers above and below it |
| `asset` | yes | yes | a file this server holds on behalf of a module |
| `gcode-preset` | yes | no | a stored, named gcode sequence. Nothing runs one |
| `dashboard` | yes | no | a page of cards a person lays out. Nothing runs one |
| `printer` | no | no | one physical printer, registered by the device plane |
| `node` | no | no | a peer site this server is linked to |

The prose for each is in `internal/kernel/ref/vocabulary.go`, and it is what
the published [resource kinds page](../resource-kinds.md) and `GET
/api/v1/vocabularies/resource-kinds` both read.

## Origin

What an event came from, and the address the event hub routes on: `kind:id`.
`sdk/event/origin.go`.

An origin is both the hub's channel key and the authorization key, which is why
a printer id and a resource instance id have to obey the same grammar.

The closed set of kinds lives in `internal/kernel/ref`, and `sdk/` cannot see
it: it is a separate Go module, so Go's own rule about `internal/` puts that
package out of reach. So
`sdk/event` checks the one thing it can, that a kind survives being rendered
into an origin and parsed back out of it, and the core checks membership of the
set (`sdk/event/origin.go:48`). One vocabulary, each half checked by the layer
that can see it.

```
device:p4f2a91c0e33b              one machine
storage:local                     one configured instance
sink:audit                        one configured instance
```

## Principal

Who is calling, as the authorization graph keys them: `type:id`.
`internal/authz/vocabulary.go:11`.

| type | is |
|---|---|
| `user` | a person |
| `group` | a team of principals, nesting through `group` member |
| `service_account` | an integration or a script |
| `sink` | one declared event sink, which nothing can authenticate as |
| `history` | one declared history provider, which nothing can authenticate as either |
| `device_credential` | one printer's own credential, and not the printer |
| `node` | a peer site |

`device` is the object a grant is written on, not a principal. The thing a
machine presents is a `device_credential`, and it holds the `agent` relation on
exactly one device.

The prefix is applied in exactly one place, `graphPrincipals.Principal`
(`internal/app/auth.go:330`). An issuer names a subject in its own namespace
and knows nothing of the graph's types; only the composition root knows which
implementation produced an id, so only it can say which type the id belongs to.

A deployment with no configured issuer serves every request as
`user:<core.principal>` through `unauthenticated`
(`internal/app/principal.go:20`). That is a wiring choice, not a bypass: there
is no path on which a credential check is skipped, because on that wiring there
is no credential to check.

A declared sink reads the bus as `sink:<instance name>` (`sinkPrincipal`,
`internal/app/sinks.go:50`), and sees exactly what that principal was granted.
Declaring a sink grants it nothing. The type is its own and not
`service_account`: under one type a service account of the same name would be
the same principal, holding one set of grants. No credential resolves to `sink`,
which is why it is the one non-human type an org role accepts. It is accepted at
viewer, and that is how one tuple feeds a sink the whole fleet.

## Grant

One relation tuple: `object#relation@subject`. `internal/authz/tuple.go`, and
the whole model is documented in `internal/authz/doc.go`.

The subject may itself be `object#relation`, which is what makes groups, nested
groups and hierarchy inheritance one mechanism instead of three.

```
device:p4f2a#viewer@user:alice            alice may read that printer
device:p4f2a#org@org:local                that printer belongs to org local
org:local#admin@user:operator             operator administers the org
resource_kind:storage#manager@org:local#admin
                                          org admins configure storage
```

The object type is `device` and never `printer`. `printer` is a resource kind
in the configuration vocabulary, and a grant written on it is refused.

Roles are ordered `manager > operator > viewer`, with `admin` above them on an
org. A printer's effective role for a principal is the highest of its direct
grant, the grants on every device group above it, and the org role. A `blocked`
tuple removes the role layer entirely, so one deny covers every action and
cannot be forgotten when an action is added.

Grants on a resource are written on the **kind**, not on the instance. The
question the API asks is not "may you touch this storage backend" but "may you
configure this server's storage". Three reasons are set out in
`internal/authz/doc.go`, and the shortest is that creating an instance names no
instance, so whatever authorizes a create is necessarily written on the kind.

## Action

What a caller performs, and the only thing a handler ever checks. No call site
names a role, which is what lets the role layer change without touching
handlers.

| action | on a device, needs | on a resource kind, needs |
|---|---|---|
| `observe` | viewer, or the device's own agent | |
| `operate` | operator | |
| `configure` | manager | |
| `administer` | manager | |
| `view_audit` | manager | |
| `report_telemetry` | agent only | |
| `fetch_job` | agent only | |
| `view_resource` | | viewer |
| `operate_resource` | | operator |
| `configure_resource` | | manager |
| `read_storage` | | viewer |
| `write_storage` | | operator |
| `admit_device` | | manager |
| `develop_device` | | manager |

The first three resource actions are about an instance's declaration: what it
is, where it points and whether it runs. `read_storage` and `write_storage` are
about what is inside one. Seeing that a storage instance exists is not reading
the files in it, and dropping a file into one is not permission to repoint it or
delete it.

An action belongs to exactly one object type and the two sets of bits are
disjoint. Asking a device action of a resource kind is refused rather than
answered no: the lookup would miss for a reason that reads like a denial.

A verb carries `operate`, `configure` or `administer`. There is no read class
for a verb, because answering even a pure read writes to a transport that
carries one command at a time. `observe` sits beside that set and no verb may
carry it.
`authz.ActionForVerb` derives the action from the verb, which is the one place
every transport passes through. The whole set, with the relations that confer
each action, is the
[authorization vocabulary](../authorization-vocabulary.md).

## Capability and verb

A **verb** is one key in a closed, dotted vocabulary:
`core.command.start_print`. A **capability set** is the verbs one printer
accepts, each with a descriptor for its arguments. `sdk/connectorapi`.

A verb and one of its arguments compose with `#`:
`core.command.start_print#path` (`sdk/connectorapi/capability.go:34`). The
separator is not `.` because argument names contain dots.

Three tiers are merged by **intersection and never by union**
(`internal/printer/capability/resolver.go`, package doc):

```
   driver baseline          what this driver's code can do at all.
   Driver.Baseline()        The ceiling. Nothing can add to it
          |
          | narrowed by
          v
   embedded profile         the firmware matrix for this model and this
                            version, compiled in and read through
                            profile.Builtin()
                            internal/printer/profile/profiles.json
                            internal/printer/profile/profile.go:38
          |
          | narrowed by
          v
   the printer's own probe  what the machine said when it connected
          |
          v
   the resolved set         what the API offers, what a command is checked
                            against, and what the arrival event's digest hashes
```

The worst a wrong or stale profile can produce is a missing capability, never a
phantom one. A printer no profile covers stands in the narrowest place the data
supports: the baseline bounded by what that driver's profiles cover between
them. A driver that ships no profiles keeps its whole baseline, which is the
one case the resolver cannot narrow.

The **capability epoch** moves on every arrival and not only when the merged
set changes (`internal/app/devices.go:308`). A disconnection the host did not
watch can carry a firmware update, and "nothing changed" is not something it
can promise about time it did not observe.

Every command is checked above the driver, in `capability.Session.Do`
(`internal/printer/capability/session.go:130`), never inside it. A driver
reached with an argument nothing checked is a driver reachable from an API
route.

A firmware version is a `connectorapi.FirmwareLabel`
(`sdk/connectorapi/driver.go:92`). It is a struct and not a string, which in Go
means `<` and `>` will not compile against it: asking whether one firmware is
newer than another is a build failure rather than a silent answer. Behaviour
comes from the resolved capability set, never from a version comparison, because
a firmware lineup that forks is not a totally ordered set. Two gates in
`internal/arch` and a forbidigo rule hold that where the type cannot.

## Event

One [CloudEvents](../glossary.md#cloudevents) 1.0 envelope by attribute name,
ours by definition. `sdk/event/envelope.go:85`.

| attribute | is |
|---|---|
| `id` | this event's identity |
| `source` | `urn:prusa:site:<site>` plus the origin |
| `type` | reverse-DNS, past tense, from the closed core set |
| `time` | when it was published |
| `subject` | the origin, as `kind/id` |
| `data` | the payload, JSON |

There is no CloudEvents SDK behind it. The attribute set is short enough to
write out, and the SDK would pull zap and json-iterator into a binary that has
neither.

### Class

The discriminator that decides queueing, propagation and expiry
(`sdk/event/envelope.go:62`). A node that has never heard of a type still knows
what to do with it.

| class | queue | replay |
|---|---|---|
| `telemetry` | conflated, at-most-once | never replayed, carries no offset |
| `state` | FIFO, durable | replayable, never silently dropped |
| `command` | FIFO | the only class that may carry a TTL |
| `audit` | FIFO, durable | replayable, signed per event |

### The core types

Declared once in `sdk/event/vocabulary.go`, and published as the
[event type reference](../event-types.md). The namespace
`com.prusa.connect.` is closed: the hub refuses a publication naming a type
under it that the vocabulary does not carry. A type under anybody else's
namespace is checked for syntax and carried (`sdk/event/vocabulary.go:213`).

These are the ones a reader of this page meets first.

| type | class | is |
|---|---|---|
| `device.arrived` | state | a printer joined the fleet |
| `device.departed` | state | a printer left it |
| `device.presence.changed` | state | a printer went away or came back |
| `device.telemetry.sampled` | telemetry | one measurement changed |
| `resource.state.changed` | state | a declared instance started, stopped or failed |
| `stream.continuity.lost` | state | a reader fell too far behind to resume |

### Position and cursor

A **position** is one event's place: epoch, origin and offset. A **cursor** is
a reader's place across every origin it follows. `sdk/event/cursor.go`.

Both are on the wire. An SSE frame's `id` is a position, and a browser replays
it in `Last-Event-ID` with no client code involved. A window that moves a
reader on more than one origin ends with a frame carrying the whole cursor, so
a reconnect resumes every origin rather than one.

Telemetry frames carry no id at all. An offset counts an origin's durable
events, and a reader that never receives one must not be handed a number that
moves when other origins publish (`internal/transport/stream/hub.go:352`).

## Fault

A failure on its way to a client, carrying a registered `domain/REASON` code.
`sdk/fault/fault.go:48`. It renders as [RFC 9457](../glossary.md#rfc-9457)
`problem+json` over HTTP, as an
envelope's `data.error` on the bus, and as the error on a printer command
result, from one `Payload` (`sdk/fault/payload.go:12`). The whole failure
vocabulary is [when things go wrong](./when-things-go-wrong.md).
