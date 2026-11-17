---
title: "ADR-20261021-3dp: an operator hides a file action or a print section"
description: "hidden_contributions takes every slot key of the slot table except cards, so an operator hides an item in the file menu and a section of the print dialog too."
date: 2026-10-21
status: accepted
amends: [ADR-20261009-wax]
tags:
  - adr
  - extensions
  - frontend
  - api
---
# ADR-20261021-3dp: an operator hides a file action or a print section

- status: accepted
- date: 2026-10-21

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Supersedes in part [ADR-20261009-wax](20261009-wax-an-operator-hides-a-module-contribution.md) | Builds on [ADR-20260929-nqd](20260929-nqd-the-module-slots-come-from-one-registry.md), [ADR-20261008-sww](20261008-sww-the-home-page-is-a-dashboard-of-cards.md)

ADR-20261009-wax lets an operator hide single entries of a module's interface. Its
list of slot keys was written before the file menu and the print dialog had
slots. A module that adds an item to every file's menu, or a section to the
print dialog, could not be hidden there. This record replaces rules 1 and 3 of
ADR-20261009-wax. The other rules of ADR-20261009-wax stand.

## rules

1. An entry of `hidden_contributions` is `<slot key>/<identity>`. The slot key
   is `SlotName.Key()` of any slot in `sdk/manifest/slots.go` except
   `cards`. Today the keys are `pages`, `settings`, `device_card`,
   `device_section`, `device_tab`, `device_action`, `file_action` and
   `print_section`.
2. The identity is the entry's `path` for `pages` and its `id` for every other
   slot. Example: `["file_action/plan", "print_section/planner"]`.
3. The API accepts at most 64 entries, each at most 200 characters, with no
   duplicates. Each entry is a hideable slot key, a slash, and an identity with
   no whitespace. It does not check an entry against the manifest.
4. The server builds the list of hideable slot keys from the slot table. A new
   slot is hideable in the change that adds it. A slot that must not be hidden
   is named in this record as an exception.
5. A hidden `file_action` entry is missing from the menu on each file's row in
   the storage listing, and from the menu on a file's own page.
6. A hidden `print_section` entry is not drawn in the print dialog.
7. The extension page's Where it shows tab lists both kinds with a switch, as
   a file action and as a print dialog section.
8. A dashboard card cannot be hidden, and the API refuses `cards/<id>`. A card
   shows only where a person placed it, and that person removes it from the
   dashboard.

## enforcement

| gate | what it holds |
|---|---|
| tests in `internal/app/resource/store_test.go` | an entry of every hideable slot is accepted, and `cards/<id>` is refused |
| tests in `packages/svelte/src/slots/registry.svelte.test.ts` | a hidden file action and a hidden print section register, and no query returns them |
| tests in `frontend/src/lib/screens/storage.svelte.test.ts` and `file-detail.svelte.test.ts` | a hidden file action is missing from both file menus |
| tests in `packages/svelte/src/shell/print-picker/print-dialog.svelte.test.ts` | a hidden print section is not drawn |
| tests in `frontend/src/lib/screens/extension-detail.svelte.test.ts` | a switch for each kind writes its entry to the list |
| `make openapi-check client-sdk-check` | the field's description names the hideable slot keys |

## rejected

| option | why not |
|---|---|
| a fixed list of slot keys in the pattern | it did not name the two slots added after ADR-20261009-wax, and the next new slot would be absent as well |
| hiding a card from the card picker | a card already placed on a dashboard would stay there as a placeholder. The person who placed it removes it |
| editing rules 1 and 3 of ADR-20261009-wax in place | an accepted ADR is never edited |
