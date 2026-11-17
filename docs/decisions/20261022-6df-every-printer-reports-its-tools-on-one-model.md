---
title: "ADR-20261022-6df: every printer reports its tools on one model"
description: "Tools are counted from 0 at /tool/<i>, the driver maps its machine's own numbering and words onto that model, a tool with nothing loaded is a known state, and the driver reports the build volume."
date: 2026-10-22
status: superseded in part by ADR-20261023-gna
tags:
  - adr
  - sdk
  - connectors
---
# ADR-20261022-6df: every printer reports its tools on one model

- status: superseded in part by ADR-20261023-gna. Rules 1 to 6
  give way to the tool tree, and rules 7 to 9 stand. Rule 8 is amended on
  2026-10-23
- date: 2026-10-22

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Rules 1 to 6 superseded by [ADR-20261023-gna](20261023-gna-a-printers-tools-are-a-tree-of-heads-and-feeder-slots.md) | Builds on [ADR-20260826-c77](20260826-c77-one-name-for-one-thing-on-the-wire.md), [ADR-20260831-c7b](20260831-c7b-an-adapter-declares-the-states-and-the-permissions-and-the-host-places-them.md)

A program that picks a printer for a print file needs to know each tool of the
printer: its nozzle, and the filament loaded in it. Every vendor reports this
in its own way. A Prusa firmware keys its tools from 1 and writes `---` for an
empty tool. Klipper names its extruders `extruder`, `extruder1` and on. This
record sets one model for all of them, and says who converts.

## rules

1. A tool is addressed at `/tool/<i>/<member>`, with the paths and members
   `sdk/connectorapi/tools.go` names. The index counts from 0, as G-code
   counts tools: the tool a file selects with `T0` is `/tool/0`.
2. Every index a reader sees counts from 0: the state tree, the API, the SDK
   and the labels a page shows, which read `T0`, `T1`.
3. The driver normalizes. It maps its machine's numbering, its words and the
   differences between firmware versions onto the model at the boundary where
   it reads a frame. A host, a module and a page never branch on a vendor, a
   model or a firmware to read a tool.
4. A path a driver does not write is unknown, and a reader never takes it as
   zero, false or an empty tool.
5. A tool with no filament loaded has the material `connectorapi.NoMaterial`,
   the empty text. It is a known state, apart from a material nobody reported.
6. When a machine stops listing a tool, the driver removes that tool's paths.
   The host keeps nothing of a removal of a path it does not hold, so a driver
   may remove a tool it cannot know is there.
7. The tool facts are state, so the host stores them with the state tree.
   They are kept while the printer is away and across a restart.
8. The `tool` argument of the published nozzle commands keeps its meaning and
   counts from 1. A page sends `index + 1`. Changing it would change the
   capability vocabulary, which ADR-20260825-bhe versions on its own.
   - **Amended on 2026-10-23.** The argument counts from 0, as rule 2 says,
     and a page sends the index unchanged. It names a head by the id the state
     tree publishes it under, so a client that reads `/tools/1` changes the
     same head with `tool` 1. The driver maps it to each firmware's own key.
     The name stays `tool`, and [ADR-20260825-dee](20260825-dee-the-capability-vocabulary.md)
     rule 6 is not followed here. That rule asks for a new name when a
     meaning changes. The API is before 1.0, and it freezes at 1.0. The one client, the
     web pages, changes with it.
9. A driver reports the space a print may fill at `/build_volume/x`, `/y` and
   `/z`, in millimetres. The driver carries the definitions of the models it
   knows, from public sources it cites. The host keeps no table of machines.
   A machine the driver has no figures for reports nothing, and its volume is
   unknown.

## Why `/tool/<i>` and not `/slot/<i>`

The Prusa driver wrote `/slot/<n>` from 1 before this record. Moving those
paths to count from 0 under the same name would let a reader written against
the old numbering read the wrong tool and raise no error. A new name makes
that reader find nothing, which it can see. "Tool" is also the word G-code and
every vendor use: an XL toolhead, an MMU slot and a Klipper extruder are all
selected with `T<i>`. "Slot" is the Prusa firmware's word for the same thing.

## enforcement

| gate | what it holds |
|---|---|
| `connectorapi.ToolIndex` and its test | only the canonical spelling of an index inside the bound is a tool |
| tests in `internal/connector/xbuddy/tools_test.go` and `slot_test.go` | the firmware's 1-based keys and active slot become indexes from 0, and `---` becomes the empty material |
| `tools_sim_test.go` in the same folder | the simulator, which speaks the firmware's numbering, arrives on the model, and no 1-based path is written |
| tests in `packages/svelte/src/domain/state-paths.test.ts` | a page reads the tools from 0, and reads none from the old paths |

## rejected

| option | why not |
|---|---|
| keep counting from 1, as the Prusa screen does | G-code, slicer files and Klipper count from 0, and a file's `T0` would then be tool 1 |
| keep the `/slot/<n>` name and renumber it | a reader of the old numbering would read the wrong tool and see no error |
| a host table that maps each vendor's numbering | the host would branch on vendors, and a driver from outside this repository could not add its own |
| the absent path for "nothing loaded" | absent already means unknown, and the two lead to different answers |
| a table of build volumes in the host | a driver from outside this repository could not add its machines, and the host would branch on vendors |

## open items

- A bed and sheet table per model is not built. What a sheet is, and how a printer would report the sheet on it, needs a study of its own
