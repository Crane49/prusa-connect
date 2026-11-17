---
title: "ADR-20261024-34x: each tool says whether it is active"
description: "Each tool carries the architecture's active member, the active tool is derived from those members, a heater that is off is a target of 0 that reads as null, and the map from a G-code T number to a tool is /gcode_tool_map."
date: 2026-10-24
status: accepted
amends: [ADR-20261023-gna, ADR-20261024-fsz]
tags:
  - adr
  - sdk
  - connectors
---
# ADR-20261024-34x: each tool says whether it is active

- status: accepted
- date: 2026-10-24

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Supersedes in part [ADR-20261023-gna](20261023-gna-a-printers-tools-are-a-tree-of-heads-and-feeder-slots.md), rules 7 and 11 | Amends [ADR-20261024-fsz](20261024-fsz-the-tool-tree-marks-the-names-that-are-not-the-architectures.md)

ADR-20261023-gna rule 7 gave the machine one active tool at `/active_tool`. The public
Prusa architecture's
[printer telemetry](https://specs.prusa3d.tech/architecture/#/printer_configuration)
marks each tool instead, and firmware 8.0 sends that member for each tool.
One id cannot say what the members say. On a machine with a feeder on each
head, a slot loaded into a head that is not picked is active, and one id
names only the slot that prints. The architecture also writes a null target
for a heater that is off, which the tree removed and a reader took as
unknown. And `/tool_mapping/<n>` carried the name of the print command's
`tool_mapping` argument, which says a different thing.

## rules

1. A tool has the member `active`, a boolean, as the architecture's
   telemetry defines it. A head is active while it is picked, and the one
   head of a machine that is not a toolchanger is always active. A slot is
   active while it is loaded into its head, even when that head is not
   picked. More than one tool can be active at once.
2. `/active_tool` stays, as Prusa Connect's own. It is derived from the
   members by `connectorapi.ActiveToolOf`: the deepest active tool whose
   tools above it are all active, and the lower id between two of the same
   depth. A driver writes the members and the path together, so they agree.
   A reader that finds no path takes the derivation from the members.
3. A driver reports a member when it changes. The Prusa driver keeps both
   kinds of path in its reserved slots, so a full queue never drops the last
   value.
4. `nozzle_target_temp` is 0 while the heater is off. A target of 0 is never
   a real target, so 0 is the fourth null of ADR-20261023-gna rule 8.
   `connectorapi.ReadTools`, the JSON form and the API read it as `null`. A
   driver removes the path only when it does not know.
5. The tool a G-code `T<n>` selects is at `/gcode_tool_map/<n>`. The default
   rule of ADR-20261023-gna rule 11 holds for a number the map leaves out. The name
   says what the key is, the n of a `T<n>` line in a print file or a G-code
   command. It does not repeat the name of the `tool_mapping` argument, which
   goes from the caller to the printer.
6. ADR-20261024-fsz rule 1 now lists `/gcode_tool_map/<n>` where it listed
   `/tool_mapping/<n>`, and `active` is the architecture's name.

## How the Prusa driver reads active

| shape | what the frame says | what the driver writes |
|---|---|---|
| tree, firmware 8.0 | `active` for each tool | each member as it is |
| numbered, a toolchanger | the slot block's `active`, the head picked from 1, 0 for none | `true` for that head, `false` for each other head the block lists |
| numbered, an MMU | the slot block's `active`, the slot loaded from 1, 0 for none | head `0` always `true`, `true` for that slot, `false` for each other slot |
| numbered, one head and no slot block | nothing | head `0` `true`, from INFO |

## enforcement

| gate | what it holds |
|---|---|
| tests in `sdk/connectorapi/tools_test.go` | `ActiveToolOf`, the member that `ReadTools` returns and prefers, the off heater as null, `/gcode_tool_map` |
| `slot_test.go` and `tooltransitions_test.go` in `internal/connector/xbuddy` | each shape becomes the members, a change only is reported, a slot loaded into a head not picked |
| `tools_sim_test.go` in the same folder | the simulated printers arrive with their members |
| `dialect_test.go` in `examples/adapters/moonraker` | a Klipper head is active as `toolhead.extruder` names it, and an off heater is 0 |
| tests in `internal/app/devicetools_test.go` | the device record carries the members, the derived tool and the null target |
| tests in `packages/svelte/src/domain/state-paths.test.ts` | a page reads each head's member and the slot loaded into it |

## rejected

| option | why not |
|---|---|
| drop `/active_tool` | every page and module reads it, and the derivation is one rule |
| derive `active` from `/active_tool` | the id cannot say that a slot is loaded into a head that is not picked |
| write the members on every frame | each patch is an event, and on an MMU that is six more for each frame |
| remove the target of an off heater | a removal is unknown, and the architecture says off |
| `/file_tool_map/<n>` | a raw G-code command uses the same `T<n>`, so the map is not only a file's |
