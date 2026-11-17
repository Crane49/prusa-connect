---
title: Registries and names
description: One registration rule, one descriptor, a namespace derived from the registering module, and the eight closed sets that are gated, generated into pages and served by the API.
sidebar:
  order: 6
audience: build
rewrite:
  describes: current
  implementedBy:
    - sdk/extension
    - internal/extension/registry
    - internal/kernel/ref
---

Two ideas cover everything registered in this codebase. Registration is one
rule with one descriptor, and a closed set is enforced, published and served,
all three at once. This page shows both in use.

## The registration rule

```
  package init                     the host builds
  ------------                     ---------------
  eventsapi.Register(...)   -->    Set gathers          -->  Drain
  storageapi.Register(...)         entries into a            seals the list
  registry.Register(...)           package-level Set
                                                        -->  NewRegistry
                                                             immutable table
                                                             shared with no lock

  a registration after Drain      ->  ErrSealed, not a silent no-op
  a second entry on one name      ->  ErrDuplicate, naming both modules
```

A registry gathers declarations at package initialisation, which is Go's
guarantee that every `init` function in a linked package runs before `main`
does. The host drains the list once, when it builds
(`sdk/extension/set.go:92`), and from that moment it is
[sealed](../glossary.md#sealing). A qualified name may be taken once, and a
second registration is
refused rather than replacing the first, which is how a third-party plugin
would otherwise take over a built-in. Both refusals name the entry and the
module each registrant came from (`sdk/extension/registry.go:53`).

The registry is immutable once built, so it is safe to share and there is
nothing to lock on a lookup (`sdk/extension/registry.go:32`).

In this application it looks like this:

| step | where |
|---|---|
| a resource type registers from `init` | `internal/app/builtin/builtin.go:19` |
| the composition root reaches it with a blank import | `internal/app/plane.go:16` |
| the two registries are folded into one table | `internal/app/builtin/adapters.go:86` (`Registry()`) |
| the supervision plane resolves a declared type against it | `internal/app/resourcehost` |

`builtin.Registry()` is where the application's own registry and the published
contract registries meet. They are separate by construction: an adapter
compiles against `sdk/` alone, so it registers with `sdk/eventsapi` or
`sdk/authapi` and never with the application's registry. One list per kind,
`adaptedSinks`, `adaptedIdentities`, `adaptedCameras`, `adaptedStorages`,
`adaptedHistories` and `adaptedDatabases`
(`internal/app/builtin/adapters.go:55`), names what this plane lifts out of
those registries. They are inclusion lists rather than everything the
registries hold. A name this plane admits is a name an operator may write, so
it is written down rather than followed from whatever a blank import linked.

## Where a namespace comes from

A name is qualified by a [namespace](../glossary.md#namespace) derived from the
**[module](../glossary.md#module) of the package that registered it**. The
registering code does not pass it and cannot set it.

```
  Register("webhook", ...)
       |
       v
  extension.Declare                      sdk/extension/descriptor.go:361
       |
       | walk the live stack to the first frame outside the fixed list of
       | registration wrappers                sdk/extension/namespace.go:100
       v
  that frame's package
       |
       | look the package up in the build's module graph (debug.ReadBuildInfo)
       v
  the module path
       |
       | reverse the host, put "aux" where the host ends, keep the rest
       v
  the namespace                          sdk/extension/namespace.go:281

  github.com/acme/mk4       ->  com.github.aux.acme.mk4
  the main module of a build ->  core
```

The label `aux` in `com.github.aux.acme.mk4` marks where the host name ends and
the module path begins, and every part of the system depends on it. Go refuses
`aux` as a path element and as the first label of a host, and a module path
carrying it as a label is refused here too, so the namespace can be read back
as the module path it came from. Without it `github.com/acme/mk4` and
`acme.github.com/mk4` would render identically and land in one collision domain.
Every label is a
[DNS-1123](../glossary.md#dns-1123) label, so the value is legal as a
capability key prefix, an error domain and an event type prefix at once.

Only the main module of a build holds `core` (`sdk/extension/namespace.go:12`).
A module path that would render as `core` is refused, whatever `replace`
directive put it in the graph, and a path that is not already a namespace is
refused rather than rewritten to fit. `example.com/a_b`, `example.com/a-b` and
`example.com/A-B` are three refusals rather than one folded namespace.

The composition root checks the derivation against itself when it adopts an
adapter. `adopt` (`internal/app/builtin/adapters.go:406`) calls
`registry.Declare`, which mints a descriptor for the package that calls it, and
compares the qualified name it got back against the one the adapter registered
under. A mismatch is `ErrForeignNamespace`, which turns a silent relabelling
into a refusal that says which module the entry came from.

An operator writes the bare name until two namespaces answer to it, and the
qualified name after that. `extension.IsQualified`
(`sdk/extension/registry.go:24`) is what tells them apart, and an ambiguous
bare name is `ErrAmbiguous` rather than a guess.

## The descriptor

Every entry carries the same fixed attributes, the way the event
[envelope](../glossary.md#envelope) gives every event the same ones.
`sdk/extension/descriptor.go:312`.

| field | mandatory | is |
|---|---|---|
| `Summary` | yes | one sentence for a table row |
| `Description` | yes | the longer form |
| `Version` | yes | the implementation's own version |
| `Contract` | yes | the contract version it compiled against |
| `Stability` | yes | experimental, stable, deprecated |
| `ReplacedBy` | when deprecated | refused otherwise. A deprecation with no answer attached is a dead end |
| `Options` | no | the config keys it accepts |
| `Emits` | no | the events it emits |
| `Requires` | no | what it needs of the host |

A missing mandatory field is a registration error, because an empty string
would ship as documentation. Optional fields are validated too (`Doc.validate`,
`sdk/extension/descriptor.go:473`): a config key with no summary, a required
key that also carries a default, an emitted event with no version, and a
requirement naming none of capability, type and core are all registration
errors. Nothing on the
generated page may be an empty cell.

A [descriptor](../glossary.md#descriptor)'s strings carry no control character and no line break, and no
backtick where the reference publishes them inside a code span (`validateText`,
`sdk/extension/descriptor.go:573`). A line separator or a paragraph separator
ends a line without being a control character, so a value carrying one would
split a published table that `unicode.IsControl` says is intact.

The extension points are a closed set, `sdk/extension/descriptor.go:65`:
connector, storage, identity, event-sink, routes, database, history, camera,
device-kind and link. A name is unique per point and not globally, so storage
`local` and sink `local` are two things.

## The vocabularies

A [vocabulary](../glossary.md#vocabulary) is a closed set this codebase's
behaviour depends on. Each one is
enumerable from code by an exported function returning the set in a stable
order, is gated in CI, is generated into a committed page, and is served by the
API. Not one of the three, all three.

| vocabulary | owned by | gate | page | API |
|---|---|---|---|---|
| error codes | `sdk/fault` | `make error-codes-check` | `/docs/error-codes` | `/api/v1/vocabularies/error-codes` |
| extensions | `sdk/extension` | `make extensions-check` | `/docs/extensions` | `/api/v1/vocabularies/extensions` |
| capability verbs | `sdk/connectorapi` | `make capabilities-check` | `/docs/capabilities` | `/api/v1/vocabularies/capabilities` |
| resource kinds | `internal/kernel/ref` | `make resource-kinds-check` | `/docs/resource-kinds` | `/api/v1/vocabularies/resource-kinds` |
| authorization | `internal/authz` | `make authz-vocabulary-check` | `/docs/authorization-vocabulary` | `/api/v1/vocabularies/authorization` |
| event types | `sdk/event` | `make event-types-check` | `/docs/event-types` | `/api/v1/vocabularies/event-types` |
| device issues | `internal/kernel/deviceissue` | `make device-issues-check` | `/docs/device-issues` | `/api/v1/vocabularies/device-issues` |
| device models | `internal/printer/profile` | `make device-models-check` | `/docs/device-models` | `/api/v1/vocabularies/device-models` |

`GET /api/v1/vocabularies` is the index over them, and it also serves the
device states an adapter declares. Every one of them is
public: the entries are identical in every deployment of a build, name no
principal, grant or instance, and are read by clients and authors who do not
hold a credential yet.

Config keys are exempt. The struct field, its tag and its
doc comment are already the descriptor, and a decoder that refuses what it does
not know is already the enforcement.

### A static table costs no heap

This is the rule a measurement changed. A string constant written into the
source is compiled into the binary's read-only data, which the operating system
maps straight from the executable file and pages in as it is touched. It costs
the process no allocated memory at all. Building the same table at start-up
instead costs heap, live objects and resident memory: measured at +1.84 MB heap
and +5.16 MB resident for 20,000 entries.

So a core-owned vocabulary is a composite literal the compiler checks.
`internal/kernel/ref/vocabulary.go:44` and `sdk/event/vocabulary.go:73` are
both written that way, and each says so in a comment. Registration is for what
only a plugin can supply.

### The gate runs both ways

An entry with no page row fails, and a page row naming no entry fails. That is
what makes the committed page trustworthy: a drift is a diff a reviewer sees
rather than a rebuild nobody runs.

Two more gates sit beside them.
`TestEveryGeneratorThatPublishesAnAnchorAsksDocsrefForIt`
(`internal/arch/docsref_test.go:73`) holds every anchor on the six pages
to the one function that derives it, so a reader holding
`core.printer/GCODE_NOT_ALLOWED` from a 409 response can construct its link
without opening the page. `TestOnlyDocsrefDerivesAPublishedAnchor`
(`internal/arch/docsref_test.go:42`) refuses a second copy of that function
anywhere under `cmd/` or `internal/`, because the way that invariant broke the
first time was six copy-pasted functions rather than six callers.

### The namespace a wire name closes

A vocabulary whose names travel on the wire closes the namespace this project
owns and leaves every other namespace alone. `event.ValidateType`
(`sdk/event/vocabulary.go:213`) refuses a type under `com.prusa.connect.` that
the vocabulary does not carry, and checks a type under anybody else's prefix
for syntax only. `fault.NewDomain` (`sdk/fault/code.go:51`) refuses the `core`
namespace for the same reason from the other side.

The alternative is either an open core set or a rule a plugin cannot satisfy.

## The build tag airlock

A configuration file never names a Go [build tag](../glossary.md#build-tag).
Config is operator input and a build tag is a compiler argument, so
`adapters/tags.json` is [the airlock](../glossary.md#the-airlock) where the two
meet, and the only place they do.

```
   config/config.yaml            adapters/tags.json         go build
   resources:            ---->   kind + type -> tag  ---->  -tags "full ..."
     sink:
       audit:
         type: webhook
```

`cmd/detect-build-tags` reads it and nothing else maps a type or a condition
onto a tag. `internal/arch/airlock_test.go` refuses a registered type with no
entry, an entry naming a type nothing registers, a tag no build recipe passes,
and a config file whose key names a build tag.

Most entries carry an empty tag, and each says why: those adapters implement
their wires on the standard library, so none of them pulls in a third-party
client and all are compiled unconditionally. The notes in `adapters/tags.json`
carry the measured binary cost of each.
