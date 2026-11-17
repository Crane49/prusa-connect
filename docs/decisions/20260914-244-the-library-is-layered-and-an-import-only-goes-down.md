---
title: "ADR-20260914-244: the library is layered and an import only goes down"
description: "Seven tiers from the generated SDK up to the application, one direction of dependency between them, restricted-import zones that refuse the other direction, one npm package with six published subpaths in place of ADR-20260911-4p6 rule 3's eight, and a module free to name any Tailwind utility."
date: 2026-09-14
status: accepted
tags:
  - adr
  - frontend
  - svelte
  - modules
  - enforcement
---
# ADR-20260914-244: the library is layered and an import only goes down

- status: accepted. It extends [ADR-20260911-4p6](20260911-4p6-a-module-extends-the-ui-through-one-library-and-four-slots.md)
- date: 2026-09-14

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Builds on [ADR-20260911-4p6](20260911-4p6-a-module-extends-the-ui-through-one-library-and-four-slots.md), [ADR-20260909-c7x](20260909-c7x-a-client-sdk-is-generated-from-the-core.md), [ADR-20260910-74m](20260910-74m-a-module-declares-what-it-depends-on.md)

The frontend is seven tiers, and a tier may import a tier below it only.
[ADR-20260911-4p6](20260911-4p6-a-module-extends-the-ui-through-one-library-and-four-slots.md)
gave the library one package, four slots and eight subpaths. It did not say
which part of the library may import which part, and the import graph has
three cycles today.

A module author reads the tiers as a menu. The `ui` tier is where the
components are. The `domain` tier is where the rules about printing are. A tier
above those is the library's own business and a module never imports it.

## rules

1. The frontend is seven tiers, and a tier imports a tier below it and never
   one above. Bottom to top:

   | tier | what is in it | may import |
   |---|---|---|
   | `sdk` | `@prusa/connect`, generated from the core's OpenAPI document | nothing of ours |
   | `domain` | printing rules: lifecycle, heating, printer state, notification, command projections, and the shapes above agree on | the SDK, types only |
   | `i18n` | `t`, the catalogue registry, the formatting, the fault rendering | nothing |
   | `ui` | tokens and theme, primitives, the slot outlet | `domain`, `i18n` |
   | `core` | the stores over the SDK, the session, the module host object, the slot registry and loader, the navigation rule | `sdk`, `domain`, `i18n` |
   | `screens` | whole screens, with their slots and their injected actions | everything below |
   | the application | the route table, the client, the filling of slots, the composition root | everything below |

2. The `ui` tier is the one a module builds its own components on. It carries
   markup and styles. The `core` tier is runtime with no markup in it, so a
   module reaches the same behaviour through the host object of
   [ADR-20260911-4p6](20260911-4p6-a-module-extends-the-ui-through-one-library-and-four-slots.md)
   rule 18.
3. The application is a leaf. Nothing imports it, and it is the only place a
   route table, a client instance and a filled slot may appear together.
4. A screen varies in three ways, and a module reaches a screen through one of
   them:

   | way | what a module gives | who renders it |
   |---|---|---|
   | a slot | a component, named in `contributes` | the screen, through the slot outlet |
   | a route of its own | a page component under the module's namespace | the application's route table |
   | an injected action | a handler the screen's own button calls | the screen's own button |

   `device-action` is this way's data-driven form: the manifest gives the
   label and icon, and the bundle gives only the handler
   ([ADR-20260911-4p6](20260911-4p6-a-module-extends-the-ui-through-one-library-and-four-slots.md)
   rule 46).

5. Two build tools sit at the edges of the contract, one per side. `./build`
   exports `moduleBuild()`, the Vite config a module's bundle is built with.
   The import-map plugin is the other, and the application's own build reads it.
6. Restricted-import zones in the library's eslint config refuse an import that
   goes up. A test names one import per zone and proves the rule fires, so a
   zone whose path resolves to nothing cannot report clean.
7. The tiers are subpaths of one npm package and never separate packages. The
   release job stamps both manifests from the core's tag
   ([ADR-20260911-4p6](20260911-4p6-a-module-extends-the-ui-through-one-library-and-four-slots.md)
   rule 7), so every tier already moves on one version. Separate packages would
   buy separate publish jobs and separate lockfiles, and a consumer would
   resolve the same code either way.
8. The published subpath set is six: `./tokens.css`, `./domain`, `./i18n`,
   `./primitives`, `./data` and `./slots`. `./build` and `./eslint` are the two
   tool subpaths of rule 5 and are not part of the six.
9. Rule 8 supersedes
   [ADR-20260911-4p6](20260911-4p6-a-module-extends-the-ui-through-one-library-and-four-slots.md)
   rule 3's table of eight. `./i18n` is built and a consumer imports it, and
   the table of eight was written before it existed.
10. A module may name any Tailwind utility, and not only a utility the
    application already uses. The application's `@source` lines scan the
    application's own tree and the library's `dist`. A module is absent from
    that build, so a utility only a module names is emitted by nobody, and the
    page renders with the class present and the style missing.
11. `moduleBuild()` therefore builds the module's own Tailwind. The bundle
    emits utilities and never Tailwind's preflight. The naive stylesheet an
    author would write emits the preflight, which restyles the host page.
12. A module's stylesheet reads Tailwind's own theme and the library's
    `tokens.css` by reference. Neither reference emits a byte, and the
    variables resolve from the host page's `:root`. So the dark variant, the
    library's own utilities and its scale resets all reach a module.
13. The emitted CSS reaches the browser inside the one file the manifest's
    `entry` names, because the manifest names one file. One bundle reached
    twice styles the page once.
14. `moduleBuild()` wraps that CSS in `@scope ([data-module])`, with
    `@property` rules and layer-order statements kept outside, where a browser
    accepts them. The host's utilities share the module's layer and come
    earlier in the page, so an unscoped module rule beats a host variant on the
    host's own elements. The build owns the scope so that no stylesheet a
    module author writes can reach the host. Content a module portals out of
    its slot does not receive the module's utilities.

## enforcement

| rule | gate | state |
|---|---|---|
| 1, 2, 3 | restricted-import zones in `packages/svelte/eslint.config.js` | owed |
| 6 | a test that each zone refuses one named import | owed |
| 4 | review, against the three ways | no gate |
| 5 | `packages/svelte/build/module.test.ts`, extended to the Tailwind output | exists |
| 7 | the stamping step in `.github/workflows/release.yml` | exists |
| 8 | `packages/svelte/build/specifiers.test.ts`, which holds the import map's specifier list against the `exports` map | exists |
| 10 | a fixture module naming an unusual utility, which receives it | exists |
| 11 | the same fixture, read for a preflight rule it must not carry | exists |
| 12 | the same fixture, read for a rule only the library's theme defines | exists |
| 13 | the same fixture, mounted twice on a page the host styles | owed |
| 14 | the same fixture, read for the scope; and a slot test, where a scoped rule misses a host element | exists |

## consequences

- six imports in `packages/svelte/src` move, and each move breaks one
  back-edge. The rows of
  the old backlog
  name them
- the injected-action contract and the `ReadonlyFleet` type reach the `domain`
  tier, where a component at any tier above may read them
- `moduleBuild()` gains a Tailwind step, so a module's bundle grows by the
  utilities that module names
- a module's styles cost less than its code at the sizes the two proof modules
  reach. `examples/spools` carries 26 utility rules in 1,638 bytes of CSS
  against 9,514 bytes of JavaScript, and `examples/nozzle-watch` 17 rules in
  1,819 bytes. A module naming 87 utilities was measured at 9,420 bytes of CSS
  against 4,453 of JavaScript, so the two lines cross
- the preamble every module repeats is the banner and the `@property`
  declarations the named utilities need. It is under 100 bytes for `spools` and
  about 400 for `nozzle-watch`, and it grows with which utilities are named
- `@tailwindcss/vite` is a dependency of the library and not a peer. The
  library imports it and a module author never names it, so a peer would be a
  peer an author forgets, and the failure would land in their build
- a module author has one page to read about styling, because the answer is no
  longer the application's `@source` lines

## rejected

| option | why not |
|---|---|
| one npm package per tier | separate publish jobs and lockfiles, and a consumer resolves the same code |
| a `madge` circular-import scan instead of zones | it reports a cycle after the fact and names no direction |
| the application listing every module's utilities in `@source` | the application's build runs before a module exists |
| a fixed utility list a module may name | Tailwind has no closed set, and an author would meet a refusal with no repair |
| shipping the tokens again inside every bundle | the theme is already on the page, and a second copy can disagree with the first |
| leaving the slot outlet in the `core` tier | it is markup, and the tier above renders it |
| keeping ADR-20260911-4p6 rule 3's eight subpaths and hiding the other two | a consumer already imports both, so the record would describe a package nobody publishes |
| a warning on an upward import | [ADR-20260826-gtb](20260826-gtb-writing-that-reads-as-written.md) rule 9. A warning is how a gate dies |

## what is not decided

- A development loop for a module author. Every rebuild moves the digest the
  install recorded, so a watch-and-reinstall loop needs one of two things.
  Either a reinstall per change, or a mode in which the core reads a bundle
  from disk without verifying its digest. The second weakens
  [ADR-20260911-4p6](20260911-4p6-a-module-extends-the-ui-through-one-library-and-four-slots.md)
  rule 30, so it is an open question and this record leaves it open.

## open items

- rule 8 names six subpaths and the package's `exports` map has nine
  entries. `.`, the package itself, is the one the rule does not name, and it
  is published. The rule's count is a question for a record, not for the gate:
  `packages/svelte/build/specifiers.test.ts` reads the map itself
- the `screens` tier of rule 1 holds nothing. The old backlog
  row 8 deleted the screens with the primitives they render, and the tier is
  written again against the ported components. The `ui` tier holds the theme,
  six primitives with no React counterpart and the slot outlet until those
  rows land
- no eslint zone exists, so rule 6 has nothing to test
- rule 13's second half has no gate. The style element is keyed by a digest of
  the CSS, and nothing mounts one bundle at two addresses and counts the
  elements
- rule 10 closes one silence and leaves the other. A utility the application
  never named now reaches the page. A utility the library's theme does not
  define still emits nothing, and `rounded-4xl` is one, because the theme
  resets that scale
- this record carries no diagram, and rule 1's table shows the direction.
  [ADR-20260826-shb](20260826-shb-a-document-is-read-at-speed.md) rule 10 asks for one. A
  `mermaid` fence here also needs a row in
  `internal/arch/diagram_floor_test.go`
