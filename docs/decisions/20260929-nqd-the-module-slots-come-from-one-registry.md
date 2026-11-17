---
title: "ADR-20260929-nqd: the module slots come from one registry"
description: "sdk/manifest/slots.go is the one table of slots, screens, areas, forms, subjects and icons; make generate-slots writes every other list from it, and what still reads the table by hand or is checked against it."
date: 2026-09-29
status: accepted
tags:
  - adr
  - frontend
  - svelte
  - modules
  - sdk
---
# ADR-20260929-nqd: the module slots come from one registry

- status: accepted. It extends [ADR-20260911-4p6](20260911-4p6-a-module-extends-the-ui-through-one-library-and-four-slots.md)
- date: 2026-09-29

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Builds on [ADR-20260911-4p6](20260911-4p6-a-module-extends-the-ui-through-one-library-and-four-slots.md), [ADR-20260914-244](20260914-244-the-library-is-layered-and-an-import-only-goes-down.md)

[ADR-20260911-4p6](20260911-4p6-a-module-extends-the-ui-through-one-library-and-four-slots.md)
rule 47 grows the slot set by a rule here and a row in a table. Before this
record, adding `device-section`, `device-tab` and `device-action` meant
editing about six hand-kept lists. They were the TS `SLOT_NAMES`, the Go
`contributesKeys` and `Contributes` struct, and `tree.go`'s refusal text. The
rest were `ResourceContributes` and its mapping in `resourcehost`, the
generator's own `slotNames`, `slots()` and `fill`, and the scaffold `Plan`
fields and templates. This record puts the table in one place,
`sdk/manifest`, on the pattern the repo already uses for a closed
vocabulary: a table in Go, `cmd/generate-<x>`, a `-check` flag, and
`make <x>-check`.

## rules

1. The slot table in `sdk/manifest/slots.go` is the only list of slots,
   screens, areas, forms, subjects and icons. Every name is a named type
   used through its constants.
2. `make generate-slots` writes the wire struct, the mapping, the library's
   `slots/generated.ts`, the slot reference and the guide region. Nothing
   else keeps a separate list of slots.
3. `cmd/new-module` and the manifest parser read the table directly.
4. A slot's layout in `SlotOutlet` and its template in `scaffold` are
   written directly, not generated. The compiler (`LAYOUT` is `satisfies
   Record<SectionSlot, …>`) and `TestEverySlotHasATemplate` refuse a slot
   without them.
5. Every screen in the table has a draw function in
   `slot-outlets.svelte.test.ts`, and every slot is found there on its
   screen.
6. How to add a slot, a screen, a subject and a form is the reference
   page's four recipes, kept there and not duplicated here.

## enforcement

| rule | gate | state |
|---|---|---|
| 1 | `go test ./sdk/manifest/`: the registry rules (`slots_test.go`) | exists |
| 2 | `go run ./cmd/generate-slots -check`, joined by `make slots-check` | exists |
| 3 | `go test ./cmd/new-module/ ./sdk/manifest/` | exists |
| 4 | the `LAYOUT` type in `slot-outlet.svelte`; `TestEverySlotHasATemplate` in `internal/app/scaffold` | exists |
| 5 | `slot-outlets.svelte.test.ts`, typed from `SlotsOf<Screen>` so a screen with no draw function fails the typecheck | exists |

Rule 2's chain reaches further than `slots-check`: `openapi-check` and
`client-sdk-check` also run, and either one catches a stale `openapi.json` or
TS client left over from `make generate-slots`.

## rejected

| option | why not |
|---|---|
| code generation from a JSON file | the repo's vocabularies are Go tables |
| a TS test that compares two hand lists | it still means six edits |
| `map[string]` contributes on the wire | openapi would lose the typed lists |

## consequences

- `sdk/manifest/slots.go` holds `SlotName`, `Screen`, `Area`, `Form`,
  `Subject` and `Icon`, each a named type with constants, and the `Slots`
  and `Icons` tables.
- `cmd/generate-slots` writes
  `internal/transport/http/httpapi/contributes_gen.go`,
  `internal/app/resourcehost/contributes_gen.go`,
  `packages/svelte/src/slots/generated.ts`,
  `docs/src/content/docs/reference/module-slots.md`, and the generated
  region of `docs/src/content/docs/writing-a-module.md`.
- `sdk/manifest`'s own `Contributes` type is not generated: it stays
  `Pages`, `Sections map[SlotName][]Section` and
  `Actions map[SlotName][]Action`, and the parser walks the table by form.
- `docs/vocabularies/slots.json` does not exist. Nothing reads it, and a
  vocabulary file brings a version and a baseline with it. It is added when
  a reader needs it.
