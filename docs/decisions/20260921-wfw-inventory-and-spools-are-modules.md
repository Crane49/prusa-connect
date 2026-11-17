---
title: "ADR-20260921-wfw: inventory and spools are modules"
description: "The core holds no item table, no kind vocabulary and no inventory permission. A stock of physical things is a module that keeps its records through sdk/stateapi under a namespace of its own, and the materials extension is the reference."
date: 2026-09-21
status: accepted
tags:
  - adr
  - modules
  - inventory
  - authorization
  - extensibility
---
# ADR-20260921-wfw: inventory and spools are modules

- status: accepted
- date: 2026-09-21

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Builds on [ADR-20260827-wgc](20260827-wgc-an-extension-reaches-persistence-through-the-sdk.md), [ADR-20260827-wmv](20260827-wmv-a-module-is-mounted-and-an-adapter-is-constructed.md), [ADR-20260827-rwq](20260827-rwq-a-module-keeps-its-data-out-of-the-host-schema.md), [ADR-20260910-74m](20260910-74m-a-module-declares-what-it-depends-on.md), [ADR-20260911-4p6](20260911-4p6-a-module-extends-the-ui-through-one-library-and-four-slots.md)

A spool of filament, a build sheet and a nozzle are things an operator owns.
They move between machines, they run out, and they carry a price. This record
says where that list of things lives, which is a module and not the core.

The core stays the server that talks to printers. A stock is one of the things
a module adds to it.

## rules

### where inventory lives

1. Inventory is a module. The core defines no item table, no kind vocabulary,
   no inventory action and no inventory event type.
2. An inventory item outlives its attachment to one device. What a device
   reports about itself is device state and belongs to the device record, so a
   tool slot is not an item and a spool is.
3. The materials extension `extensions/materials` is the reference. It keeps a
   list of spools, what each one is made of, and how much filament is left on
   it.
4. A stock of another kind is another module, or a later kind inside one. None
   of it enters the core.

### where a module keeps its records

5. A module writes its records through `sdk/stateapi`, into the store the host
   gives it under a namespace of its own. No module owns a table in the host
   schema.
6. A module names in `requires` the host methods it cannot run without. The
   host does not start a module whose record backend is absent, and the
   instance shows which method is unserved.
7. A module bounds how much it keeps, with a setting its manifest declares and
   a default the operator can raise. `max_spools` is that bound in the materials
   extension.

### who may read and write a stock

8. The host authorizes every call before the module sees it. It takes
   `view_resource` on the instance before a read and `operate_resource` before
   a write. There is no grant on one item.
9. A module checks no permission itself. It reads the name of the person off
   the call the host makes, and never out of an HTTP header.
10. A module that acts only for the person who called it asks the host for no
    permission of its own, and its manifest carries no `permissions` list.

### the data a stock holds

11. A module owns its own schema and its own version. The materials extension
    builds its materials on the OpenPrintTag catalogue.
12. A module reads and writes the file formats the community already uses, so
    an operator can bring a list in and take it out again. No core code knows
    any of those formats.
13. A print that is cancelled subtracts an estimate from the spool. Until the
    firmware reports what a print really used, an estimate is what a module
    has.

### what a person sees

14. A module ships its own page through the UI slots of
    [ADR-20260911-4p6](20260911-4p6-a-module-extends-the-ui-through-one-library-and-four-slots.md),
    and its own OpenAPI fragment describing its routes.
15. A module publishes its own event types through the event stream the host
    hands it.

## enforcement

| rule | gate | state |
|---|---|---|
| 1 | `make resource-kinds-check` and `make event-types-check`, both directions | exists |
| 5, 6 | a module test over a host that serves no record backend | owed |
| 7 | `manifest_test.go`, which fails when the manifest and the readme differ | exists |
| 8, 9 | `openapi_test.go`, which compares the document with the routes the module registers | exists |
| 12 | a round trip through each supported format, in the module's own tests | owed |
| 14 | the bundle gate of ADR-20260911-4p6 | exists |

## rejected

### one item table in the core

The core is the part that stays the same on every machine, and a stock of
things is not part of talking to a printer. A table in the core also fixes the
shape for everyone. A print shop counting resin and a school counting build
sheets want different columns, and a module gives each of them their own.

### a table per inventory kind

Two hand-written migration files per engine per kind, and a module owns no
table in the host schema anyway.

### a gated kind vocabulary in the core

A vocabulary in the core is a published surface the core then has to version
and serve. The kinds a stock holds are the module's own business, and a second
module with its own kinds costs the core nothing.

### a per-item authorization object type

Principals times items, recompiled inside every tuple write. Two thousand
spools and fifty principals is a hundred thousand rows, and the grant on the
instance answers the same question.

### a stock inside the core's device records

Nothing about a spool survives being unplugged from a printer, and nothing
about a tool slot survives the printer. Joining the two puts a shopping list
inside the record of a machine.

### reading which spool is loaded from telemetry

Firmware reports a material name and no identity, so the join is asserted by an
operator or read from a tag on the reel.

### a sync with one filament program in the core

The import and the export are the whole join, and a module doing them is the
test that the formats are enough.

## open items

- The materials extension does not subtract filament when a print finishes, and it
  does not record which spool is loaded on which printer. Both are rows on
  the old backlog.
- Whether another module keeps a stock of its own is a question to its
  authors. Two stocks with two models would give an operator two answers about
  one shelf.
- Rule 13 waits on the firmware reporting what a print used.
