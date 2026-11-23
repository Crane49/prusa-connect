---
title: "ADR-20261112-62y: a module draws a printer control under the header and on each listed printer"
description: "Two slots, device-header and device-row, draw a module's component with no frame of the host's, and only once the bundle registers it, so a control can appear on some pages and not on others."
date: 2026-11-12
status: proposed
supersedes: []
amends: [ADR-20260911-4p6]
tags:
  - adr
  - frontend
  - modules
---
# ADR-20261112-62y: a module draws a printer control under the header and on each listed printer

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Builds on [ADR-20260911-4p6](20260911-4p6-a-module-extends-the-ui-through-one-library-and-four-slots.md), [ADR-20261112-2eb](20261112-2eb-prusaslicer-3-reaches-this-server-through-a-compatibility.md)

Inside PrusaSlicer, Prusa Connect shows "Set as current" on every printer: on
its card, on its row in the list and on its page. The prusaslicer3-connector
module needs the same control in the same places, and it must not show in a
browser.

The device slots of ADR-20260911-4p6 do not give that. `device-card` is a line
in the cards view alone. `device-section` is at the foot of the Status tab,
under a heading the host draws. `device-action` is drawn from the manifest
before the bundle loads, so the host shows it whether the bundle wants it or
not. Each frame the host draws around a component also stays on screen when
the component draws nothing.

## rules

1. The slot `device-header` sits on a printer's page under the header and its
   readings, above the tabs. It is a section with a device subject.
2. The slot `device-row` sits at the end of each printer in the listing, in
   every view: a card, a camera frame, a narrow row and a table row. It is a
   section with a device subject.
3. The host draws no frame, no heading and no box of its own for either slot.
   The wrapper it writes for the `data-slot` attributes is `display:
   contents`, so the component's own root is the item of the page's layout.
4. The host draws an outlet of either slot only for a component the bundle
   registered. A declared entry with no registration leaves the page exactly
   as it was. On the listing, the screen passes the outlet only once a
   `device-row` component is registered, so a row keeps no empty space.
5. A module that shows a control only in some context declares the entry and
   registers it only in that context. The component also draws nothing
   outside it.
6. These two slots join the set of ADR-20260911-4p6 rule 48 and the slots
   later records added. `sdk/manifest/slots.go` lists the whole set.

## enforcement

| rule | gate |
|---|---|
| 1, 2, 6 | `make slots-check`, and `slot-outlets.svelte.test.ts` under `make frontend-check` |
| 2 | `draws the row end on every printer in every view` in `printers-screen.svelte.test.ts` |
| 1 | `draws what sits below the header between the header and the tabs` in `printer-detail-screen.svelte.test.ts` |
| 3, 4 | `a slot drawn bare` in `slot-outlet.svelte.test.ts` |

## rejected

| option | why not |
|---|---|
| `device-action` drawn only once registered | the menu draws an action from the manifest before the bundle loads, and every module relies on it |
| `device-card` in every view, with no frame | a card line is one line of text, and existing modules write text for it |
| `device-section` moved above the tabs | a section is a block with a heading. A large block above the tabs pushes the tabs down on every printer |
| one slot for the page and the listing | the two places need different sizes, and a module may want one and not the other |
