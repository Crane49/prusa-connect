---
title: "ADR-20261009-wax: an operator hides a module contribution"
description: "hidden_contributions on an instance lists the UI entries of a module that no viewer sees; the server keeps the manifest whole and the web app filters."
date: 2026-10-09
status: superseded in part by ADR-20261021-3dp
tags:
  - adr
  - extensions
  - frontend
  - api
---
# ADR-20261009-wax: an operator hides a module contribution

- status: superseded in part by ADR-20261021-3dp. Rules 1 and 3
  take every slot key except `cards`, and the rest stands
- date: 2026-10-09

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | ADRs: [20260911-4p6](20260911-4p6-a-module-extends-the-ui-through-one-library-and-four-slots.md), [20260929-nqd](20260929-nqd-the-module-slots-come-from-one-registry.md)

An operator may want an installed extension without every place it shows. The
statistics extension keeps its page and its Status section, and its line on
every printer card is hidden. Hiding changes what people see. It does not
change what they may do.

## rules

1. An instance has `hidden_contributions`, a list of strings in the form
   `<slot key>/<identity>`. The slot key is `SlotName.Key()` from
   `sdk/manifest/slots.go`: `pages`, `settings`, `device_card`,
   `device_section`, `device_tab` or `device_action`. The identity is the
   entry's `path` for `pages` and its `id` for every other slot. Example:
   `["device_card/fleet-stats", "pages/fleet-stats"]`.
2. Omitted or empty means nothing is hidden. The list applies to every viewer.
3. The API accepts at most 64 entries, each at most 200 characters, each
   matching `^(pages|settings|device_card|device_section|device_tab|device_action)/[^\s]+$`,
   with no duplicates. It does not check an entry against the manifest.
4. An entry the manifest does not declare is kept and has no effect. A later
   version that declares it again is hidden at once.
5. The server keeps `ui.contributes` complete (ADR-20260911-4p6 rule 32). The web app
   filters. The extension page lists every entry so an operator can show one
   again.
6. A hidden entry stays declared. When the extension registers it, the library
   accepts the call and returns the entry from no query. An extension that
   registers an entry it never declared is dropped, as before.
7. A hidden page loses its menu entry. Its route still answers a direct link:
   hiding is visibility and is not access control.
8. `PUT` on a resource replaces the whole spec. Every writer sends the list
   back: the enable switch, the settings form and the visibility switches all
   send `instance.hidden_contributions` unless they change it.
9. An operator sets the list in the YAML file under the instance, as
   `hidden_contributions`, like `enabled`. A file-owned instance is read-only
   in the web app, so its list is edited in the file.
10. The list is for the UI only. It does not restart the module.

## enforcement

| gate | what it holds |
|---|---|
| `make openapi-check client-sdk-check` | the field's shape and limits in the API and the SDK |
| tests in `internal/transport/http/httpapi/resources_test.go` | a `PUT` keeps the list and an invalid entry is refused |
| tests in `packages/svelte/src/slots/registry.svelte.test.ts` | a hidden entry registers and no query returns it |
| tests in `frontend/src/lib/screens/extension-detail.svelte.test.ts` | a switch writes the list, and the enable switch and the settings form keep it |

## rejected

| option | why not |
|---|---|
| the server removes hidden entries from `ui.contributes` | the extension page could not list them, and a registered hidden entry would be dropped as undeclared |
| a hidden page answers 404 | access control belongs to permissions, and a second mechanism would look like one |
| a per-user list | the operator decides for the whole server, and a person who wants less has the browser |
| a `PATCH` for the list | the API has no patch on a resource, and a second write path would need its own version check |
| a boolean per slot | the operator needs one line of one extension, not a whole slot |
