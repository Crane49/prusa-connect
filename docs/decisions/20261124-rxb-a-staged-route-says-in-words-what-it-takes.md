---
title: "ADR-20261124-rxb: a staged route says in words what it takes"
description: "A staged route in module.yaml may carry a title, which the extension's Uploads tab shows in place of the method and path. Only the module's ask carries it, and a changed title asks the operator nothing."
date: 2026-11-24
status: proposed
supersedes: []
amends: [ADR-20261120-xys]
tags:
  - adr
  - modules
---
# ADR-20261124-rxb: a staged route says in words what it takes

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Builds on [ADR-20261120-xys](20261120-xys-a-module-route-may-take-no-caller-or-a-large-body-written.md)

The Uploads tab named each staged route by its method and path, such as
`PUT /printers/{printer}/api/v1/files/{storage}/{name}`. An operator setting
a limit cannot tell from that which client uses the route.

## rules

1. A staged route may carry `title`: plain text, at most 80 characters.
2. The ask the install records keeps the title. A grant in
   `service.staged_routes` is a limit and holds none.
3. The Uploads tab shows a titled route by its title, with the method and
   path under it. A route with no title shows the method and path, as before.
4. A title is not a request. A changed or new title in a release asks the
   operator nothing.

## enforcement

| rule | gate |
|---|---|
| 1 | `TestAStagedRouteReadsItsTitle` and the title cases of `TestARoutesBlockIsRefused` in `sdk/manifest` |
| 2 | `TestAnApprovedInstallGrantsItsPublicRoutes` in `internal/app` |
| 3 | `names a titled route by its title and saves only the limit` in `extension-uploads.svelte.test.ts` |
| 4 | `stagedBeyond` in `internal/app/moduleasks.go` compares the route and the limit alone |

## rejected

| option | why not |
|---|---|
| the title in the operator's grant | the grant is what the operator decides, and the title is the module's text |
