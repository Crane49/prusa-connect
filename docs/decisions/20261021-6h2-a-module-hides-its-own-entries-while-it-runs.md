---
title: "ADR-20261021-6h2: a module hides its own entries while it runs"
description: "A running module states with ui.hide which of its own pages and other entries are hidden now, and the web app takes them out of the sidebar and the slots without a reload."
date: 2026-10-21
status: accepted
tags:
  - adr
  - extensions
  - frontend
  - api
---
# ADR-20261021-6h2: a module hides its own entries while it runs

- status: accepted
- date: 2026-10-21

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Builds on [ADR-20260911-4p6](20260911-4p6-a-module-extends-the-ui-through-one-library-and-four-slots.md), [ADR-20261021-3dp](20261021-3dp-an-operator-hides-a-file-action-or-a-print-section.md)

A module declares its pages in its manifest, and the web app draws the
sidebar from that list before any bundle runs (ADR-20260911-4p6 rule 25). So a
bundle cannot take a page out of the menu. Take a module with a feature a
team can switch off, such as a planner's customer records. While the feature
is off, its menu entry leads to a page saying so. ADR-20261021-3dp lets the operator
hide an entry, but the operator is not the person who switches the feature,
and the operator's list is configuration and not state. This record lets the
module state the list itself.

## rules

1. A module with a process hides entries of its own interface with the wire
   method `ui.hide`, in `sdk/uiapi`. The host serves it on the session of
   every module and binds it to that instance. The params name no instance,
   so a module cannot hide another module's entries.
2. An entry is written as in ADR-20261021-3dp rules 1 and 2: a slot key of any slot
   except `cards`, a slash, and the page's `path` or the entry's `id`. Example:
   `pages/customers`.
3. A call carries the whole list, at most 64 entries of at most 200 bytes
   each, with no entry twice. A call with an empty list shows every entry
   again. The client and the host both check the bounds, and the host answers
   a list outside them with `CodeInvalidParams` and keeps the list it held.
   An entry is not checked against the manifest, and one the manifest does
   not declare has no effect.
4. The host keeps the list in memory for the running process. It forgets the
   list at each start of the process and at each stop. It never writes the
   list to the overlay or to any record.
5. A module states its list after each start, once it can read what is
   switched off, and again each time that changes. Until then every entry it
   declared shows.
6. The module's list and the operator's `hidden_contributions` are kept
   apart. `GET /api/v1/resources` carries the module's list as `ui.hidden`,
   beside the operator's list on the instance. A client hides an entry that is
   in either. A module cannot show an entry the operator hid.
7. A hidden page leaves the sidebar, and its route still answers. The page
   says itself that its feature is off. Hiding is visibility and grants or
   removes no access, as in ADR-20261021-3dp.
8. When a call changes the list, the host publishes
   `com.prusa.connect.resource.ui.changed`, a state event with the instance as
   its origin, carrying `resource` and the whole list as `hidden`. A call that
   leaves the list as it was publishes nothing.
9. The web app reads the listing again on that event, as it does on
   `com.prusa.connect.resource.state.changed`, and draws the sidebar from it.
   It hands the slot registry each module's joined list without reloading the
   bundles, so a hidden file action or section goes at the same moment.
10. The extension page's Where it shows tab marks a row the module hides. Its
    switch shows and writes the operator's list alone.
11. A module that cannot work without `ui.hide` names it in `requires`, and a
    server that does not serve it refuses to start the module.

## enforcement

| gate | what it holds |
|---|---|
| tests in `sdk/uiapi/wire_test.go` | the bounds of rule 3 on both sides, an empty list, and a host failure that names no internals |
| tests in `internal/app/resourcehost/uihide_internal_test.go` | the list is held per instance, published once per change, forgotten at a start, and a refused list keeps the held one |
| `make event-types-check` | the event of rule 8 is in the vocabulary and described |
| `make openapi-check client-sdk-check` | `ui.hidden` is in the published document and the client types |
| tests in `packages/svelte/src/data/modules.svelte.test.ts` and `packages/svelte/src/module-hidden.test.ts` | a page the module hides is hidden, and the two lists join |
| tests in `frontend/src/lib/stores/module-refresh.test.ts` | a refresh hands the registry the joined list without a reload |
| tests in `frontend/src/lib/screens/extension-detail.svelte.test.ts` | the row the module hides is marked and never written to the operator's list |

## rejected

| option | why not |
|---|---|
| the module writes the operator's `hidden_contributions` | a module could show what the operator hid, and each write would move the version other writes quote |
| keep the list in the overlay across restarts | a list on disk outlives the release that wrote it. The module states it again at each start |
| the bundle hides the page in the browser | no bundle runs to draw the sidebar, and ADR-20260911-4p6 rule 25 keeps it so |
| publish `com.prusa.connect.resource.state.changed` again | no lifecycle state moved, and a reader of that event would see a transition that did not happen |
| a manifest key that names a feature switch per page | the host would read a module's records to know a feature's state, and each new condition would need a new key |
| read the listing on a timer | the menu would be late by the whole interval, and every open tab would read the listing without cause |
