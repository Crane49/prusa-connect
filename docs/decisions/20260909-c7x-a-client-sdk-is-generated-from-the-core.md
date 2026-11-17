---
title: "ADR-20260909-c7x: a client SDK is generated from the core"
description: "One TypeScript package, @prusa/connect, generated from the committed OpenAPI document and registries, with no runtime dependency, versioned with the core and published only by the release workflow."
date: 2026-09-09
status: accepted
tags:
  - adr
  - api
  - sdk
  - typescript
  - modules
  - release
---
# ADR-20260909-c7x: a client SDK is generated from the core

- status: accepted
- date: 2026-09-09

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) · Builds on [ADR-20260825-zkq](20260825-zkq-api-compatibility.md), [ADR-20260826-53y](20260826-53y-documentation-tiers-and-release-notes.md), [ADR-20260828-rp4](20260828-rp4-a-published-event-type-declares-its-payload.md)

A page, a script or a service builds against the core's HTTP API and event
stream. Today the frontend generates types from the committed OpenAPI
document (`frontend/package.json:17`) and hand-writes its client over a
third-party fetch package (`frontend/src/clients/core-client.ts:24`). One gate
holds those types to the document (`Makefile:1412`), and nothing publishes a
package.

## rules

1. The package is `@prusa/connect` on npm. Its publisher is this
   repository's release workflow and nobody else.
2. It is one framework-agnostic ESM package with an `exports` map and shipped
   `.d.ts` files. It runs in browsers, Node, Deno and Bun with no polyfill and
   no framework glue.
3. It declares no runtime dependency and no lifecycle script.
4. The types file is generated from the committed
   `internal/transport/http/httpapi/openapi.json` by the pinned
   `openapi-typescript`. The operation functions and the event, error and
   vocabulary unions are generated from that document and the committed
   registries by `cmd/generate-client-sdk`, which carries a `-check` flag
   (`cmd/generate-event-types/main.go:52`).
5. Generated output is committed, and `generate-client-sdk -check` fails the
   unit lane when it drifts.
6. The package version is the core's `VERSION`, and one release cuts both. A
   change `tools/apicompat` reports as breaking (`Makefile:1283`) is a breaking
   package change, and the `[sdk]` changelog marker of ADR-20260826-53y rule 17 covers
   this package too.
7. A client authenticates with a token the core mints, or with a token
   provider. In a browser the PKCE flow is one navigation to
   `/api/v1/auth/login` (`internal/transport/http/httpapi/auth.go:75`), never
   client code.
8. The event stream is read over `fetch`, never `EventSource`. The reader
   resumes from the server's cursor
   (`internal/transport/http/httpapi/events.go:352`) and surfaces
   `continuity.lost` and `auth.expired` (`events.go:59`) as typed events.
9. A module declares an OpenAPI fragment for its routes, relative to its mount
   under `/api/v1/modules` (`internal/transport/http/httpapi/modules.go:22`),
   as part of its contract. The core lists enabled modules with their
   fragments, and a module author runs `generate-client-sdk` on the fragment.
   A module with routes and no fragment refuses to mount.
10. The GraphQL surface exists in an instance only while the GraphQL module is
    on. The core package carries no GraphQL client.
11. Publication runs only from the release workflow, on the release tag, with
    npm provenance through trusted publishing. No long-lived token exists, so a
    laptop cannot publish.
12. A signed-in person mints an API credential for themselves with no grant,
    at `POST /api/v1/auth/credentials`. One for another person or for a
    service account takes `configure_resource` on the `auth` resource kind, the
    grant that already administers identity
    (`internal/transport/http/httpapi/credentials.go:266`). The credential is
    stored as a hash, shown once, and revoked at
    `DELETE /api/v1/auth/credentials/{credential}`.
13. A module declares what it depends on, by the unique identifier of the
    thing it needs and by feature. The host refuses to run a module without
    them, in the shape ADR-20260910-74m gives it.

## enforcement

| rule | gate | state |
|---|---|---|
| 3 | the `package.json` case in `packages/typescript/test/coverage.test.ts`, run by `make client-sdk-check` | held |
| 4, 5 | `TestTheCommittedPackageMatchesTheContracts` in `cmd/generate-client-sdk/main_test.go` under `make test-unit` | held |
| 4, 5 | `make client-sdk-check` in the `Web / Client SDK and library` job of `.github/workflows/ci.yml` | held |
| 9 | the descriptor test refuses a module with routes and no fragment | owed |
| 11 | the workflow job carries `id-token: write` and `--provenance` | owed |
| 12 | the permit and refuse cases in `internal/transport/http/httpapi/credentials_test.go` | held |
| 12 | the lane in `test/e2e/credentials_test.go`, under `make test-e2e` | held |

## rejected

| option | why not |
|---|---|
| `openapi-fetch` as the runtime | one more package on the path, and the path is the risk |
| a version of the package's own | the number would not say which core it talks to |
| a client that fetches a module's schema at runtime | no compile-time types |
| `EventSource` | it sets no header and cannot read an HTTP status |

## consequences

- the frontend replaces its three core client files with the package
- a 14th generator joins `cmd/`, and the release workflow gains a Node job
- the core gains a stream that accepts an API credential
  (`internal/platform/auth/kind.go:65` reverses) and a CORS allow-list. The
  minting operation is rule 12
- the descriptor (`sdk/extension/descriptor.go:331`) gains a fragment field

## open items

- the licence is OCL v1, the repository's own. The package ships a copy of
  it, and the frontend's MIT line is corrected
- the source lives in `packages/typescript` in this repository. A repository
  of its own is the fallback if that becomes hard
- the fragment's shape on the descriptor
- event payloads stay `unknown` until ADR-20260828-rp4's schemas land
