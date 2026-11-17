---
title: "ADR-20261023-gna: a printer's tools are a tree of heads and feeder slots"
description: "Tools are keyed by ids such as 0 and 0.2 with no count cap, named after the public Prusa architecture, with a feeder per tool, material per slot, three values for its nulls, and a default rule from a file's Tn to a tool."
date: 2026-10-23
status: superseded in part by ADR-20261024-34x
amends: [ADR-20261022-6df]
tags:
  - adr
  - sdk
  - connectors
---
# ADR-20261023-gna: a printer's tools are a tree of heads and feeder slots

- status: superseded in part by ADR-20261024-34x. Rules 7 and 11 give way to
  it, and ADR-20261024-fsz marks the names that are not the architecture's
- date: 2026-10-23

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Rules 7 and 11 superseded by [ADR-20261024-34x](20261024-34x-each-tool-says-whether-it-is-active.md) | Rule 3 amended by [ADR-20261024-fcr](20261024-fcr-the-tool-tree-carries-the-feeder-and-the-odometers.md) | Amended by [ADR-20261024-fsz](20261024-fsz-the-tool-tree-marks-the-names-that-are-not-the-architectures.md) | Supersedes in part [ADR-20261022-6df](20261022-6df-every-printer-reports-its-tools-on-one-model.md) | Builds on [ADR-20260826-c77](20260826-c77-one-name-for-one-thing-on-the-wire.md), [ADR-20260829-jdq](20260829-jdq-the-host-bounds-the-text-a-driver-hands-it.md)

ADR-20261022-6df put every tool in one flat list from 0, so an MMU slot and a toolhead
were the same kind of thing, and it capped the list at 16. That model cannot
describe a printer with eight heads where each head has its own MMU. Prusa
firmware and Prusa Connect now describe tools as a tree, in the public Prusa
architecture's printer configuration
(`https://specs.prusa3d.tech/architecture/#/printer_configuration`). This record moves
the core to that tree. It replaces rules 1 to 6 of ADR-20261022-6df. Rules 7 to 9 of
ADR-20261022-6df stand. The tools are stored as state, the nozzle commands take a
`tool` argument counted from 0 as rule 8 now says, and the driver reports the
build volume.

## rules

1. A tool has an id: whole numbers from 0 joined by dots. A head is a nozzle
   with its own heater and has the id `0`, `1` and on. Slot k of the feeder
   on tool `X` has the id `X.k`, and a slot may carry a feeder of its own, so
   `3.1.0` is a legal id. `connectorapi.ToolID` holds the spelling: no sign,
   no leading zero.
2. There is no count cap. The only limits are the ones every state path has.
   The host keeps at most `RetainedTextFloor` bytes of one path and one value
   (ADR-20260829-jdq). It keeps at most the device's state budget for the whole tree,
   256 KiB unless the operator raises it. A reader never expands a reported count
   into a list; it lists the tools the tree holds.
3. A member of a tool is at `/tools/<id>/<member>`, with the paths and names
   `sdk/connectorapi/tools.go` gives. The names are the architecture's:
   `features/nozzle_diameter`, `features/nozzle_high_flow`,
   `features/nozzle_hardened`, `features/nozzle_high_temp`,
   `feeder/slot_count`, `nozzle_temp`, `nozzle_target_temp`,
   `print_fan_rpm`, `heatbreak_fan_rpm` and `consumed_weight`. A feature the
   driver reports under another name it knows passes through under
   `features/<name>`.
4. A feeder sits on a tool and has `feeder/slot_count` slots, which are the
   child ids `0` to `slot_count - 1`. A slot the tree still holds past the
   slot count, or under a tool whose feeder is gone, is ignored.
5. The material is per slot, or per tool when the tool has no feeder. It is
   the abbreviation of the material of the tool's material package instance,
   at `material_package_instance/package/material/abbreviation`, as the
   architecture and OpenPrintTag name it. A tool with a feeder holds no
   material of its own.
6. The temperatures and the fans are per head. Three flat paths are aliases
   of the tool printing now, the tool at `/active_tool`:
   `/temperature/nozzle` is its `nozzle_temp`, `/temperature/nozzle_target`
   its `nozzle_target_temp` and `/material` its material.
   `sdk/connectorapi/tools.go` names them. A driver writes the ones its
   machine reports. A machine with one head reports its nozzle readings at the
   flat paths only. Its driver does not copy them under the head, because
   every patch is an event a subscriber receives. A reader of a machine with
   one head takes them for the head, as `printcompat.PrinterFromState` does. A
   machine with more than one head reports each head's readings under the
   head too, and the flat paths follow the head printing now. A reader that
   needs the value of one tool reads the tree. The flat paths say nothing
   about a tool that is not printing.
7. The tool printing now is `/active_tool`, the id of the deepest selected
   tool as text: `"1"` on a toolchanger, `"0.2"` for a slot loaded into head
   `0`. The heads above it are selected too.
8. The tree cannot hold a null, because a patch with a null value removes the
   path. The three nulls of the architecture are values of their own, and
   `connectorapi.ReadTools` turns them back into nulls:
   - a disabled tool is `enabled` false, and its other members are removed
   - a tool known to have no feeder is `feeder/slot_count` 0
   - a tool with nothing loaded has the material `""`, which is
     `connectorapi.NoMaterial`
9. A path a driver does not write is unknown, and a reader never takes it as
   zero, false, no feeder or nothing loaded.
10. `/tool_count` is the number of heads the machine has room for, enabled or
    not, as the architecture's `tool_count`.
11. A print file names its filaments `T0`, `T1` and on. A driver that knows
    which tool `Tn` selects writes the id at `/tool_mapping/<n>`, and that id
    wins. Without one, the default holds: on a machine with a single head that
    carries a feeder, `Tn` is slot `0.n`; on any other machine `Tn` is head
    `n`. A machine with a feeder on more than one head is one of the others,
    so its `Tn` is head `n` until it reports a mapping.
    `ToolTree.Resolve` is the one place that rule is written.
12. The driver normalizes. It maps each firmware's or vendor's format onto the
    tree where it reads the frame, and decides which format a frame has by
    something it can test. A host, a module and a page never branch on a
    vendor, a model or a firmware to read a tool.
13. A reader of the tools reads them through `connectorapi.ReadTools`. Its
    JSON form is the architecture's shape, with the three nulls, and is what
    an API serves.

14. `sdk/printcompat` judges a print file against a printer:
    `PrinterFromState` reads the printer's tool tree, and `Evaluate` answers
    ok, warning or refusal. Each filament the file uses is checked on the tool
    its T number resolves to. Its material is held to that tool, and its
    nozzle to the head the tool prints through. It mirrors the public
    firmware's checks before a print and cites each one, and adds the build
    volume check the firmware does not make. It does no I/O and needs no
    permission of its own.
15. A check whose input is unknown on either side is reported as unverified,
    never as passed. A tool with nothing loaded is known, and fails the
    material check as the firmware's `---` does.
16. A warning is the firmware's default for a check a person may configure on
    the printer. `printcompat` cannot see that setting and says so.

17. The device record carries `tools`, the tool tree in the architecture's
    shape, and `build_volume`. They are a typed reading of the state tree for
    a client that does not read it. The host builds them with
    `printcompat.PrinterFromState` on each read and stores no second copy. A
    print file's needs reach the stat route and the module wire as
    `filaments`, by T number from `T0`.
18. The API does not flatten the tree. A member keeps the architecture's name
    and nesting, so the material is
    `material_package_instance.package.material.abbreviation` and a head's
    reading is `nozzle_temp`. The three nulls of rule 8 are `null` again: a
    disabled tool, `"feeder": null` and `"material_package_instance": null`.
    A member the printer did not report is left out, which is rule 9 on the
    wire.

## How the Prusa driver reads two shapes

The shipping firmware keys its filament paths from 1. On an XL and an INDX
each path is a head; in front of an MMU each path is a slot. The driver makes
an XL five heads, an INDX eight heads, and an MK4S with an MMU3 head `0` with a
feeder of five slots `0.0` to `0.4`. A firmware that speaks the architecture
sends `tool_count` and ids from `"0"`, and the driver copies them unchanged. It
tells the two apart by the shape of the frame, in `treeShaped`, because no
firmware version is named for the change.

## enforcement

| gate | what it holds |
|---|---|
| tests in `sdk/connectorapi/tools_test.go` | the id spelling, the paths, the three nulls, the slots a feeder count covers, and the default and reported `Tn` |
| `tools_test.go` and `slot_test.go` in `internal/connector/xbuddy` | the numbered shape and the tree shape each become the tree, and the shape is detected from the frame |
| `tools_sim_test.go` in the same folder | a simulated XL, an MK4S with an MMU3, and a machine with a feeder on each of two heads arrive on the tree |
| tests in `packages/svelte/src/domain/state-paths.test.ts` | a page reads the heads from the tree and no head from the old paths |
| tests in `sdk/printcompat` | each firmware check, a filament on a slot or on a nested feeder, a disabled slot, the empty tool, the volume |
| `dialect_test.go` in `examples/adapters/moonraker` | one extruder is reported at the flat paths only, and with two the flat paths follow the extruder printing now |
| tests in `internal/app/devicetools_test.go` | the device record nests the material as the architecture does, and keeps a left-out member apart from a `null` one |

## rejected

| option | why not |
|---|---|
| keep the flat list and its cap | it cannot say which slots belong to which head, and the cap is a number nobody can defend |
| a JSON object per tool at `/tools/<id>` | a temperature would rewrite the whole object four times a second, and a single value could no longer be patched |
| `null` in the tree for the three nulls | a null patch removes a path, which is how a driver says a value is gone; giving it a second meaning breaks every reader |
| remove `/material` and `/temperature/nozzle_target` | every page, module and client of a one-head machine reads them, and rule 6 already keeps `/temperature/nozzle` |
| keep `/tool/<i>` beside the tree for a transition | no release shipped `/tool/<i>`, so nobody reads it |
| the firmware version to tell the two shapes apart | no version is named for the change, and the shape cannot be mistaken |
| a flat `material` text and `slot_count` 0 in the device record | a client would read two spellings of one tool, the architecture's from a printer or a file and ours from the API |
| the compatibility verdict as an HTTP route | what to do with a warning is the caller's policy, and a pure function lets each caller decide |
| a host table of which models have feeders | the host would branch on vendors, and a driver from outside this repository could not add its machines |

## open items

- Whether the material should carry more of the material package instance,
  such as its brand and colour, waits for a firmware that sends them.
- Which `Tn` a file means on a machine with a feeder on more than one head
  needs a mapping from the firmware. No firmware reports one yet.
