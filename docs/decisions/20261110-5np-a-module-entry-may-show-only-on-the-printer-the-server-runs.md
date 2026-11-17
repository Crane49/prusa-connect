---
title: "ADR-20261110-5np: a module entry may show only on the printer the server runs on"
description: "An entry of a slot handed a device may carry a devices selector. With resident: true the web app shows it only for a device whose record has resident: true."
date: 2026-11-10
status: proposed
supersedes: []
amends: [ADR-20260911-4p6]
tags:
  - adr
  - extensions
  - frontend
---
# ADR-20261110-5np: a module entry may show only on the printer the server runs on

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Amends [ADR-20260911-4p6](20260911-4p6-a-module-extends-the-ui-through-one-library-and-four-slots.md) | Builds on [ADR-20261103-szc](20261103-szc-a-resident-printer-cannot-be-retired.md), [ADR-20261027-wvj](20261027-wvj-a-module-extends-how-the-core-reads-a-file-and-draws-a-file.md)

A module entry for a printer may carry `devices: {resident: true}`. The web
app then shows it only on the printer the server runs on, whose device record
has `resident: true` (ADR-20261103-szc).

A server can run on the printer itself. A module there may add a tab that
shows the printer's own local web interface. Only the machine the server runs
on has that interface. On any other printer the same server reaches, such as
an xBuddy printer that connects over the network, the tab would show the wrong
machine.

`devices` is a selector: it names the devices an entry is shown for. Today it
reads one condition. A later condition, such as the device's kind or a
capability, is one more key in the same block, not one more key on the entry.

## rules

1. An entry of a slot handed a device may carry a `devices` block. Those
   slots are `device-card`, `device-section`, `device-tab` and
   `device-action`.
2. The block reads one key, `resident`, with the value true. The web app then
   shows the entry only for a device whose record has `resident: true`. On
   every other device there is no tab, section, card line or menu item.
3. A link to such a tab on another device opens Status, the tab labelled Now,
   like a link to an unknown tab (ADR-20260911-4p6 rule 45).
4. While the web app has no record of the device yet, it shows no entry that
   carries a `devices` block.
5. An entry that leaves out `devices` is shown for every device. So is one
   that writes the key with no value, YAML `devices:`, `~` or `null`, as for
   every block in a manifest.
6. A slot handed a file or nothing refuses the key. On a slot that takes it,
   the manifest is refused when the block is not a mapping or names no
   condition, as `{}` or `resident` with no value do. It is also refused when
   `resident` is anything but true, false included, or when the block carries
   another key.
7. `GET /api/v1/resources` returns the block as `devices` on the entry, and
   leaves it out when the entry carries none. The server keeps
   `ui.contributes` complete (ADR-20260911-4p6 rule 32), and the web app does
   the filtering.
8. The component still gets only the device id (ADR-20260911-4p6 rule 10).
   `SlotOutlet`, `moduleTabs` and `SlotActions` read the device record only to
   decide whether to show an entry.
9. A menu item runs for the device or file it was chosen for, even when the
   page shows another one before the module has loaded.
10. A further condition is a further key of the block. An entry is shown only
    for a device that meets every key the block carries. A web app that
    cannot read a key or a value counts it as not met, so it shows no such
    entry. A block that names no condition is not met either. The server
    never sends one, so it can only be a condition lost on the way. On the
    wire each key is optional, and a block leaves out a condition it does
    not name.

## enforcement

| rules | gate |
|---|---|
| 1, 2 | `TestADeviceSlotEntryTakesADevicesSelector` in `sdk/manifest` |
| 1, 6 | `TestOnlyADeviceSlotEntryTakesADevicesSelector` in `sdk/manifest` |
| 5 | `TestAnEmptyDevicesIsAMissingKey` in `sdk/manifest` |
| 7 | `TestAReadyAssetInstanceCarriesTheDevicesSelector` in `internal/app/resourcehost` |
| 7, 10 | `TestEveryConditionOfASelectorReachesTheWire` in `internal/app/resourcehost` |
| 2, 4, 5, 10 | `device-selector.test.ts` in `packages/svelte/src/slots` |
| 2, 4 | `module-tabs.test.ts` in `packages/svelte/src/slots` |
| 2, 4, 8 | `slot-outlet.svelte.test.ts` in `packages/svelte/src/slots` |
| 2, 4, 9 | `slot-actions.svelte.test.ts` in `packages/svelte/src/slots` |
| 2, 3, 4 | `frontend/src/lib/screens/printer-detail.svelte.test.ts` |
| 2 | `frontend/src/lib/screens/slot-outlets.svelte.test.ts`, the card line case |

## rejected

| option | why not |
|---|---|
| a condition in the bundle, such as a function passed to `register` | the tab strip is built from the manifest, so the tab would appear before the bundle loads and vanish after |
| a component that shows nothing on another device | it leaves an empty tab |
| `hidden_contributions` | it hides an entry on every device |
| a boolean key on the entry, such as `resident_only` | each later condition would be one more key on every entry, read and documented on its own |
| the key name `device` | on an entry it looks like a fact about the entry. `devices` names the set the entry is shown for, as `file_types` does |
| the key name `device_selector` | longer, and unlike `file_types` |
| reading `resident: false` as a printer the server does not run on | no module needs it, and refusing it keeps that meaning open. Giving it one later also changes the Go field `Resident` |
| moving `file_types` into a selector | it selects files, not devices, and published modules carry it. A selector for files is a decision of its own |
| filtering on the server | the server keeps `ui.contributes` complete, and the file filter of ADR-20261027-wvj rule 17 runs in the web app too |
