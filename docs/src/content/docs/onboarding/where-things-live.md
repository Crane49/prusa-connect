---
title: Where things live
description: Every top-level directory, every package under internal/ and sdk/, and the one-line job of each, with the dependency law that decides what may import what.
sidebar:
  order: 7
audience: build
rewrite:
  describes: current
  implementedBy:
    - internal
    - sdk
    - cmd
---

The page to open when the question is "which file handles this". Every path
here exists, and `internal/arch/onboarding_docs_test.go` fails when one stops
existing. A word in a job description that reads as jargon is in the
[glossary](../glossary.md).

## Top level

| path | holds |
|---|---|
| `cmd/` | commands, one directory per binary. Only `main` packages |
| `sdk/` | the separate module a community extension compiles against. Standard library only |
| `internal/` | the application, grouped by role |
| `adapters/` | reference implementations of the `sdk/` contracts, one directory each |
| `docs/` | this site under `docs/src/`, plus the ADRs and the changelog |
| `docs/vocabularies/` | the machine-readable form of every generated closed set. Committed, never hand-edited |
| `config/` | the shipped default configuration and example profiles |
| `docker/` | one directory per service: its definition, its local overrides, its Dockerfile |
| `examples/` | examples that run: four modules, two adapters, a Fractal tree, a compose deployment and a printer settings file |
| `frontend/` | the web app's source. `make embed-app` builds it, compresses it and copies the result where the full build embeds it |
| `scripts/` | the shell gates and the certificate and config generators |
| `test/` | `e2e/` against the built binary, `fleet/` across the model and firmware matrix |
| `tools/` | developer tooling that is not the product: the printer simulator, the camera simulator, the API compatibility gate |
| `.agents/rules/` | the binding rules. Short on purpose |
| `Makefile` | every gate and every build variant |

## The dependency law

Every package under `internal/` belongs to exactly one group, and nothing below
may import upward. `internal/arch/layout_test.go` holds the rule as data
and checks it against the real import graph of every tag set and platform the
build recipes compute.

```
   cmd ---> app ---> { transport, connector, platform } ---> printer ---> kernel
                |
                +---> extension --------------------------------------> kernel
                |
                +---> authz ------------------------------------------> kernel
                                        \
                                         +--> sdk <--+
```

| group | may import |
|---|---|
| `kernel/` | nothing internal |
| `arch/` | nothing internal |
| `printer/`, `authz/` | kernel, sdk |
| `connector/` | kernel, sdk, a domain |
| `transport/` | kernel, sdk, a domain |
| `platform/` | kernel, sdk, a domain |
| `extension/` | kernel, sdk |
| `app/` | everything |

Three rules do most of the work. `transport` and `platform` never import each
other; they meet in `app`, the [composition root](../glossary.md#composition-root).
A domain does not import a sibling domain; two contexts that need each other
meet in `app`, and a concept both own belongs in `kernel`. An import that leaves
`internal/` and comes back counts as an import of wherever it ends up, so
routing a forbidden edge through `adapters/`, `examples/`, `tools/` or `test/`
does not make it legal.

## internal/, package by package

### kernel

Primitives with no business meaning.

| package | job |
|---|---|
| `internal/kernel/ref` | `Ref` and `UID`, the closed set of resource kinds, and their published descriptors |
| `internal/kernel/condition` | the condition type and the derived state a supervised instance reports |
| `internal/kernel/admission` | a device working through a dialect's pairing handshake: the code, the record and the store |
| `internal/kernel/deviceissue` | the closed set of issues the device plane raises, with their descriptors |
| `internal/kernel/devicestate` | the shape of a declared device state set, the registry it is placed in, and the refusals |
| `internal/kernel/hostfeature` | what a build of this host promises a module, as keys in the capability vocabulary |
| `internal/kernel/machinetext` | bounds one piece of text a device chose: a refusal reason, a dialog title, a filename |
| `internal/kernel/sqlstore` | what every SQL store here does the same way, starting with the SQLite journal mode |
| `internal/kernel/version` | the application version, embedded from the VERSION file `make sync-version` writes |

### printer, the domain

| package | job |
|---|---|
| `internal/printer/capability` | resolves the three tiers into one set, wraps a driver session, and checks every command against the descriptor |
| `internal/printer/profile` | the embedded firmware matrix, its validation, and the coverage a driver's profiles bound an unknown device to |
| `internal/printer/simulator` | a printer at the protocol level for every model the profiles cover, driven by `test/fleet` |
| `internal/printer/echodevice` | a fake printer that speaks the echo dialect, so a demo fleet holds a machine that is not a Prusa |

### authz

One package, and one of the three things that never become pluggable.

| file | job |
|---|---|
| `internal/authz/doc.go` | the whole model in prose: types, relations, actions, and why a resource grant is written on the kind |
| `internal/authz/vocabulary.go` | the closed sets and the relation shapes `Reconcile` writes into the schema |
| `internal/authz/tuple.go` | `object#relation@subject`, refusing an illegal edge at construction |
| `internal/authz/check.go` | `Can`, `Require`, `CanResource`, one indexed row lookup each |
| `internal/authz/filter.go` | `ListPrinters`, resolved inside the query and never by filtering rows afterwards |
| `internal/authz/visible.go` | `Visible`, the event stream's routing table, stamped with the epoch read before the rows |
| `internal/authz/grant.go` | reading, writing and withdrawing tuples over the API, bounded by what the caller holds |
| `internal/authz/project.go` | recompiles the denormalized projections inside the writing transaction |
| `internal/authz/bootstrap.go` | the first grant, written once into an empty graph |
| `internal/authz/schema.go` | the graph's migrations, which `internal/app/hostschema` applies with the rest of the host schema |
| `internal/authz/reconcile.go` | writes the vocabulary into the lookup tables and refuses to retire a shape tuples still name |
| `internal/authz/sqlite.go` | opening the SQLite handle |
| `internal/authz/publish.go` | the vocabulary as the generated page and the API endpoint read it |
| `internal/authz/migrations/` | the graph's SQL, one directory per engine |

### connector

| package | job |
|---|---|
| `internal/connector/xbuddy` | the dialect Prusa's xBuddy and Buddy boards speak. Its wire is frozen rather than chosen |

Inside it the files split by concern.

| file | job |
|---|---|
| `internal/connector/xbuddy/xbuddy.go` | the driver, its routes, and the registration handshake |
| `internal/connector/xbuddy/session.go` | one printer's session, which outlives its connection |
| `internal/connector/xbuddy/inbound.go` | the read loop, the classifier and the telemetry mapping |
| `internal/connector/xbuddy/wire.go` | the nine-byte framing |
| `internal/connector/xbuddy/verbs.go` | the 57-verb table, including the 31 that leave as gcode or as `SET_VALUE` |
| `internal/connector/xbuddy/files.go` | the printer's own file tree |
| `internal/connector/xbuddy/rawgcode.go` | the raw gcode path |
| `internal/connector/xbuddy/gcode.go` | the gcode a verb rewrites into |
| `internal/connector/xbuddy/bounds.go` | the limits |
| `internal/connector/xbuddy/registration.go` | the code a printer polls on, and the operator's approval |

### transport

| package | job |
|---|---|
| `internal/transport/http/httpapi` | the sealed router, every operation, the OpenAPI document, the mounts |
| `internal/transport/http/httpfault` | the error codes the HTTP transport returns on its own account |
| `internal/transport/stream` | the event hub: channels, queues, replay windows, subscriptions, encodings |
| `internal/transport/devtools` | the one-page developer view at `/dev` |
| `internal/transport/http/loginguard` | holds a caller in front of a credential check for as long as their past failures say |
| `internal/transport/webrtc` | remuxes a camera's H.264 stream into a WebRTC track a browser plays |

Inside `internal/transport/http/httpapi`, one file per surface.

| file | job |
|---|---|
| `internal/transport/http/httpapi/server.go` | builds the sealed router and refuses one whose routes and document disagree |
| `internal/transport/http/httpapi/routes.go` | the only place an operation is mounted |
| `internal/transport/http/httpapi/mount.go` | guards what is mounted beside the operations |
| `internal/transport/http/httpapi/middleware.go` | the stack every request passes through, and the principal resolution |
| `internal/transport/http/httpapi/problem.go` | the RFC 9457 rendering and the trace id |
| `internal/transport/http/httpapi/devices.go` | the printer listing, one printer, one command |
| `internal/transport/http/httpapi/devicefiles.go` | the printer's own file tree and its storages |
| `internal/transport/http/httpapi/printergcode.go` | the raw gcode surface |
| `internal/transport/http/httpapi/resources.go` | the configured inventory as this surface reads it |
| `internal/transport/http/httpapi/resourceroutes.go` | the resource operations |
| `internal/transport/http/httpapi/gcodepresets.go` | the stored gcode sequences |
| `internal/transport/http/httpapi/gcodepresetroutes.go` | the preset operations |
| `internal/transport/http/httpapi/grants.go` | reading, writing and withdrawing grants |
| `internal/transport/http/httpapi/events.go` | the server-sent event stream |
| `internal/transport/http/httpapi/auth.go` | the login, callback and logout ceremony |
| `internal/transport/http/httpapi/readiness.go` | the readiness operation |
| `internal/transport/http/httpapi/vocabularies.go` | the nine vocabularies and their index |
| `internal/transport/http/httpapi/devtools.go` | the operations the developer view reads |
| `internal/transport/http/httpapi/openapi.json` | the committed document the route gate diffs against |

Inside `internal/transport/stream`.

| file | job |
|---|---|
| `internal/transport/stream/hub.go` | publish, subscribe, replay, and the bounds |
| `internal/transport/stream/channel.go` | one origin: its subscribers and its id minting |
| `internal/transport/stream/history.go` | one origin's replay window |
| `internal/transport/stream/queue.go` | the conflating queue and the durable queue |
| `internal/transport/stream/subscription.go` | one reader's pump and its live re-authorization |
| `internal/transport/stream/encoding.go` | the one marshal every subscriber shares |

### platform

| package | job |
|---|---|
| `internal/platform/auth` | credentials, tokens, the verifier, and the in-memory store |
| `internal/platform/auth/local` | the credential store a deployment with no identity provider logs in against |
| `internal/platform/auth/dbstore` | those people and credentials in the database the host already opened |
| `internal/platform/devicestore` | the device records, the prints and the registrations, in the same database |
| `internal/platform/auth/oidc` | discovery, PKCE, the token exchange and JWT verification |
| `internal/platform/auth/session` | the login ceremony, the cookie, and resolving a request to an id |
| `internal/platform/observability` | the metric catalogue, handles and buckets |
| `internal/platform/observability/prom` | the Prometheus registry, behind a build tag |
| `internal/platform/observability/health` | the per-connector readiness rollup |

### extension

| package | job |
|---|---|
| `internal/extension/registry` | compile-time registration of resource types, tier 1 of the three extension tiers |
| `internal/extension/supervisor` | one wrapper per running instance: queue, breaker, timeout, panic recovery, state machine |
| `internal/extension/supervised` | typed views of a supervised instance, one per contract |
| `internal/extension/host` | the handle an instance is constructed with |
| `internal/extension/bus` | lets a supervised extension publish an event on this server's event stream |
| `internal/extension/script` | compiles and evaluates a script sink's expression |
| `internal/extension/wirehost` | launches a module's child process and speaks the wire protocol to it |

### app, the composition root

| file | job |
|---|---|
| `internal/app/app.go` | `Run` and `serve`: the startup and shutdown order, and the graph bootstrap |
| `internal/app/options.go` | what the command line supplies and how it lays over the file |
| `internal/app/logging.go` | the one logger every part is handed |
| `internal/app/hostschema` | the host database's schema: every owner's migrations in order, and the compatibility floor |
| `internal/app/listen.go` | binds one or two listeners, serves them, drains them |
| `internal/app/apitls.go` | the TLS profile a browser is served, and its expiry warning |
| `internal/app/plane.go` | starts the supervision plane and puts each status change on the bus |
| `internal/app/connectors.go` | builds every inbound driver and decides where its routes go |
| `internal/app/devices.go` | the device plane: admit, register, hold, command, retire |
| `internal/app/deviceevents.go` | the payloads a printer's events carry |
| `internal/app/devicepresence.go` | one printer going away and coming back, rate bounded |
| `internal/app/stream.go` | the authorization graph as the event hub reads it |
| `internal/app/sinks.go` | a declared sink attached to the bus as a principal of its own type |
| `internal/app/auth.go` | which identity implementation is wired, and the graph subject prefix |
| `internal/app/principal.go` | the one-subject resolver for a deployment with no issuer |
| `internal/app/readiness.go` | the ready verdict, served and logged |

| package | job |
|---|---|
| `internal/app/config` | the `core:` block, its defaults, its validation, and TLS loading |
| `internal/app/config/build` | maps `resources:` onto build tags through `adapters/tags.json` |
| `internal/app/config/install` | reads `enabled` and `runtime`, and refuses a runtime a kind cannot offer |
| `internal/app/resource` | the two-input configured inventory, its merge, its writes and its reconcile plans |
| `internal/app/resourcehost` | carries a plan out by driving the supervision plane |
| `internal/app/builtin` | the resource types this binary registers, and the adapters it adopts |
| `internal/kernel/atomicfile` | write to a temporary name, fsync, rename, fsync the directory |
| `internal/app/devicekinds` | turns the device-kind declarations in this binary into the build's half of the verb table |
| `internal/app/scaffold` | writes a new module from a skeleton, so an author starts from something that builds |

### arch and docsref

| package | job |
|---|---|
| `internal/arch` | the dependency law and the rules for `sdk/` and `adapters/` as data, and the tests that enforce them |
| `internal/docsref` | the one function that derives a published anchor, shared by every generator |
| `internal/codegen` | the committed-artefact protocol every generator command shares |
| `internal/printfile` | reads the preview a slicer wrote into a print file: the picture and the few numbers beside it |

`internal/arch/recipes.go` is the only importable thing in that package: what
the project compiles, read out of the recipes that compute it, because the
error-code gate has to answer the same question.

## sdk/, package by package

| package | job |
|---|---|
| `sdk/extension` | the registration rule, the namespace derivation, the descriptor, the catalogue |
| `sdk/fault` | the error taxonomy: domains, codes, params, the RFC 9457 payload, the registry |
| `sdk/event` | the envelope, the origin, the cursor, and the closed core event-type set |
| `sdk/connectorapi` | a printer dialect: baseline, direction, session, capability set, storage, files |
| `sdk/deviceapi` | how a module reads the fleet and drives one device |
| `sdk/storageapi` | a storage backend, its confinement rules and its staged-write handles |
| `sdk/transferapi` | the record of a file this host moved from a storage onto a device |
| `sdk/eventsapi` | an event sink and the domain facts it consumes |
| `sdk/historyapi` | a store for the measurements and events the hub has forgotten |
| `sdk/cameraapi` | a camera source and the frames it hands over |
| `sdk/authapi` | an identity provider and the identity it returns |
| `sdk/accessapi` | how a module asks whether one principal may perform one action on one object |
| `sdk/busapi` | how a module tells the rest of the deployment that something happened |
| `sdk/routesapi` | additional HTTP routes and the printer view a plugin reads |
| `sdk/stateapi` | the module state store: keys, values, versions, and the `state.*` methods |
| `sdk/scriptapi` | the environment a script sink is evaluated in |
| `sdk/manifest` | what a module declares about itself, and the reader that validates it |
| `sdk/linkapi` | the link to the server above this one |
| `sdk/fractal` | the Fractal protocol: its messages, its state tree and its rules |
| `sdk/internal` | what the sdk packages share and nothing outside the module may import |
| `sdk/wire` | the frame a module's process and this host exchange, and the handshake that opens it |
| `sdk/wirerpc` | the JSON-RPC codec, the credit balance and the session over that frame |
| `sdk/vocabulary` | the deprecation marker every published closed set uses |

A package with a `test` subdirectory publishes a conformance suite:
`sdk/connectorapi/connectorapitest`, `sdk/storageapi/storageapitest`,
`sdk/eventsapi/eventsapitest`, `sdk/authapi/authapitest`,
`sdk/cameraapi/cameraapitest`, `sdk/fault/faulttest`,
`sdk/fractal/fractaltest` and `sdk/wire/wiretest`.

`sdk/routesapi` and `sdk/stateapi` are the two halves of a module.
`internal/app/resourcehost/modules.go` hosts both. It mounts a module's routes
under the namespace the module declares, and hands it a state store scoped to
that same namespace. `sdk/scriptapi` is the script tier's environment, hosted by
`internal/extension/script`.

`sdk/routesapi` is the module route contract: a module answers `routes.serve`
over the wire and the host mounts it under the module's namespace.

## cmd/

| command | job |
|---|---|
| `cmd/prusa-connect` | the binary. It hands every argument to `compose`, and `core` is accepted rather than required |
| `cmd/demo` | the server and a simulated fleet in one process, which `make demo` runs |
| `cmd/compose-build` | builds a binary from a build file, pulling in the adapters it names |
| `cmd/detect-build-tags` | reads `resources:` and the top-level blocks, and emits the build tags |
| `cmd/detect-build-feature` | answers whether one feature is on, for the Makefile's conditional steps |
| `cmd/fleetload` | drives a large simulated fleet at a running server |
| `cmd/printer-listener` | the printer half of the API, for a deployment that serves it on its own |
| `cmd/generate-error-codes` | the error reference, from the fault registry |
| `cmd/generate-extensions` | the extension reference, from the registries |
| `cmd/generate-capabilities` | the capability vocabulary, from the shipped profiles |
| `cmd/generate-resource-kinds` | the resource-kind reference, from `internal/kernel/ref` |
| `cmd/generate-authz-vocabulary` | the authorization reference, from `internal/authz` |
| `cmd/generate-event-types` | the event-type reference, from `sdk/event` |
| `cmd/generate-device-issues` | the device-issue reference, from `internal/kernel/deviceissue` |
| `cmd/generate-device-models` | the printer-model reference, from `internal/printer/profile` |
| `cmd/generate-printer-error-codes` | the printer error texts of the interface, from Prusa-Error-Codes |
| `cmd/generate-config-reference` | the configuration reference, from the config types |
| `cmd/generate-sdk-reference` | the sdk reference, from the contract packages |
| `cmd/generate-wire-protocol` | the module wire protocol reference |
| `cmd/generate-client-sdk` | the TypeScript client, from the committed OpenAPI document |
| `cmd/scaffold-driver` | a new driver that compiles and passes the connector contract on the first run |
| `cmd/new-module` | a new module: UI only, process only, or both, chosen with `-kind` |

## Where the tests are

| kind | lives | runs |
|---|---|---|
| unit | beside the code, `*_test.go` | every change |
| external-surface | `package x_test`, exercising a package as a third party does | every change |
| contract, for our own implementations | the same package, driven by the sdk suite | every change |
| contract, for a community implementation | `sdk/<contract>/<contract>test/` | the author runs it |
| the model and firmware matrix | `test/fleet` | `make test-matrix` |
| end to end, against the built binary | `test/e2e` | `make test-e2e` |
| device, against the simulator | `internal/printer/simulator` | every change |
| benchmarks, gated on allocations | beside the code | every change |

The distinction that matters: a conformance suite is published, not internal.
`sdk/storageapi/storageapitest` is run by this project against the built-in
provider and by a community author against theirs. Both pass the same suite or
neither ships.

## The directory with no tracked file

`storage/` holds no tracked file and `.gitignore` excludes it. It is where the
shipped configuration points its one storage instance, so a server started from
this tree writes uploads there. Nothing imports or reads it.
