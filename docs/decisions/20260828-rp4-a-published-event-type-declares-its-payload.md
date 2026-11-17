---
title: "ADR-20260828-rp4: a published event type declares its payload"
description: "The core event set is a specification this project publishes: an exported payload type in sdk/event, a JSON Schema generated from it, dataschema stamped by the hub, and one payload on every surface that carries the event."
date: 2026-08-28
status: accepted
tags:
  - adr
  - events
  - vocabularies
  - sdk
  - contract
---
# ADR-20260828-rp4: a published event type declares its payload

- status: accepted, and every gate is owed
- date: 2026-08-28

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) · Builds on [ADR-20260825-bhe](20260825-bhe-every-vocabulary-is-a-gated-published-resource.md), [ADR-20260826-c77](20260826-c77-one-name-for-one-thing-on-the-wire.md) · Reads with [ADR-20260826-5yj](20260826-5yj-a-device-is-the-object-and-a-printer-is-the-type.md), [ADR-20260827-qvk](20260827-qvk-a-published-version-is-refused-or-it-is-not-published.md)

A consumer that routes on `com.prusa.connect.printer.arrived` asks one more
question. What is inside `data`?

The type name answers none of it. That name is a published identifier held from
three directions, and a hand rename of it fails three test packages. The payload
behind the name is an unexported Go struct in `internal/app`. The published page
gives it a paragraph of prose and no member list. There is no standard set to
adopt: earlier research looked
and found none, so publishing this one is our own work.

Two measurements taken on branch `restructure` on 2026-08-28 say what the
missing half already costs.

The first is a mutation. `arrival`'s json tag was changed from `printer` to
`device_id` in `internal/app/deviceevents.go`. `go build ./...` succeeded, and
`internal/app`, `cmd/generate-event-types` and
`internal/transport/http/httpapi` all passed. A published payload member moved
and nothing in the tree noticed.

The second is what that permits. Both payload examples this server publishes are
wrong. `internal/transport/http/httpapi/examples.go:770` renders
`com.prusa.connect.printer.presence.changed` as
`{"printer":"...","reachable":false}`, while `presence` at
`internal/app/deviceevents.go:50` marshals `printer`, `driver` and `connected`.
Line 757 renders the telemetry sample as a map of path to value, and `sampleOf`
at `internal/app/deviceevents.go:167` marshals `{"path":...,"value":...}`.

So the payload record a consumer can read is wrong in both places it exists.
This record closes that half and leaves the type names to
[ADR-20260826-5yj](20260826-5yj-a-device-is-the-object-and-a-printer-is-the-type.md).

## rules

1. **Publish the core event set as a specification.** A type declares four
   things: a name, a class, prose and a payload schema. All four are generated
   from one table and served at one endpoint. Adopting a published vocabulary
   lost, because none covers this domain on this wire. CDEvents is continuous
   delivery, MTConnect and OPC 40540 are information models over their own
   transports, and Sparkplug names devices and metrics.
  Keeping
   the set private also lost.
   [ADR-20260827-qvk](20260827-qvk-a-published-version-is-refused-or-it-is-not-published.md)
   rule 3 freezes it at `1.0.0` whether or not anybody promised it.
2. **Declare every core payload as an exported type in `sdk/event`.** All eight
   exist already as unexported structs, so all eight are a move. Six are in
   `internal/app/deviceevents.go`: `arrival`, `departure`, `presence`,
   `linkReading`, `recordChange` and `sample`. `transition` is at
   `internal/app/plane.go:134` and `continuity` at
   `internal/transport/http/httpapi/events.go:109`. Leaving them in
   `internal/app` and hand-writing a schema beside them lost. That is two
   declarations of one shape, and the drift above is what two declarations
   produce.
3. **Generate a JSON Schema per core type from its exported payload type.**
   Write it into that type's entry in `docs/vocabularies/event-types.json`.
   Move `VocabularyFormat` to `1.1` and leave `VocabularyVersion` at `1.4.0`,
   which is ADR-20260827-qvk rule 2. A directory of schema files beside the artefact
   lost. It is a second artefact with a second gate and a second accessor, and
   [ADR-20260825-bhe](20260825-bhe-every-vocabulary-is-a-gated-published-resource.md) rule 11
   refuses two readers of one set.
4. **Derive each schema's `$id` from the type name.** The base URI is
   `https://prusa-connect.org/schemas/events/`, and `sdk/fault.TypeBaseURI`
   already names a published thing that way. A version inside the `$id` lost.
   ADR-20260827-qvk rule 3 lets a payload gain an optional member and never lose one.
   One id therefore holds for the life of the major, and the version a consumer
   needs is already on the entry.
5. **Let the hub stamp `dataschema` from the vocabulary.** Refuse a publisher
   that sets its own on a core type. `DataSchema` is declared at
   `sdk/event/envelope.go:92` and copied from the publisher at
   `internal/transport/stream/hub.go:661`. No publisher in this tree sets it.
   Leaving it to the publisher lost, because that is a published fact minted at
   eight call sites. `ValidateType` already holds the type name from the same
   table. A type outside `com.prusa.connect.` keeps whatever its publisher
   passed.
6. **Spell a bare device id `device_id` on every event payload.** ADR-20260826-c77 rule
   1 spells a bare id as the entity word plus `_id`. Its rule 26 leaves the
   entity word to ADR-20260826-5yj, whose rule 1 gives an object's identity the word
   `device`. `printer_id` lost on that reading. Four payloads carry `printer`
   today, and `recordChange` is already at `device_id`.
7. **Give every exported payload field an explicit snake_case json tag.** That
   is ADR-20260826-c77 rule 19. It binds these types the moment rule 2 moves them into
   `sdk/`. An inferred name lost there and loses here for the same reason. Go's
   default is the field name, so a payload member would move by renaming a Go
   identifier.
8. **Carry one payload per type on every surface that delivers the event.** A
   surface renders the `data` member whole and renames nothing in it. A typed
   fact per surface lost, and `sdk/eventsapi/provider.go:56` is what it costs.
   It declares five facts, so `forwardToSink` at `internal/app/sinks.go:340`
   drops seven of the eight core types. MQTT is exempt, because ADR-20260826-c77 rule 20
   freezes its topics.
9. **Generate a core type's published example from its payload type.** A
   hand-written example lost because no gate can compare prose to a struct.
   That is ADR-20260825-bhe's own reason for generating the enumerable half. The two
   wrong examples above are the measurement behind this rule.
10. **Take the five printer renames under ADR-20260826-5yj step 5.** Move each `$id`
    with them, and add no schedule and no window here. Holding the schemas back
    until the rename lands also lost. The `$id` is derived by the generator from
    the type name, so the rename moves it at no cost. Waiting would park the
    payload half behind the event-type baseline.

## what the rename costs

ADR-20260826-5yj rule 21 and step 5 of its order already decide the rename and the
split. This record only prices it and orders the
schemas around it.

The compiler catches the Go half and nothing catches the wire half. Counted over
tracked files on 2026-08-28 with `git grep -o`:

| type | tracked files | occurrences |
|---|---|---|
| `com.prusa.connect.printer.telemetry.sampled` | 20 | 30 |
| `com.prusa.connect.printer.arrived` | 11 | 22 |
| `com.prusa.connect.printer.presence.changed` | 14 | 21 |
| `com.prusa.connect.printer.departed` | 8 | 10 |
| `com.prusa.connect.printer.link.changed` | 8 | 10 |
| the five together | 27 | 93 |

The five Go constants add 73 occurrences in 22 files, and the compiler reports
every one of them.

Three test packages catch a hand rename of the string. Changing
`TypePrinterArrived` to `com.prusa.connect.device.arrived` and running the
gates on 2026-08-28 gave `go build ./...` success and three red cases:
`TestTheCommittedArtefactsMatchTheTable` in `cmd/generate-event-types`,
`TestAnEmittedEventTypeIsRefusedWhileTheShippedSinksStillRegister` in
`cmd/generate-extensions`, and `TestThePageRoutesOnlyOnDeclaredEventTypes` in
`internal/transport/devtools`. The file was restored.

What no gate catches is a subscriber. That is the event-type baseline
[ADR-20260825-zkq](20260825-zkq-api-compatibility.md) owes, and ADR-20260826-5yj rule 21 makes it a
precondition of the rename rather than a follow-up.

## enforcement

| rule | gate | state |
|---|---|---|
| 1 | `make event-types-check`, extended to refuse an entry carrying no schema | owed |
| 2 | an arch test that no core payload type is declared outside `sdk/event` | owed |
| 3 | `make event-types-check` both directions over the schema member | owed |
| 3 | `make vocab-check`, ADR-20260827-qvk rule 2 on the format version | exists |
| 4 | the generator, which derives the `$id` rather than accepting one | owed |
| 5 | `Publication.validate` refusing a `DataSchema` on a core type | owed |
| 6 | a test over the generated schemas that a bare device id is `device_id` | owed |
| 7 | the reflection test over `sdk/` payload types ADR-20260826-c77 rule 19 owes | owed |
| 8 | none. No gate compares two surfaces' renderings of one payload | none |
| 9 | a test that each published example decodes into its payload type | owed |
| 10 | `make api-check`, the event-type baseline, and ADR-20260826-5yj's own order | the baseline is owed |

Every gate here reads the committed artefact or the exported Go type, on
ADR-20260827-qvk's practice. Rule 8 has no honest gate, because whether two surfaces
render one fact the same way is a judgement over two shapes.

## rejected

### writing the schema first and generating Go from it

This is what CDEvents does, and it is the shape a specification project reaches
for. A JSON Schema per type is the source, and the Go struct is generated from
it, so the published document cannot lag the code.

It loses on three counts here. ADR-20260825-bhe rule 4 makes the Go declaration the
source wherever the Go declaration is the source, and what is generated from it
is the artefact. The server marshals from the struct. A schema not derived from
that struct can disagree with the bytes on the wire, which is the defect this
record exists to close. And a schema-to-Go generator is a build dependency,
while `sdk/go.mod` requires nothing at all.

Take this if a payload ever has to be implemented by a peer that is not this
server. Federation is the case that would ask for it.

| option | why not |
|---|---|
| document the payload in prose on the published page | prose is what the page carries today, and it is why no consumer can generate a decoder |
| a schema directory under `docs/vocabularies/` | a second artefact, a second gate and a second accessor, which ADR-20260825-bhe rule 11 refuses |
| set `dataschema` to a URL this deployment serves | a self-hosted server has no canonical host, and a federated peer cannot fetch the sender's |
| put the vocabulary version in the `$id` | ADR-20260827-qvk rule 3 lets a payload only gain optional members, so one id holds for the major |
| keep the payload structs in `internal/app` and reflect over them from `cmd/` | `sdk/` consumers still could not unmarshal into the type the server marshalled from |
| take a CloudEvents SDK for its schema support | an earlier design decision already refused it: it pulls zap and json-iterator into a binary with neither |
| open the core namespace so a module declares a type and a schema | ADR-20260825-bhe rule 13 closes it, and an unregistered name end to end is what it exists to stop |
| a second event contract for scripts | one standardized set covers every reader, which is the answer earlier research is written under |
| rename the five printer types after 1.0.0 | ADR-20260827-qvk rule 3 refuses a retirement from a published set at or above the floor |
| decide the type filter on `GET /api/v1/events` here | that is the subscription surface, not the payload declaration. The old backlog carries it |

## open items

- `linkReading` embeds `httpapi.DeviceLink`, and `sdk/` may not import
  `internal/`. So rule 2 moves `DeviceLink` into `sdk/event` as well, which is
  14 non-test call sites and one declaration at
  `internal/transport/http/httpapi/printers.go:219`. Whether an alias keeps the
  OpenAPI schema name was not run, so `make api-check` may report a diff
- `sample.Value` is a `json.RawMessage` carrying the number the printer encoded.
  Its schema can say no more than "any value", and re-encoding it to say more
  would change it
- `stream.continuity.lost` is never handed to `Hub.Publish`. It is synthesized
  per connection at `internal/transport/http/httpapi/events.go:473`, so rule 5
  cannot stamp its `dataschema` from the hub. Whether it belongs in the hub's
  event table at all is an open row on
  the old backlog
- Rules 2, 6 and 10 all move `internal/app/deviceevents.go`, and the member
  rename has no gate either. ADR-20260826-5yj step 5 already bundles the member with the
  type, and rule 2's move is the third thing in that change
- `ClassCommand` and `ClassAudit` carry no type, so two of the four classes get
  no schema. The old backlog holds that as an open question
- Nothing measures how many outside consumers read a payload today. The examples
  were wrong for long enough to prove nobody checked them against the wire
