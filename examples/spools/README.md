# The spools module

A module with both halves: a Go process that serves two routes, and a browser
bundle that adds a sidebar item and a page to the app. It tracks filament
spools per device in memory. Storage is not the point of the example; how the
two halves work together is.

It is the first proof extension of [ADR-20260911-4p6](../../docs/decisions/20260911-4p6-a-module-extends-the-ui-through-one-library-and-four-slots.md).

## Build it and run it

```bash
bash scripts/check-example-spools.sh
make demo DEMO_ARGS="-config bin/spools/config.yaml -dir bin/spools/demo"
```

The script builds both halves and runs their tests. It lays the result out as
a release and installs it with `prusa-connect module add`. Then it starts
the demo and drives the module through the API.

The install records the digest of everything it copied, so nothing in this
repository carries one: every build moves it.

The app is `frontend`. Point its dev server at the demo and open it:

```bash
cd frontend && PCL_CORE_URL=http://127.0.0.1:8080 pnpm run dev
```

Sign in with the account the demo printed. "Spools" appears in the sidebar
after Fleet and Settings. Opening it loads the bundle.

## Key files

| path | purpose |
| --- | --- |
| `main.go` | the Go half: the handshake, the lifecycle set, and the two routes |
| `openapi.json` | the fragment describing those routes, with every path relative to the mount |
| `module.yaml` | the manifest, with the `ui` block. It is copied into the bundle root |
| `routes_test.go` | both routes driven as `routes.serve` calls, the way the host drives them |
| `manifest_test.go` | the manifest read the way the host reads it, and its library range held against the library |
| `client/` | the typed client, written by `generate-client-sdk -module` from the fragment |
| `ui/src/index.ts` | the bundle's entry: it builds the shelf and registers the page |
| `ui/src/spools.ts` | the module's routes, bound onto the client the host handed over |
| `ui/src/shelf.svelte.ts` | what the page reads and what it writes through |
| `ui/src/spools-page/spools-page.svelte` | the page, on the library's `PageLayout` and primitives |

## One declaration carries both halves

`module add` writes one line into the machine-owned overlay, and that line
carries both halves (ADR-20260911-4p6 rule 30):

- `runtime: service` names the process, with the directory the binary is in
  and the digest of the file
- `service.assets` names the bundle beside it, with the digest of the whole
  directory

Both are served under one namespace, `com.github.aux.acme.spools`. The routes
answer at `/api/v1/modules/com.github.aux.acme.spools/spools`, the files at
`/api/v1/modules/com.github.aux.acme.spools/ui/`, and one `enabled` flag
switches both. The namespace is the module path reversed, which is
`github.com/acme/spools` here. The author never writes it.

The instance is declared under the `sink` kind, which is what the example
service module uses. The server warns that a declared sink is receiving no
events. That is correct. A module reads this server's events by subscribing
to the event API, and this one subscribes to nothing.

## Install it yourself

```bash
prusa-connect module add github.com/acme/spools@1.0.0 -from bin/spools/release/spools
prusa-connect module list
prusa-connect module remove github.com/acme/spools
```

`-from` names the release the build produced. Without `-approve` the
declaration is written switched off, and the command prints what the module
asked for so you can read it before you say yes.

## What the page holds

The bundle receives one host object and keeps no other state (ADR-20260911-4p6
rule 18). It calls the module's own routes through `host.client`, the app's
SDK in session mode, so the credential is the cookie the browser already
has. There is no `fetch` in this module and no token in the page. The browser
code contains no server address either: `bind()` builds every path from the
namespace the fragment was generated under.

`host.principal.id` is who the core says is signed in, and the page shows it.

## What is not here

| gap | what it means |
| --- | --- |
| the rows are in memory | restarting the module empties the shelf. `examples/service-plugin` shows the host's record store |
| nothing signs the bundle | the digest is integrity, not provenance |
| the module publishes no event | a page open in a second browser does not see a spool the first one added until it reads again |
