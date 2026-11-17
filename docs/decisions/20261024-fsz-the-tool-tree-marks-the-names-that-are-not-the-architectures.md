---
title: "ADR-20261024-fsz: the tool tree marks the names that are not the architecture's"
description: "Four parts of the tool tree are Prusa Connect's own and not the public Prusa architecture's: the active tool, the enabled member, the tool mapping and the build volume. Each place that names them says so."
date: 2026-10-24
status: accepted
amends: [ADR-20261023-gna]
tags:
  - adr
  - sdk
  - connectors
---
# ADR-20261024-fsz: the tool tree marks the names that are not the architecture's

- status: accepted. ADR-20261024-34x renames `/tool_mapping/<n>` to
  `/gcode_tool_map/<n>`
- date: 2026-10-24

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Amended by [ADR-20261024-34x](20261024-34x-each-tool-says-whether-it-is-active.md) | Amends [ADR-20261023-gna](20261023-gna-a-printers-tools-are-a-tree-of-heads-and-feeder-slots.md) | Builds on [ADR-20260826-c77](20260826-c77-one-name-for-one-thing-on-the-wire.md)

ADR-20261023-gna names the tool tree after the public Prusa architecture's
[printer configuration](https://specs.prusa3d.tech/architecture/#/printer_configuration).
Most of its names are the architecture's, and four parts are not. A reader of
ADR-20261023-gna can take all of them for the architecture's. A client author who also
reads a Prusa printer or Prusa Connect then looks there for a name that does
not exist. This record says which parts are Prusa Connect's own. It changes no
path and no value.

## rules

1. These parts of the tool tree are Prusa Connect's own. The architecture has
   none of them:
   - `/active_tool`, one id for the whole machine. The architecture marks each
     tool in its telemetry as `active` or not, and more than one tool can be
     active at once
   - `/tools/<id>/enabled`. It is how the state tree holds the architecture's
     null tool, because the tree cannot hold a null (ADR-20261023-gna rule 8). The
     API serves `null` again, as the architecture does
   - `/tool_mapping/<n>`, the tool a file's `T<n>` selects. It is not the
     `tool_mapping` argument of a print command. That argument goes from the
     caller to the printer and says which tool to use for each filament
   - `/build_volume/x`, `/y` and `/z`. A driver derives them from the model or
     from the machine's configuration
2. `features/nozzle_high_temp` is a name of the architecture that its public
   pages do not list yet. No Prusa firmware sends it.
3. Every other name ADR-20261023-gna rule 3 lists is the architecture's, with its
   meaning.
4. Each place that lists the tool paths marks the parts of rules 1 and 2: the
   doc comments in `sdk/connectorapi/tools.go` and
   `sdk/connectorapi/buildvolume.go`, the generated SDK reference, and
   [writing a driver](../src/content/docs/writing-a-driver.md#report-the-tools).
5. A source that core cites for the architecture is its public site,
   `https://specs.prusa3d.tech/architecture/`. A page there is linked with the
   `#/` the site needs, such as `#/printer_configuration`.

## enforcement

| gate | what it holds |
|---|---|
| `make sdk-reference-check` | the generated SDK reference carries the doc comments that mark the parts |
| review | a new tool path says whether it is the architecture's |

## rejected

| option | why not |
|---|---|
| edit ADR-20261023-gna | an accepted ADR is never edited ([decisions](README.md)) |
| rename the four parts now | that is a contract change and needs its own record. This one only marks them |
| leave the marks to the code comments | a client author reads the driver page and the API, not the source |
