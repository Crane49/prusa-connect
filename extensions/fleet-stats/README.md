# Fleet Stats

A Prusa Connect module that is only a browser bundle: the
host verifies it against a digest, serves it, and never runs it.

It reads the prints the core keeps, `GET /api/v1/jobs`, and shows them in four
places:

- a **Statistics** page (icon `chart-line`) over 7, 30 or 90 days, for the
  whole fleet, one printer or one tag. It holds five KPI cards with their
  change against the previous period and a trend day by day. Under them are
  an hour-by-weekday utilization heatmap, the files printed and a table of
  failures per printer. The address keeps the period, the printer and the tag
- four **dashboard cards**: fleet figures, the daily trend, the heatmap and
  recent failures. Each counts the period and the printers its settings name
  and links to the page showing the same
- one line on each printer's card in the listing: success rate, jobs and days
- a section at the foot of a printer's Status tab with its own heatmap row,
  its recent failures and a link to the page narrowed to it

Each period is read once, however many screens show it, and a summary is worked
out once per period and scope. The core keeps every print for as long as it
keeps the printer and answers only the printers the person may see, so the
module keeps nothing of its own.

The numbers are worked out in a pure domain file, `ui/src/domain/`, so they can
be tested without a browser. It is the reference for a module that reads the
API.

## What it needs

Prusa Connect 0.0.1-alpha.8 or later, which keeps how each print ended. It
needs no `history:` resource. `make demo` seeds three weeks of prints, so the
module has something to show at once.

| path | purpose |
| --- | --- |
| `module.yaml` | the manifest: the kind and the version, and the `ui` block with the ceiling this bundle may register into |
| `CHANGELOG.md` | what an operator sees change when they upgrade this module |
| `Makefile`, `scripts/check.sh` | the gate `make -C extensions check EXT=fleet-stats` runs |
| `.vscode/extensions.json` | the editor extensions for the languages in here |
| `ui/src/index.ts` | the bundle's entry: it receives the host object and registers what the manifest declares |
| `ui/src/page.svelte` | `fleet-stats`: a page of its own, with an entry in the sidebar |
| `ui/src/device-card.svelte` | `fleet-stats`: one line on each printer's card in the listing |
| `ui/src/device-section.svelte` | `fleet-stats`: a section at the foot of each printer's Status tab |
| `ui/src/cards/` | the four dashboard cards, their frame and the settings they read |
| `ui/src/view/` | what the page and the cards share: figures, trend chart, files table, failures list, page address |
| `ui/src/data/` | reading the prints, one read per period, and the store every screen shares |
| `ui/src/styles.css` | this module's own Tailwind. Four lines, and only one emits |
| `ui/src/locales/en.json` | every text this module draws. No component writes an English string |
| `ui/src/locales/cs.json` | the Czech catalogue; a key left out falls back to English |

## Installation

To see it, run this in the checkout. `-dir` is this folder:

```bash
go run ./cmd/new-module -start demo -dir extensions/fleet-stats
```

It starts the demo on a free port with this module installed, and this
module's dev server in front of it. It prints the address to open and the
account to sign in with. `-start app` starts the real app instead, which asks
for an account on its first run. Ctrl+C stops everything it started.

## Running the tests

From the repository root:

```sh
make -C extensions check EXT=fleet-stats
```

It lints, type-checks and tests the bundle in Chromium, then builds the
release and installs it with `install.sh`, the same way a person does.
`pnpm run check` in `ui/` runs the first half alone.

## Iterating

`-start` runs the module's own dev server, `pnpm dev` in `ui/`. Open the
address it prints, not the core's. The page is the core's app and this module
is served from `src/`, so an edit to a component shows up without a reload.
Started directly, `pnpm dev` reads the core's address from `PCL_CORE_URL`,
`http://localhost:8080` by default.

Install again after changing `module.yaml`: the core reads what a module
contributes when it is installed.

## What the pieces are

The bundle's build writes `ui/dist`, and that directory is already the release
layout: `module.yaml` at its root and the entry at `ui/index.js`. `-from ui/dist` is what the install
reads. A `checksums.txt` is optional and verified when it is there; a release
you just built has nothing to prove against.

```bash
prusa-connect module add github.com/prusa3d/fleet-stats@0.1.5 -from ui/dist
prusa-connect module list
prusa-connect module remove github.com/prusa3d/fleet-stats
```

Without `-approve` the declaration is written switched off, and the command
prints what the module asked for so an operator can read it first.

## The ceiling

`contributes` in `module.yaml` is the whole of what this bundle may register.
A `host.register` call the block does not declare throws, and the host drops
this module whole. The generator wrote both sides from one answer:

- `pages: fleet-stats` ← `host.register.page('fleet-stats', Page)`
- `device_card: fleet-stats` ← `host.register.deviceCard('fleet-stats', DeviceCard)`
- `device_section: fleet-stats` ← `host.register.deviceSection('fleet-stats', DeviceSection)`

Add one to the manifest and the other to the entry, or neither.

## Styling

`ui/src/styles.css` carries four directives and only the third emits anything.
They let this module name any Tailwind utility the library's theme defines,
without shipping Tailwind's preflight into the host page. The page behind them
is `styling-a-module` in the core's documentation.

## Texts

Every text a person reads is a key in `ui/src/locales/en.json`, read with
`host.t('<key>')`. The host merges that file under this module's namespace, so
a key here may be one the core already names. Copy the file to `<tag>.json` to
translate it.

## Troubleshooting

| what you see | what it means |
| --- | --- |
| `module add` refuses the release | `ui/dist` is not the release layout: `module.yaml` at its root, the entry at `ui/index.js` |
| the module is listed and switched off | `module add` ran without `-approve`. Read what it printed, then switch the instance on |
| the sidebar entry is missing | the app refused the bundle. Open the browser console: it names each registration that `contributes` does not declare |
| a screen shows a key instead of a sentence | the key is missing from `ui/src/locales/en.json` |
| a change does not show | the install was not run again. Every change is a new install |
