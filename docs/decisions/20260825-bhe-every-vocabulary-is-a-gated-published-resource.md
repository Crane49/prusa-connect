---
title: "ADR-20260825-bhe: every vocabulary is a gated, published resource"
description: "A closed set the code depends on is enforced before shipping, generated into documentation, and served by the API. Not one of the three, all three."
date: 2026-08-25
status: accepted
tags:
  - adr
  - documentation
  - extensibility
---
# ADR-20260825-bhe: every vocabulary is a gated, published resource

- status: accepted, with the amendments earlier research named
- date: 2026-08-25

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Builds on [ADR-20260825-rre](20260825-rre-one-registration-rule-and-one-descriptor.md) | Rule 7's rendering is [ADR-20260826-swm](20260826-swm-a-generated-reference-is-read-by-a-person.md)

> [!NOTE]
> **Accepted after measurement, and the memory concern was aimed at the wrong
> half.** Descriptor prose is in `.rodata`, is file-backed and demand-paged,
> and costs zero heap and nothing measurable in resident set. It is `init()`
> **registration** of the same data that costs: measured at +1.84 MB heap and
> +5.16 MB RSS for 20,000 entries. All four schemes for keeping prose out of the
> binary were built and measured and every one is worse than doing nothing: the
> compressed blob and the sidecar file cost 393 to 524 KB of binary and 107 to
> 116 KB of heap when read, to save 82 KB that was never resident. This whole
> repository's vocabulary prose is 8,899 bytes, which is 0.29 % of the tightest
> binary margin.
>

A **vocabulary** is a closed set this codebase's behaviour depends on: the error
codes, the extension points and what is registered on them, the authorization
actions and object types, the capability verbs and their argument descriptors,
the resource kinds, the event types, the config keys.

Three of them are gated and published today and five are neither. **None is
reachable by API.** That inconsistency is what makes documentation
untrustworthy. A reader cannot tell which page is generated from the code and
which is prose somebody wrote once, so they distrust all of it equally.

## rules

1. A vocabulary is enumerable from code, by an exported function returning the
   closed set in a stable order.
2. Every entry carries a descriptor: a summary, a longer description, and the
   version of the vocabulary it belongs to. `sdk/fault.Doc` and
   `sdk/extension.Doc` are the shapes to follow. The prose is free: it lands in
   `.rodata`, is demand-paged, and never reaches the heap.
3. A vocabulary carries a version of its own, so a client, an extension or a
   federated peer can say which set it is speaking.
4. **A vocabulary is a static table, not a map assembled by `init()`.** This is
   the rule the measurement changed. A static table costs `.rodata` and nothing
   else; the same entries registered at start-up cost heap, objects and
   resident set, and add `.text` and `.gopclntab` on top. Registration is for
   what only a plugin can supply. Where the table has an upstream source it is
   generated from it; where the Go declaration **is** the source, the table is
   a composite literal the compiler checks and what is generated from it is the
   artefact. Generating Go from the same Go is a step that proves nothing.
   - **Amended in part by [ADR-20260901-b2z](20260901-b2z-driver-declared-permissions.md) rule
     24.** The generated half of the verb table
     stays a static Go table, reviewed in a pull request. The per-build half is
     gathered once at start from the registrations the binary itself carries,
     which is the shape `sdk/eventsapi` registration already has. It is never
     read from configuration or from a running process. ADR-20260831-nbj rule 15 and
     ADR-20260828-xdp rule 8 conceded the split; that record writes it down.
5. **Enforcement happens before shipping, not at start-up.** A duplicate, a
   missing descriptor, an entry present in the code and absent from the
   published artefact, or an artefact naming an entry nothing generates, all
   fail a gate that runs in CI. A process that only discovers this when it
   starts has already shipped.
6. Prefer enforcement the compiler performs, and **do not overstate what it
   reaches**. Go's type system cannot make a string constant a member of a set:
   an unexported type with an exported accessor stops a caller minting one, and
   nothing stops a caller passing a value that type happens to hold. The gate is
   the enforcement; the compiler narrows who can construct.
7. A vocabulary generates a published reference, and that artefact is committed
   so a drift arrives in the pull request that caused it. An artefact that is
   not in the tree is only checked when somebody reruns the generator.
8. The gate runs both directions: an entry with no artefact row fails, and an
   artefact row naming no entry fails.
9. The gate is a test, not only a `make` target, because CI runs `go test` and
   a target somebody has to remember is not a gate.
10. A vocabulary is served by the API, under `/api/v1/`, so an operator, a UI or
   a plugin author reads what this build actually carries rather than what the
   documentation of some version says.
11. The served answer and the generated document come from the same accessor.
    Two readers of one set is the shape that drifts.
12. A new vocabulary ships with its descriptor, its gate, its page and its
    endpoint. Not after them.
13. A vocabulary whose names travel on the wire closes the namespace this
    project owns and leaves every other namespace alone. A value under
    `com.prusa.connect.` that nothing declares is refused; a value under
    somebody else's prefix is checked for syntax and carried. The alternative
    is either an open core set or a rule a plugin cannot satisfy.
14. Config keys are exempt. The struct field, its tag and its doc comment are
    already the descriptor, and the enforcement a config file needs is a
    decoder that refuses what it does not know. A parallel registry means every
    key is declared twice.

## enforcement

| vocabulary | gate | page | API |
|---|---|---|---|
| error codes, `sdk/fault` | `make error-codes-check`, four gates | `error-codes.md` | `GET /api/v1/vocabularies/error-codes` |
| extensions, `sdk/extension` | `make extensions-check`, both directions | `extensions.md` | `GET /api/v1/vocabularies/extensions` |
| capability verbs, `sdk/connectorapi` | `make capabilities-check` | `capabilities.md` | `GET /api/v1/vocabularies/capabilities` |
| resource kinds, `internal/kernel/ref` | `make resource-kinds-check`, both directions | `resource-kinds.md` | `GET /api/v1/vocabularies/resource-kinds` |
| authorization actions, types, relations and shapes, `internal/authz` | `make authz-vocabulary-check`, both directions | `authorization-vocabulary.md` | `GET /api/v1/vocabularies/authorization` |
| event types, `sdk/event` | `make event-types-check`, both directions, plus the hub's refusal | `event-types.md` | `GET /api/v1/vocabularies/event-types` |
| device issues, `internal/kernel/deviceissue` | `make device-issues-check`, both directions, plus the plane's own gate | `device-issues.md` | `GET /api/v1/vocabularies/device-issues` |
| device states, `internal/kernel/devicestate` | none, and none can be written. See the row below | none | `GET /api/v1/vocabularies/device-states` |
| config keys | **exempt**, rule 14 | `config.md`, hand-written | not owed |

`GET /api/v1/vocabularies` is the index over the eight, and seven of them are
public: those entries are identical in every deployment of a build, name no
principal, grant or instance, and are read by clients and authors who do not
hold a credential yet.

The eighth is the exception on both counts. A declared device state set is what
an adapter placed at this install, so the answer is per-install and it takes an
identity. It is the per-install half of
[ADR-20260828-xdp](20260828-xdp-a-contributed-resource-kind-is-a-grantable-object.md) rule 8's
split, required by
[ADR-20260831-c7b](20260831-c7b-an-adapter-declares-the-states-and-the-permissions-and-the-host-places-them.md)
rule 9, and there is no generated half to gate.

Rule 3 is met for resource kinds, the authorization vocabulary and event types,
which carry a content version and a format version. Error codes, extensions and
capability verbs carry none, so the index publishes an empty version for them
rather than inventing one beside the set. Device states carry a format version
and no content version, because a content version would name a set this build
does not carry ([ADR-20260827-qvk](20260827-qvk-a-published-version-is-refused-or-it-is-not-published.md)
rule 1).

## rejected

| option | why not |
|---|---|
| enforce at start-up instead of in CI | the binary is already built and already shipped. A duplicate registration discovered by a customer's server is a defect that a gate would have caught before it left |
| generate the page and skip the endpoint | a page describes the version it was generated from. An operator debugging a running server needs what that build carries, and those differ the moment anything is behind a build tag |
| serve the endpoint and skip the page | an author choosing whether to write an extension is not running the server yet |
| one generator for every vocabulary | they have genuinely different shapes, and `sdk/fault` was already converged into `sdk/extension` once and correctly refused. Share the gate's shape, not its code |
| let a plugin extend the authorization actions | an action set a plugin can widen is a security surface a plugin controls. Closed is the default, and an extension adding an object type is the open question. See [why this row still holds](#why-the-plugin-action-row-still-holds) |
| hand-write the page and gate it against the code | the gate then diffs prose against a set, which no gate can do. Generate the enumerable part and hand-write the argument around it |
| keep descriptor prose out of the binary | measured, and every scheme is worse than doing nothing. A build tag, a gzip blob, a sidecar file and a separate binary were all built: the blob and the sidecar **cost** 393 to 524 KB of binary and 107 to 116 KB of heap when read, to save 82 KB that was never resident, because `.rodata` is demand-paged |
| apply the rule to config keys | the struct tag is already the descriptor and the decoder refusing unknown keys is already the enforcement. A second declaration beside it is the duplicated-invariant shape |

### Why the plugin action row still holds

[ADR-20260827-rqc](20260827-rqc-an-extension-declares-a-permission.md) is accepted and its rule
1 gives an extension's declared permission to whoever administers the object.
That seems to contradict the row above. It does not.

The row refuses a plugin adding to **the core's** action set. ADR-20260827-rqc rule 14
refuses exactly that: a declaration naming `device` or a core resource kind is
refused, so no core object gains an action and no core action moves. What
ADR-20260827-rqc opens is a permission on an object only the declaring extension
contributes, `resource_kind:<namespace>/<name>`, which is a set the core's own
code never reads.

Before rule 14 the two really were one thing, because `Type` is closed and every
declaration landed on a core object. Rule 14 separates them, so the row stays
rejected and needs no amendment.

## open items

- Rules 4 and 5 have no reading for a per-install half, and one is now served.
  A declared state set is no static table, and no gate can read it before
  shipping. Its values arrive when an operator installs an adapter. The package
  `internal/kernel/devicestate` holds the shape and no value of it. So the only
  table this build carries meets rule 4, and the values that would break it are
  never in the build. ADR-20260827-rqc's open items record the same standing conflict
  for the declared action region. It is an amendment an agent may not take.
- Rule 12 is unmet for the same vocabulary. A device-state set ships with its
  descriptor and its endpoint, and with no page and no gate.
  `internal/transport/http/httpapi/vocabularies.go` advertises the reference
  route `/docs/device-states/`, and no page stands behind it. What a page could
  hold is the shape and the refusals, because the values are per-install.
- Rule 3 is unmet for error codes, extensions and capability verbs. None of the
  three carries a content version or a format version of its own, so the
  vocabulary index publishes an empty version for them. All three are named in
  `tools/vocabcompat/baseline.json`, which refuses a fourth
  ([ADR-20260827-qvk](20260827-qvk-a-published-version-is-refused-or-it-is-not-published.md)
  rule 4).
- Rule 3 had no gate for the four vocabularies that do carry a version.
  `fa8f5cc3` moved the authorization set from 20 published entries to 22 and
  left the version alone. `make vocab-check` refuses that under ADR-20260827-qvk rule 1.
- `sdk/fault` fills its registry through `init()`, which rule 4 now refuses for
  a core-owned closed set. It is measured at about 340 bytes of heap and three
  objects per code, so the cost is small and the shape is wrong.
- The core event types are declared in `sdk/event` and spelled again as string
  constants in `internal/app/devices.go`, `internal/app/plane.go` and
  `internal/transport/http/httpapi/events.go`. The hub refuses a mismatch at
  publish time, so a drift fails the publish and the second declaration should
  still go.
- `internal/printer/profile` parses 144 KB of embedded JSON into maps at run
  time and `Builtin()` does not cache, so every caller re-parses and
  re-validates. Measured at +309 KB heap, +1,595 objects and +1.2 MB RSS: the
  largest vocabulary cost in this repository by two orders of magnitude, and it
  is not prose. It is exactly what rule 4 exists to prevent. The capability
  endpoint loads it once, lazily, rather than at start-up.
