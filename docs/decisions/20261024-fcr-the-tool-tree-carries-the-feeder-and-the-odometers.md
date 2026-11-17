---
title: "ADR-20261024-fcr: the tool tree carries the feeder and the odometers"
description: "A feeder names its model, its family and its kind, its unit reports its progress code and the command it runs, and each tool reports its odometers, all under the public Prusa architecture's names."
date: 2026-10-24
status: accepted
amends: [ADR-20261023-gna]
tags:
  - adr
  - sdk
  - connectors
---
# ADR-20261024-fcr: the tool tree carries the feeder and the odometers

- status: accepted
- date: 2026-10-24

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Amends [ADR-20261023-gna](20261023-gna-a-printers-tools-are-a-tree-of-heads-and-feeder-slots.md), rule 3 | Builds on [ADR-20261024-34x](20261024-34x-each-tool-says-whether-it-is-active.md)

The public Prusa architecture's
[printer configuration](https://specs.prusa3d.tech/architecture/#/printer_configuration)
names a feeder by its model, its family and its kind. It gives the feeder's
unit a telemetry of its own, and each tool odometers. Firmware 8.0 sends
them. The tree held only the feeder's slot count and model. A page could not
say what the MMU is doing, and a module could not count the filament a tool
used.

## rules

1. These members join the list of ADR-20261023-gna rule 3, with the architecture's
   names and meanings:
   - `feeder/base_model` and `feeder/type`, beside `feeder/model`
   - `feeder/mmu_progress_code`, the step the unit is at, and
     `feeder/mmu_command_in_progress`, the command it runs as one character
   - `odometer_id`, `odometer_e` and `odometer_weight` on a tool that takes a
     filament
2. The architecture keeps a feeder's telemetry apart from its configuration.
   The tree keeps both under `feeder/` on the tool that carries the feeder,
   and the API serves them in one `feeder` object.
3. `feeder/mmu_command_in_progress` is removed while the unit runs no
   command. Firmware 8.0 leaves the key out then, and earlier firmware sends
   an empty character.
4. A feeder whose slot count nobody reported is unknown, whatever else it
   carries. A reader does not take its slot count of 0 as no feeder.
5. `odometer_id` changes, without counting up, at each reset of the
   odometers. A reader that sees it change counts again from zero.

## How the Prusa driver reads them

| shape | feeder names | unit | odometers |
|---|---|---|---|
| tree, firmware 8.0 | `feeder` in INFO | `feeder` in each head's telemetry | each tool's telemetry |
| numbered | `type` `MMU` while the MMU block is enabled, no model | the slot block's `state` and `command` | none |

## enforcement

| gate | what it holds |
|---|---|
| tests in `sdk/connectorapi/tools_test.go` | the members read back, a feeder with no slot count unknown |
| `slot_test.go`, `tooltransitions_test.go` and `tools_sim_test.go` in `internal/connector/xbuddy` | both shapes, an idle unit, an MMU switched off |
| tests in `internal/app/devicetools_test.go` | the device record carries them |
| `printer-tools.svelte.test.ts` in `packages/svelte` | the Tools card names the feeder and says what its unit is doing |

## rejected

| option | why not |
|---|---|
| a `feeder_telemetry/` prefix | a reader would look in two places for one feeder, and the API serves one object |
| a model from the MMU version in the numbered shape | a version is not a model, and the guess would be wrong for a unit upgraded in place |
| a text for each progress code in the API | the codes belong to the unit's firmware. A page translates the ones it shows |
