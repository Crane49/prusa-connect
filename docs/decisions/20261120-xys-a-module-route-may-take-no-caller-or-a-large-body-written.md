---
title: "ADR-20261120-xys: a module route may take no caller or a large body written to its folder"
description: "A manifest may list routes the host answers without resolving a caller, and routes whose large request body the host writes into the module's data folder. The operator approves each one."
date: 2026-11-20
status: proposed
supersedes: []
amends: [ADR-20261005-64j, ADR-20260827-wmv]
tags:
  - adr
  - extensions
---
# ADR-20261120-xys: a module route may take no caller or a large body written to its folder

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Builds on [ADR-20260827-wmv](20260827-wmv-a-module-is-mounted-and-an-adapter-is-constructed.md)

A module route takes a body of 1 MiB, so a module cannot take a print file,
which is larger. A module that speaks another program's protocol may also
need a request that comes before anybody holds a credential. This record
gives modules both, and each is general: no module is a special case in the
host.

## rules

### a route without a caller

1. A manifest may list routes under `routes.public`. Each entry is a method and
   a path inside the module's mount. The host sends a request on such a route
   to the module without resolving a caller. The principal is empty.
2. The operator approves each public route at install, as a request of its
   own. The install lists it as `public POST /hooks/announce`, and an
   approved install writes it to `service.public_routes`. A route the operator
   took away stays away on an update, and a route a new release adds is a new
   request. A path segment written `{name}` matches any one segment.
   The host matches the decoded path, and the module gets that same path,
   never parsed again. A decoded path with `?`, `#`, `%` or a control
   character reaches no module, because a module on an older SDK would read
   it as another route.
3. A public route is for a step before anybody holds a credential, such as a
   device that announces itself before it is paired. A module answers nothing
   on it that a person owns.

### a large body written to the module's folder

4. A manifest may list routes under `routes.staged`, each with a method, a
   path and `max_mib`, the largest body the module asks for, at most 4096.
   On an approved staged route the host writes the request body into the
   module's data folder. It sends the module the file's path in `body_file`
   instead of the body. The path is relative to that folder. No request is
   ever on both a public and a staged route. Routes are compared as
   patterns, so a public `/up/{id}` reaches a staged `/up/x`. The manifest
   refuses such a pair, and so does the loader of the configuration. Two
   staged routes that one request can reach are refused the same way,
   because the host holds a request to the first one it matches.
5. Each staged route is a request the operator approves at install, beside
   the public routes. It reads `upload PUT /files/{name} up to 1024 MiB`.
   With approval, the route and its limit go to `service.staged_routes`. On
   an update, a route the operator took away stays away and a limit the
   operator changed stays. A route a new release adds, or one that asks for
   more than before, is a new request. When the operator changed the limit,
   a route is a new request only when it asks for more than before and more
   than that limit. The operator may change the limit
   later, from 1 to 4096 MiB, in the extension's settings or with
   `staged_routes` on the resource API. The module then restarts. A route
   the operator did not approve is not staged, and its body goes on the
   wire, held to 1 MiB.
6. The module needs `needs.data` to list a staged route, and an instance the
   operator granted no data folder gets the body on the wire. The file is in
   `staged/` of the data folder. The host deletes it after the module
   answers, and a module that keeps the file moves it before it answers.
   The module has as long to answer as the host waits for the body: 30
   seconds and one second for each MiB of the route's limit. A
   body a stopped server left in `staged/` is removed when the server first
   starts the instance again. The server holds a request on a staged route
   to its approved limit, and every other module request to the document
   bound of 1 MiB. It refuses a `Content-Length` over the limit with
   `BODY_TOO_LARGE` before it reads the body. The host refuses a body that
   grows past the same limit, before the module is called. The module reads
   each approved route and its limit in `wirerpc.Grants.Staged` when it
   starts. A module that tells a client its largest file takes the number
   from there. The host holds at most 4 staged bodies of one instance at
   once, and refuses one more with `MODULE_UPLOADS_BUSY` and `Retry-After`.
   Before it reads a body, it checks the disk of the data folder for room
   for the body and 64 MiB more. The room held for bodies still being
   written counts as taken. It refuses with `MODULE_STORAGE_FULL` when there
   is none. A body of no stated length needs room for the whole limit.

### what the marketplace catalogue lists

7. This rule amends ADR-20261005-64j rule 10. A catalogue release row lists
   two more requests of the release's manifest, as the marketplace reviewed
   them. They are `public_routes` (rule 2) and `staged_routes` with each
   route's `max_mib` (rule 5). An approved marketplace install is held to
   them as to the permissions: a manifest that asks one the row does not
   list is refused with `MODULE_REQUESTS_CHANGED`. So a marketplace that
   does not publish these fields yet cannot serve an approved install of a
   module that asks for them.

## enforcement

| rule | gate |
|---|---|
| 1 to 3 | `TestAPublicRouteReachesTheModuleWithNoPrincipal` in `httpapi`, and the public route tests in `internal/app` |
| 2 | `TestAnEncodedPathCannotTurnAPublicRouteIntoAnother` in `httpapi` |
| 2 | `TestThePathReachesTheHandlerAsTheHostSentIt` in `sdk/routesapi` |
| 4 | the routes tests in `sdk/manifest` |
| 4 | `TestARouteGrantIsRefusedAtLoad` in `internal/app/config/install` |
| 4 | `TestAPublicRouteIsNeverStaged` in `resourcehost` |
| 4 | `TestAPublicPlaceholderKeepsItsRequestsOffTheDisk` in `resourcehost` |
| 4, 6 | `TestAStagedBodyReachesTheModuleAsAFile` in `resourcehost` |
| 5 | `TestAnUpdateKeepsTheOperatorsStagedRoutes` in `internal/app` |
| 5 | `TestAWriteChangesOnlyTheLimitOfAnApprovedStagedRoute` in `internal/app/resource` |
| 6 | `TestABodyOverItsModuleLimitIsRefusedBeforeTheModuleRuns` in `httpapi` |
| 6 | `TestTheSurfaceAndTheGrantsCarryTheHostsStagedLimit` in `resourcehost` |
| 6 | `TestAStagedRequestTakesTheBudgetOfItsRoute` in `resourcehost` |
| 6 | `TestTheFirstStartRemovesStagedBodiesARunLeft` in `resourcehost` |
| 6 | `TestAModuleHoldsAFewStagedBodiesAtOnce` in `resourcehost` |
| 6 | `TestAStagedBodyTheDiskHasNoRoomForIsRefused` in `resourcehost` |
| 6 | `TestTheRoomOfABodyInFlightIsHeldForIt` in `resourcehost` |
| 7 | `TestEveryAskSurvivesEveryHop` and `TestAMarketplaceReleaseAskingMoreThanListedIsRefused` in `internal/app` |

## rejected

| option | why not |
|---|---|
| a module listener on its own port for the HTTP routes | ADR-20260910-cqq rule 5 forbids serving this server's HTTP there |
| a larger bound for every module route | the operator would approve nothing, and every route of every module would take a body of any size |
| the module reads the body from a stream, as `storage.write` sends one the other way (ADR-20261026-c7m) | see [a body on a stream](#a-body-on-a-stream) |

### a body on a stream

The module would hold a call open for the whole upload, which takes minutes on
a slow network, and the host would count it against the module's breaker. A
module that prints or keeps the file would write it to a disk anyway. Written
once to the module's folder, the file goes on to a printer with
`delete_source` or into a storage with `storage.move` (ADR-20261123-vvb), and
the module never reads it.
