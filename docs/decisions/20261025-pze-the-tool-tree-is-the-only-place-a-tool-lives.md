---
title: "ADR-20261025-pze: the tool tree is the only place a tool lives"
description: "No flat path for the nozzle or the material of the tool printing now, and no path naming the active tool. A machine with one nozzle is head 0, and a reader derives the tool printing now from each tool's active member."
date: 2026-10-25
status: accepted
amends: [ADR-20261023-gna, ADR-20261024-fsz, ADR-20261024-34x]
tags:
  - adr
  - sdk
  - connectors
---
# ADR-20261025-pze: the tool tree is the only place a tool lives

- status: accepted
- date: 2026-10-25

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Supersedes in part [ADR-20261023-gna](20261023-gna-a-printers-tools-are-a-tree-of-heads-and-feeder-slots.md), rule 6, and [ADR-20261024-34x](20261024-34x-each-tool-says-whether-it-is-active.md), rules 2 and 3 | Amends [ADR-20261024-fsz](20261024-fsz-the-tool-tree-marks-the-names-that-are-not-the-architectures.md), which named `/active_tool` | Builds on [ADR-20261024-fcr](20261024-fcr-the-tool-tree-carries-the-feeder-and-the-odometers.md)

ADR-20261023-gna rule 6 kept three flat paths beside the tree for the tool printing
now: `/material`, `/temperature/nozzle` and `/temperature/nozzle_target`.
ADR-20261024-34x kept `/active_tool` as one id derived from the members. The project
is before its first stable release, so it keeps no second copy of its own
contract. Every reader read the same value in two places, and every driver
wrote it twice.

## rules

1. A tool's readings and its material are in the tree and nowhere else.
   There is no `/material`, `/temperature/nozzle` or
   `/temperature/nozzle_target`.
2. A machine with one nozzle is head `0`. Its temperature is
   `/tools/0/nozzle_temp`, its target `/tools/0/nozzle_target_temp`, and its
   material the head's material, or the material of the slot loaded into
   it.
3. There is no `/active_tool` and no `active_tool` in the API. Each tool
   carries its `active` member. A reader that needs the tool printing now
   derives it with `connectorapi.ActiveToolOf`, which `ToolTree.Active` holds
   for a Go reader. A page uses the head printing now, and head `0` when none
   is picked.
4. A driver still reads each firmware's own wire, also of firmware that sends
   one nozzle temperature for the machine. It writes the tree alone. The
   Prusa driver puts the temperature and target at the top of a frame on the
   head they belong to.
5. The Prusa driver removes the four old paths from a stored tree at the
   first INFO of a session, as it removes the 1-based slot paths.
6. A stored reading keeps the path it was stored under. A nozzle stored
   under `/temperature/nozzle` before this change is not drawn under
   `/tools/0/nozzle_temp`.
7. `enabled`, `/gcode_tool_map/<n>` and `/build_volume` stay, as ADR-20261024-fsz
   names them. `enabled` is the tree's encoding of the architecture's null
   tool, and the API serves `null`.

## enforcement

| gate | what it holds |
|---|---|
| `internal/arch/flatpaths_test.go` | no source outside the allowed files names an old flat path |
| `internal/app/tooltree_contract_test.go` | a server with each simulated printer family, on both wires, holds only tree paths, and its device record parses |
| tests in `sdk/connectorapi`, `sdk/printcompat`, `internal/connector/xbuddy` and `examples/adapters/moonraker` | the tree alone is read and written |

## rejected

| option | why not |
|---|---|
| keep the flat paths as aliases | two places for one value, and the project has no released contract to keep |
| keep `/active_tool` as a derived convenience | it is one rule, which a reader applies to the members |
| move the old history samples to the new path | a stored sample is a record of what was written, and a rewrite would change history |
