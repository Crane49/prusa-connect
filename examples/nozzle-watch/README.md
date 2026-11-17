# The nozzle-watch module

A module that is only a browser bundle, with no server process. It fills all
four of the slots ADR-20260911-4p6 gives a module for a printer. They are one line
on the Printers page card, a section at the foot of the Status tab, and a
tab of its own. A fourth is an item in the action menu at the top of the
page.

It is the second proof extension of [ADR-20260911-4p6](../../docs/decisions/20260911-4p6-a-module-extends-the-ui-through-one-library-and-four-slots.md).
`examples/spools` proves a module can add a screen of its own.
This one proves the other half: a module can add to a screen the app already
has, without running a process.

## What it proves

- **one bundle, no process.** There is no Go half, no wire handshake and no
  route. The host verifies a directory against a digest and serves it
- **one declaration.** One `asset` instance is the whole module. A module
  with a process half is one line too, carrying its bundle beside its binary
- **it installs like any other module.** The same manifest, the same digest,
  the same `enabled` flag
- **it renders inside the app's own page.** The card line, the section, the
  tab and the action menu item all appear after the core's own. Each one is
  handed only the printer's id (ADR-20260911-4p6 rule 10)
- **it makes no request to read.** The fleet is already on the host object.
  The app loads it once and updates it from the event stream, so four
  hundred printers with this module's card line and section make zero extra
  requests
- it keeps state of its own: `mark-changed` writes to the module's own
  store, scoped to its namespace, and `nozzle-tab` reads what it wrote

## Build it and run it

```bash
bash scripts/check-example-spools.sh
make demo DEMO_ARGS="-config bin/spools/config.yaml -dir bin/spools/demo"
```

The script's generated config declares `resources.db.main`, which is what
`mark-changed` writes to; `make demo` on its own declares no `db` backend, so
the mark does not survive a reload without it.

The script builds both proof extensions, runs their tests, lays each out as a
release and installs it:

```bash
prusa-connect module add github.com/acme/nozzle-watch@1.0.0 -from bin/spools/release/nozzle-watch
```

The install records the digest of the directory it copied, so nothing in this
repository carries one: every build moves it.

The app is `frontend`. Point its dev server at the demo and open it:

```bash
cd frontend && PCL_CORE_URL=http://127.0.0.1:8080 pnpm run dev
```

Sign in with the account the demo printed. Each card in the fleet shows the
nozzle-watch line, and each printer's page shows the section, the tab and the
action menu item.

## Key files

| path | purpose |
| --- | --- |
| `module.yaml` | the manifest, which the install copies into the bundle root: the entry, the library range, and the four slots |
| `ui/src/index.ts` | the bundle's entry: it holds the host and registers the four |
| `ui/src/host.ts` | where the host object is held, so a component can read the fleet |
| `ui/src/nozzle.ts` | the device's state tree paths that carry the temperature and target readings, shared by `nozzle-section` and `nozzle-line` |
| `ui/src/nozzle-line/nozzle-line.svelte` | the card line: the diameter and material a driver reports, or the temperature reading past neither |
| `ui/src/nozzle-section/nozzle-section.svelte` | the section, on the library's primitives and the domain's heating rules |
| `ui/src/nozzle-section/words.ts` | the word and the colour for each heater state |
| `ui/src/nozzle-tab/nozzle-tab.svelte` | the tab: the date `mark-changed` last wrote, or that it never ran |
| `ui/src/mark-changed.ts` | the action menu item: writes today's date to the module's own store |
| `ui/src/test/host.svelte.ts` | the stub host: the library's own registry, a fleet a test writes, and an in-memory store |

## The manifest is the ceiling

`contributes` names four entries, one per slot: `device_card`, `device_section`,
`device_tab` and `device_action`, each carrying `nozzle` or `mark-changed`.
This is not a description of what the module intends to do. It is the
complete list of what the module is allowed to do:

```yaml
ui:
  entry: ui/index.js
  library: ">=0.1.0 <1.0.0"
  contributes:
    device_card:
      - id: nozzle
        title: Nozzle
    device_section:
      - id: nozzle
        title: Nozzle temperature
    device_tab:
      - id: nozzle
        title: Nozzle
    device_action:
      - id: mark-changed
        title: Mark nozzle changed
        icon: wrench
        destructive: false
```

A fifth registration, such as `register.deviceSection('other', …)`, is refused,
and the host refuses the whole module rather than running it with one
registration missing (ADR-20260911-4p6 rule 21). A section's and a tab's title come
from the manifest too, so an operator can read them before installing and the
running code cannot change them. Every one of these behaviours has a test.

## What the section and the card line read

A device record has a `state` field: a tree of values keyed by path. The
module reads the first head of the printer's tool tree (ADR-20261023-gna). Its nozzle
temperature is at `/tools/0/nozzle_temp` and its target at
`/tools/0/nozzle_target_temp`. The host writes each telemetry sample into the
tree under the metric's path. The card line also reads, when a driver reports
it, the nozzle diameter at `/tools/0/features/nozzle_diameter` and the loaded
material at
`/tools/0/material_package_instance/package/material/abbreviation`. Without
both it falls back to the same temperature reading the section shows. Every reading checks the type of
the value before using it, because the core publishes no schema for the tree;
each driver declares the paths it reports.

Whether a reading counts as heating, at temperature, cooling or off is not
decided here. That rule is in the library's domain layer, at
`@prusa/connect-ui/domain`, so this module and the core's own screens
answer the question the same way. The edges are where that pays. A nozzle
holding 214.6 against a 215 target has arrived, and a heater told to go to
zero is off rather than cooling once it is under the floor.

## What `mark-changed` writes

`host.store` takes one key per record, and a key is one DNS-1123 label. A
device's id is not one, so every printer's mark lives under the module's one
key, `changed`, as a JSON record of `{ deviceId: date }`. The action menu item
adds today's date to it; the tab reads the same record back. Nothing here
reaches the core beyond the store's own write: the mark is this module's
record, not a field the core's device model carries.

## What is not here

| gap | what it means |
| --- | --- |
| nothing signs the bundle | the digest is integrity, not provenance |
| the section shows one heater | the bed and a second tool head would be two more lines each |
| the mark carries no note | a person cannot say why the nozzle was changed, only when |
