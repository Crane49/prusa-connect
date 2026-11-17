---
title: "ADR-20260910-cqq: GraphQL and the old schemas are modules"
description: "GraphQL and the old Prusa Connect shapes are two out-of-process modules, each behind its own enabled flag, each claiming a root prefix through the host, each acting as the person the core resolved. A module serves no HTTP of its own, and may listen on a declared port for a protocol of its own."
date: 2026-09-10
status: accepted
tags:
  - adr
  - api
  - graphql
  - modules
  - security
---
# ADR-20260910-cqq: GraphQL and the old schemas are modules

- status: accepted. The modules must fit what exists
- date: 2026-09-10

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Builds on [ADR-20260827-wmv](20260827-wmv-a-module-is-mounted-and-an-adapter-is-constructed.md), [ADR-20260909-c7x](20260909-c7x-a-client-sdk-is-generated-from-the-core.md), [ADR-20260910-74m](20260910-74m-a-module-declares-what-it-depends-on.md)

Clients written for Prusa Connect speak GraphQL and REST shapes this core
does not serve. This core serves REST from one committed document
(`internal/transport/http/httpapi/openapi.json`) and reaches a module at
`/api/v1/modules/{namespace}/*` (`internal/transport/http/httpapi/modules.go:31`)
with the caller's principal.

## rules

### the two modules

1. GraphQL and the old Prusa Connect shapes are two service modules, out of
   process, never native in the core.
2. Each is its own instance with its own `enabled` flag
   (`internal/app/config/install/install.go:322`), the only switch. The
   GraphQL module is not part of any other module.
   - **Amended by [ADR-20261019-h76](20261019-h76-an-operator-may-switch-installed-modules-off.md) rule 9.** An
     installed instance of either module does not start while
     `core.modules_enabled` is `false`, whatever its flag says.
3. The GraphQL module claims `/graphql` and serves the core's own vocabulary.
   Its schema is generated from the committed OpenAPI document, and a check
   refuses one that drifts.
4. The compatibility module claims the root prefixes old clients call. It
   translates old REST and old GraphQL to the core's operations through one
   mapping table, generated and gated against the core document and the old
   ones.
   - **Withdrawn.** No compatibility module is built. The mapping table, its
     generator and its gate are not in the tree, and rules 9 and 10 wait with
     this one.

### where a module answers

5. A module claims a root prefix through the host, as a `Mount`
   (`internal/transport/http/httpapi/mount.go:30`) resolved to a named
   instance. The host refuses a second claim on one prefix and any claim
   under `/api/` (`mount.go:168`). A module never serves this server's HTTP on
   a listener of its own. It may listen on a port of its own for a protocol of
   its own, on a port its manifest declares. The operator approves those ports
   at install, apart from the permissions, and the page for the module shows
   every port it opened. The Fractal module's `accept.client_listen` and
   `accept.leaf_listen` (`packages/fractal/module.yaml`) are the worked
   example.

### who a module acts as

6. Every `routes.serve` request carries the principal the core resolved
   (`sdk/routesapi/module.go:58`). The module acts as that person and never
   holds a token. Background work runs as the module, inside its declared
   ceiling.
7. The core verifies an old credential through an identity plugin
   (`IdentityProvider`, `sdk/authapi/provider.go:103`); the module never sees
   a secret. The Prusa Account kind checks RS256 against the issuer's cached
   JWKS (`adapters/auth/oidc/token.go:171`, the same lookup). A pairing maps
   the subject to a local principal; an unpaired subject is refused, never
   created.
8. A token travels in the `Authorization` header or the core's cookie, never
   in a query string.

### what an old client gets

9. An old operation with no local equivalent answers a refusal that says so.
   The mapping table lists it as unmapped with a reason, never as a constant.
10. An old error code maps onto the core's fault registry
    (`sdk/fault/registry.go:227`) through the same table. In GraphQL it is a
    `ConnectError` inside `data`, the cloud's shape.

### events

11. No old subscription exists to serve. A client polls, or reads
    `GET /api/v1/events`. `routes.stream` is out of scope until a client
    needs it.

### what a module ships

12. Each module ships its OpenAPI fragment (`sdk/routesapi/openapi.go:46`,
    ADR-20260909-c7x rule 9). The GraphQL module ships its schema beside it.
13. Each declares its dependencies in `Requires`
    (`sdk/extension/descriptor.go:363`) as ADR-20260910-74m shapes them: the fleet
    wire methods it calls (`sdk/deviceapi/wire.go:48`,
    `sdk/busapi/subscribe.go:25`) and the identity plugin of rule 7.
14. Each is released as a static binary per platform, signed by keyless
    Sigstore from the release workflow, and installed by `module add` with
    its checksum pinned. One repository per module. Owner ruling of
    2026-09-10.

## enforcement

| rule | gate | state |
|---|---|---|
| 2 | `enabled` on the declaration, changed by `put-resource` | exists |
| 3 | a generate-and-check for the schema | owed |
| 4, 9, 10 | the table gate: an old field with no row and no reason fails | owed |
| 5 | `validateMounts` (`mount.go:60`) refuses a duplicate and a reserved path | compiled mounts; the claim is owed |
| 5, for a port | none. A manifest has no key for a port | owed |
| 6 | `routes.serve` carries `principal`; no wire method hands out a token | exists |
| 7 | the credential kind's tests against a fixed JWKS | owed |
| 12, 13 | ADR-20260909-c7x's fragment gate; ADR-20260910-74m's gates | owed |
| 14 | the signing step in the release workflow; `module add` verifies | owed |

## consequences

- two binaries in the release, two declarations in `config/config.yaml`, each
  `enabled: true`
- the core gains a root prefix claim, a Prusa Account credential kind and a
  mapping table gate
- no GraphQL runtime enters the core's dependencies

## rejected

| option | why not |
|---|---|
| native GraphQL in the core | a query language in the core, deleted once |
| another module carrying GraphQL | one surface, one module, one switch |
| the module holding a fleet-wide identity | it filters per caller from its instance's whole grant |
| a token in the query string | it lands in access logs and browser history |

## tasks

One agent per task. Module tasks wait on the placement answer below.

| # | task | side | waits on |
|---|---|---|---|
| 1 | the `mount` key, the claim in `resourcehost`, the route in `httpapi` | core | nothing |
| 2 | the Prusa Account credential kind and its pairing | core | nothing |
| 3 | the mapping table, its generator and its gate | core | nothing |
| 4 | `device.jobs` and `device.files` | core | nothing |
| 5 | the GraphQL module, schema from `openapi.json` | module | 1, placement |
| 6 | the compatibility module, GraphQL half | module | 1, 3, placement |
| 7 | the compatibility module, REST half | module | 6 |
| 8 | wire refusals to `ConnectError` in `data` | module | 6 |
| 9 | the release: both binaries, declared and signed | core | 5, 6 |

## open items

- A manifest has no key for a port, so the ports of rule 5 are ordinary
  settings. The Fractal module writes its two addresses under `accept`, and the
  install has nothing to approve. The manifest key, the approval and the
  reading the host shows are owed.

## what is not decided

- where the module source lives. The options:
  - this repository under `modules/`. Cost: a GraphQL runtime in this tree's
    Go module, the core's gates over module code, and rule 14 amended for
    these two
  - one repository per module, as rule 14 reads. Cost: a pinned core version
    per module, and a core schema move found only when that repository
    rebuilds
- whether `jobs` and `queueFile` are served
- who performs the pairing
- the kind a module that sinks nothing is declared under (ADR-20260827-wmv rule 8)
