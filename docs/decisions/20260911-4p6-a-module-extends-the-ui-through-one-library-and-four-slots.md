---
title: "ADR-20260911-4p6: a module extends the UI through one library and four slots"
description: "One Svelte library at packages/svelte that the core's app and every module draw with, the slots a module fills and the pages it adds from scratch, a ui block in the manifest, a host object that carries the SDK in session mode and no token, and an asset module served by the core."
date: 2026-09-11
status: accepted
tags:
  - adr
  - frontend
  - svelte
  - modules
  - sdk
  - security
---
# ADR-20260911-4p6: a module extends the UI through one library and four slots

- status: accepted. It answers the eight questions of earlier research
- date: 2026-09-11

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Rule 12 and the rejected rows for a `dashboard` slot and a settings schema are superseded by [ADR-20261008-sww](20261008-sww-the-home-page-is-a-dashboard-of-cards.md), which adds the slot `dashboard-card` with the dashboard screen | Builds on [ADR-20260827-wmv](20260827-wmv-a-module-is-mounted-and-an-adapter-is-constructed.md), [ADR-20260827-2bp](20260827-2bp-a-module-travels-as-a-verified-artefact.md), [ADR-20260829-cne](20260829-cne-the-front-door-serves-the-core.md), [ADR-20260909-c7x](20260909-c7x-a-client-sdk-is-generated-from-the-core.md), [ADR-20260910-74m](20260910-74m-a-module-declares-what-it-depends-on.md), [ADR-20260910-cqq](20260910-cqq-graphql-and-the-old-schemas-are-modules.md)

The core binary serves no page. The React app under
`frontend/` never imports the client SDK (finding 5), and a module has no UI
surface at all (finding 7). The SDK sends a bearer on every call and has no
session mode, so a page that obeys `frontend-structure.md` cannot sign a
request today (finding 13). Pull requests 184 and 185 hold a design system, a
domain layer, a slot shape and a runtime-sharing mechanism. This record takes
those and leaves the plugin API (section 8).

The brief asks for three things. An extension expands the app's native elements and creates new ones. It uses the
existing SDK and the existing session. It is easy to install and handle.

## rules

### the library

1. One Svelte package lives at `packages/svelte`, beside `packages/typescript`,
   and is `@prusa/connect-ui` on npm under OCL v1.
2. Its peer dependencies are `svelte` and `tailwindcss`. It depends on
   `@prusa/connect` and on no meta-framework (earlier research,
   recommendation 4).
3. It exports eight subpaths from one `exports` map (recommendation 5):

   | subpath | holds |
   |---|---|
   | `./tokens.css` | the Tailwind theme, every value in OKLCH, the dark variant |
   | `./domain` | the framework-free rules: lifecycle, heating, notification, printer state |
   | `./shell` | the app shell, the sidebar, the page layout, the footer, the theme store, the shared-screens list |
   | `./device` | the device card, the status badge, the heater rows, the telemetry icons |
   | `./forms` | text field, select, checkbox, field error, submit row, on native elements |
   | `./data` | the service, store and component pattern over the SDK, with one fleet store |
   | `./slots` | the slot names, their typed snippets, and the registration functions |
   | `./primitives` | button, badge, card, dialog, sheet, tooltip, popover, progress, skeleton |

4. A component enters the library when a second consumer needs it, and the
   second consumer is a second core screen or a module. A component one
   screen uses stays in that screen (recommendation 6).
5. Component tests run in browser-mode Vitest on the Playwright provider. The
   tokens test compiles the theme and checks WCAG contrast per theme, so a
   token registered in one place and missing from another fails
   (recommendation 8, finding 5).
6. `scripts/check-client-sdk-install.sh` packs and publishes both packages to
   the registry fixture. One consumer installs both and renders a page with
   the shell and the device card, and `publint` and the types check run
   there (recommendation 8).
7. The release workflow publishes the package on the core's tag, at the
   core's `VERSION`, with npm provenance, as ADR-20260909-c7x rules 1, 6 and 11
   publish the SDK. Its major moves when a slot's contract or a shared
   component's props change, under the `[sdk]` changelog marker
   (recommendation 19).
8. `.agents/rules/svelte-5.md` and `.agents/rules/frontend-structure.md` say
   Tailwind, on the model `frontend-css.md` gives, before the first component
   is written (recommendation 7, finding 10).

### what an extension may do

9. An extension extends what the app has and creates what it lacks. It
   fills any of seven slots, and it adds a whole page with a route of its
   own, drawn new on the library. Owner brief of 2026-09-11, recommendation
   1 and 10:

   | slot | where it draws | what it is handed |
   |---|---|---|
   | `sidebar` | after the core's own items, in the app's rail | nothing |
   | `page` | the page area, inside a boundary | nothing |
   | `settings` | a tab in the settings screen | nothing |
   | `device-card` | one line on each printer's card, in the listing's `cards` view | the device id |
   | `device-section` | the foot of a printer's Status tab, after the core's own | the device id |
   | `device-tab` | a tab of its own on a printer's page, after the core's tabs | the device id |
   | `device-action` | the action menu at the top of a printer's page | the device id |

10. A page slot implies a sidebar entry unless the manifest says `hidden`. A
    per-device slot is handed the device id and nothing else (finding 19).
    Every slot is optional, and a screen renders with none filled.
11. Every slot name is a string `./slots` exports. A manifest naming a slot
    the library does not publish fails the manifest gate. The set grows only
    by a rule added to this record (recommendation 16).
12. No `dashboard` slot exists in the first release, because the core has no
    dashboard screen (finding 1). It arrives with the screen.

### the manifest

13. The `ui` block of `module.yaml` carries `entry`, `library` and
    `contributes`, on the VS Code model (recommendation 11, finding 29):

    ```yaml
    ui:
      entry: ui/index.js
      library: ">=0.3.0 <1.0.0"
      contributes:
        pages:
          - path: spools
            title: Spools
            icon: layers
        device_card:
          - id: spool-status
            title: Loaded spool
        settings:
          - id: spools
            title: Spools
    ```

14. `entry` is a relative path that cannot climb out of the bundle root.
    `library` is the range of the library the bundle compiled against.
    `path` and every `id` are one label with no slash (finding 20).
15. The host derives the namespace and builds every href. The manifest names
    none, and a module's items sort after the core's (finding 21).
16. `sdk/manifest` validates the block with a test per key, and `identify`
    holds a module that ships both halves to one manifest (ADR-20260910-74m rule 9,
    recommendation 12).
17. `core.feature.ui` joins the capability vocabulary (ADR-20260910-74m rule 16), so
    a module that needs the slots requires it. The core holds `core` at
    install, and the frontend holds `library` before import (recommendation
    19).

### the host object

18. The bundle's default export receives one host object and holds nothing
    else (recommendation 14):

    | field | what it is |
    |---|---|
    | `client` | the app's `PrusaConnect`, in session mode |
    | `principal` | the signed-in person's id and grants, read from the core |
    | `register` | `page`, `deviceCard` and `settings`, each held to `contributes` |
    | `store` | a scoped key-value store under the module's namespace, over `state.*` |
    | `fleet` | the library's fleet store, read-only |

19. A module's typed client binds onto `client` (finding 14), so a page
    reaches its own routes and the core's through one credential it never
    sees.
20. The object carries no `fetch` and no token. In a browser the credential
    is the HttpOnly cookie (ADR-20260829-cne rule 7). Where a host holds a bearer, it
    hands the SDK a token provider closure. What a bundle may reach is what
    the person may reach, and the host authorizes every request on the
    instance. Owner brief of 2026-09-11, recommendation 15.
21. `register` is held to `contributes`. A registration the manifest did not
    declare throws, and a bundle that throws while registering is dropped
    whole (finding 21).
22. `networkAccess` stays a declaration. The page's CSP carries
    `connect-src 'self'` and allows module scripts from `'self'` only.
    Widening either takes the owner's word (recommendations 15 and 18).
23. A watch on one device is a selector on the fleet store in `./data`, fed
    by the SDK's `events()`. No bundle opens a second stream
    (recommendation 15).

### loading

24. The frontend reads `GET /api/v1/resources` and keeps the instances with
    a `ui` object. For each it checks `library` against its own version. Then
    it imports the entry URL as an ES module and calls its default export
    with the host object (recommendation 13).
25. Activation is lazy. The sidebar entry is drawn from the listing alone.
    The bundle is imported when its page first opens or its first slot first
    renders. Bundles load one at a time, so the sidebar order is stable
    (recommendation 13, finding 29).
26. A refusal is state the settings screen shows with its reason, never a
    console line. A page renders inside a boundary, and a section that
    throws is dropped without taking the card (findings 21, recommendation
    28).
27. Every bundle shares one Svelte runtime through an import map. The app
    emits `svelte/internal/client` and `svelte/internal/disclose-version` as
    chunks with `preserveSignature: 'strict'`. It prepends the map to
    `<head>`. The map carries two more entries, `@prusa/connect-ui` and
    `@prusa/connect` (findings 24 and 26, recommendation 20).
28. A bundle's build marks `svelte`, the library and the SDK external,
    compiles in runes mode and inlines its CSS. The library pins one Svelte
    version, and the release moves the app and the bundles together
    (finding 25).
29. A module brings no framework of its own in the first release: one
    Svelte, one library, one runtime. The custom element is a later lane
    (finding 27).

### installation and serving

30. An extension with a UI is a module with an asset bundle. It has one
    repository, one `module.yaml`, one release and one `module add
    repo@version`. It shows as one line in the resources listing with one
    `enabled` flag (recommendation 3). A module
    that runs no process is the asset kind of ADR-20260827-wmv rule 6 and ADR-20260827-2bp
    rule 11, verified by `Digest.Verify`
    (`internal/extension/wirehost/digest.go:40`) and never launched.
    - **Amended by [ADR-20261019-h76](20261019-h76-an-operator-may-switch-installed-modules-off.md) rules 9 and 10.**
      An installed module is not started or served while `core.modules_enabled`
      is `false`, whatever its flag says.
31. The bundle's files are served under `/api/v1/modules/<namespace>/ui/`,
    with the cache and content-type discipline `docsui.go` applies, and every
    request is authorized on the instance like a route (recommendation 12).
32. `/api/v1/resources` gains a `ui` object beside `openapi_url`
    (`internal/transport/http/httpapi/resources.go:215`). It carries the
    entry URL, the library range, the `contributes` block and the signing
    level found at install (ADR-20260827-2bp rule 5). It appears for an enabled and
    ready instance only (recommendations 12 and 17).
33. The core's own app is the first asset module, compiled into the full
    build and served at `/` through the `/*` mount
    (`internal/transport/http/httpapi/mount.go:33`). A path that names no
    file answers the entry document. The minimal build carries none
    (recommendation 22).

### the app

34. The app is plain Vite, client-only, with no server code and no
    SvelteKit.
35. The sign-in and the fleet ship first. The device card, the printer detail
    page and the settings screen follow (recommendation 21).
36. The Svelte app takes `frontend/` in one commit, which deletes the React
    app with its `AGENTS.md` and points the Makefile target at the new app.
    That commit lands when the shared-screens test passes on the Svelte app.
    No second application directory joins the tree.
37. The shared-screens list exports from `./shell`, and each app's test reads
    its own route directory against it (recommendation 26).
38. A closed product lives in its own repository, as an app on the same
    library and the same SDK. Its private business logic lives in private
    modules, and it hangs their UI through the same slots (recommendation
    25).

### what comes from pull requests 184 and 185

39. These pieces move in, each as one task, after this record is accepted
    (recommendation 9 and 29):

    | piece | from | into |
    |---|---|---|
    | the tokens and the contrast test | PR 185, PR 184 | `./tokens.css` |
    | the domain layer and its tests | PR 185 | `./domain` |
    | the app shell and the layout | PR 185 | `./shell` |
    | the slot shapes, the shared-screens list, the injected action | PR 185 | `./slots`, `./shell` |
    | the primitives, under rule 4 | PR 185 | `./primitives` |
    | the admission order and the nav rules | PR 185 | the plugin host in the app |
    | the import map plugin and its hook | PR 185 | the app's build |
    | the manifest shape, `contributes` in place of `widgets` | PR 185 | `sdk/manifest` |
    | `definePlugin` and the message shape | PR 185 | the host object |

40. The work starts from the library and moves the team's pieces in. It does
    not adopt pull request 185's tree.
41. These stay out: the fleet adapter, the mediated fetch, the static plugin
    index, the React tree and the closed product. The mediated fetch refuses
    the host's own origin, so a module's page cannot reach its own routes
    (finding 22), and that contradicts ADR-20260909-c7x rule 9.
42. The four documents in the two pull requests stay where they are.
    Earlier research records what was read from them, with the commit
    sha and the line numbers, and that record is what survives the
    branches. A copy of another author's prose cannot be held to the
    writing gates without editing it, so none is made. An agent touches
    neither pull request; a person tells the team (owner ruling of
    2026-09-10).

### the device slots

43. `device-card` draws one line per registration on each printer's card in
    the cards view of the listing. The host cuts it to one line and gives it
    no heading.
44. `device-section` draws a section with its title at the foot of a
    printer's Status tab.
45. `device-tab` draws a tab after the core's tabs, routed at
    `/printers/<id>/<namespace>.<id>`. An unknown tab opens Status.
46. `device-action` is data: the manifest gives its title, icon and whether
    it is destructive, and the bundle registers only `run(deviceId)`. The
    host draws it in the printer page's action menu, disables it while
    `run` is pending, and shows a rejection as a toast. It shows no
    confirmation.
47. The set of slots is the table ADR-20260929-nqd names. Rule 11 is met by a rule
    here and a row there, in the same change.
48. The title's count is the count of the first four slots. The set is now
    ten: the seven of rule 9, `dashboard-card` of ADR-20261008-sww, and the two of
    rules 49 and 50.
49. `file-action` has the form of `device-action` and sits on the storage
    screen. Its subject is a file reference, `<storage>:<path>`, the name of
    a storage of this server, a colon and the file's path in it. A storage
    name holds no colon. The host draws it in the menu of each file's row in
    the storage listing and in the menu at the top of the file's page. It
    never draws one for a folder.
50. `print-section` is a section in the print dialog, below the list of
    printers. It is drawn only for a file on a storage of this server, and it
    is handed the file reference of rule 49. A section reads the printers
    ticked in the dialog, and a function that closes it, through
    `printDialogContext()`. Its props stay the subject alone, so rule 10
    still holds.

## enforcement

| rule | gate | state |
|---|---|---|
| 4 | review, against the second-consumer question | no gate |
| 5 | the tokens test in `packages/svelte`, under `make frontend-check` | owed |
| 6 | `scripts/check-client-sdk-install.sh`, the fourth consumer | owed |
| 7 | the publish job in `.github/workflows/release.yml` | owed |
| 8 | the two rule files read Tailwind; `make style-check` holds the prose | owed |
| 9, 43–50 | `make slots-check`, and `slot-outlets.svelte.test.ts` under `make frontend-check` | exists |
| 11, 14, 16 | `sdk/manifest` tests, one per key | owed |
| 17 | `capabilities-check`, unchanged, once the key exists | exists |
| 20, 21 | `make frontend-check` greps for `fetch(`, `XMLHttpRequest`, `EventSource` and `/api/v1` | owed |
| 21, 26 | a plugin host test: an undeclared registration throws and the module is dropped | owed |
| 30 | the digest test in `wirehost`, extended to the asset kind | owed |
| 31 | an `httpapi` test: a file under the prefix is refused without the grant | owed |
| 33 | an `httpapi` test: an unknown path answers the entry document | owed |
| 37 | the shared-screens test in the app | owed |
| 39, 41 | review, against the two tables | no gate |

## consequences

- the core gains a resource kind, a static file route under the module
  prefix, a `ui` object in the listing, a `/*` mount and a feature key
- the SDK gains a session mode
- a second npm package leaves the release workflow on every tag
- `frontend/` is replaced whole, and the retired application under `/legacy/`
  keeps its own clock under ADR-20260829-cne rule 2

## rejected

| option | why not |
|---|---|
| SvelteKit as the app's frame | the app is client-only; SvelteKit adds a file router and a second injection path for the import map (finding 24) |
| pull request 185's tree as the base | its fleet adapter, plugin index and the parts that are not open source all go; what stays is framework-free or a shell |
| `frontend-svelte/` beside `frontend/`, renamed later | one move |
| the custom element boundary from the start | it costs the shared design system and the device card (finding 27) |
| a module bringing its own framework | two runtimes on one page crash with `effect_orphan` (finding 25) |
| the mediated fetch of PR 185 | it refuses the host's origin, so a module's page cannot reach its own routes (finding 22) |
| a `fetch` in the host object | a bundle would reach what the person may not; the SDK and the session already cover every call |
| an iframe or a worker per module at v1 | measured as a renderer per panel on a Raspberry Pi; the message-shaped host object keeps the door open |
| a `dashboard` slot at v1 | the core has no dashboard screen (finding 1) |
| a static plugin index written by the module's build | the core's listing already carries every enabled instance |
| React | the framework this record replaces |
| a component library from a closed Prusa product | its fonts and components are not open source |
| the library inside `packages/typescript` | the SDK declares no runtime dependency (ADR-20260909-c7x rule 3) and Svelte would be one |
| a `configSchema` per widget at v1 | an idea for a generated settings form, kept for a later release |
| a module bundling the runtime and the library | 14.1 kB against 1.7 kB gzip per bundle (finding 25), and the private module risk stays |

## tasks

The twenty tasks of earlier research are the rows of
the old backlog,
each naming the rule it serves.

## what is not decided

- whether the custom element lane opens, and when
- whether `networkAccess` may widen the CSP for a named module
- the Home Assistant variant with its base path, kept as a second build until
  the ingress question is asked again
- a `configSchema` on a settings pane
