---
title: "ADR-20261124-e2e: a module draws a section on a printer's Printer tab"
description: "The slot device-info-section draws a module's card on a printer's Printer tab, under its Connection card and above Issues. Its title sits inside as on the core's cards there."
date: 2026-11-24
status: proposed
supersedes: []
amends: [ADR-20260911-4p6]
tags:
  - adr
  - modules
  - ui
---
# ADR-20261124-e2e: a module draws a section on a printer's Printer tab

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Builds on [ADR-20260911-4p6](20260911-4p6-a-module-extends-the-ui-through-one-library-and-four-slots.md), [ADR-20260929-nqd](20260929-nqd-the-module-slots-come-from-one-registry.md)

A module section could sit only at the foot of the Status tab, which shows
what the printer does now. The PrusaSlicer 2 Connector draws the address and
the API key a person pastes into the slicer once. That belongs beside the
printer's connection on the Printer tab, not with its live state.

## rules

1. The slot `device-info-section`, manifest key `device_info_section`, draws
   a card on a printer's Printer tab, under its Connection card and above
   Issues. What a person sets up to reach the printer sits there. The
   host draws the card and the entry's title inside it, as each of the core's
   cards on that tab has its title, and the module draws the card's content.
2. It is handed the device id, as `device-section` is, and takes the same
   entry fields, `devices` included.
3. `device-section` keeps its place at the foot of the Status tab.

## enforcement

| rule | gate |
|---|---|
| 1, 2 | `sdk/manifest/slots.go` and `make slots-check` |
| 1 | the card case in `slot-outlet.svelte.test.ts` |
| 2 | `SLOT_NAMES` in `packages/svelte/src/slots/slots.test.ts` |
| 3 | none. The registry entry of `device-section` is unchanged |

## rejected

| option | why not |
|---|---|
| move `device-section` to the Printer tab | a module that shows live state, such as a nozzle temperature, belongs on the Status tab |
| a tab of the module's own (`device-tab`) | one small section does not earn a tab, and a person looks for a printer's setup on its Printer tab |
