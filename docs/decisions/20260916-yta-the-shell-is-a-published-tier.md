---
title: "ADR-20260916-yta: the shell is a published tier"
description: "The published subpath set is eight, the shell fills the empty screens tier of ADR-20260914-244, the frame renders from props with no store read, and the route table stays in the application."
date: 2026-09-16
status: accepted
tags:
  - adr
  - frontend
  - svelte
---
# ADR-20260916-yta: the shell is a published tier

- status: accepted. It extends [ADR-20260914-244](20260914-244-the-library-is-layered-and-an-import-only-goes-down.md) and [ADR-20260915-4an](20260915-4an-what-a-second-product-needs-from-the-library.md)
- date: 2026-09-16

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Builds on [ADR-20260914-244](20260914-244-the-library-is-layered-and-an-import-only-goes-down.md), [ADR-20260915-4an](20260915-4an-what-a-second-product-needs-from-the-library.md), [ADR-20260911-4p6](20260911-4p6-a-module-extends-the-ui-through-one-library-and-four-slots.md)

`src/router/` and `src/shell/` are both built now, and publish as `./router`
and `./shell`. Two counts written before either existed need a correction: how
many subpaths the package publishes, and what the `screens` tier of
[ADR-20260914-244](20260914-244-the-library-is-layered-and-an-import-only-goes-down.md) rule 1
holds. It held nothing when that rule was written.

## rules

1. The published subpath set is nine: `./app.css`, `./tokens.css`,
   `./domain`, `./i18n`, `./primitives`, `./data`, `./slots`, `./router` and
   `./shell`. This replaces the count of eleven in
   [ADR-20260915-4an](20260915-4an-what-a-second-product-needs-from-the-library.md) rule 5,
   which does not add up on any reading. `./build` and `./eslint` are the two
   tool subpaths of
   [ADR-20260914-244](20260914-244-the-library-is-layered-and-an-import-only-goes-down.md)
   rule 8 and sit outside the nine.

   The two stylesheets are one file each for one reader. `./app.css` is the
   theme and the scan of this package's components, and an application
   imports it. `./tokens.css` is the theme alone, and a module imports it.
   A module emits only the utilities it names itself
   ([ADR-20260914-244](20260914-244-the-library-is-layered-and-an-import-only-goes-down.md)
   rules 10 to 13), so the scan would put the whole library's in every module
   bundle. `@source` takes a path and never a package name. The path lives in
   the library, where both of its ends are inside one package.
2. `src/shell/` is the `screens` tier of
   [ADR-20260914-244](20260914-244-the-library-is-layered-and-an-import-only-goes-down.md)
   rule 1. It imports what is below it, and nothing imports it except an
   application.
3. The frame never reaches the core. Every value about a printer, a person or
   a connection arrives as a prop, so the install check of
   [ADR-20260911-4p6](20260911-4p6-a-module-extends-the-ui-through-one-library-and-four-slots.md)
   rule 6 can render it from the published package, where none of this
   repository's stores exist.
4. The theme is the one state the frame holds itself. It reads the `theme`
   store this package ships and writes the class `tokens.css` matches, so a
   page is drawn on the right side with no control on screen and no wiring in
   the application. A product that wants its own control reads the same store.
5. The route table stays in the application and names paths, groups, glyphs
   and catalogue keys. This restates
   [ADR-20260915-4an](20260915-4an-what-a-second-product-needs-from-the-library.md) rule 2
   and changes nothing.

## enforcement

| rule | gate | state |
|---|---|---|
| 1 | `packages/svelte/build/specifiers.test.ts`, which reads the `exports` map | exists |
| 2 | `packages/svelte/src/test/tiers.test.ts`, which lints one probe import per zone | exists |
| 3 | a test that renders the frame from props alone and calls no operation | owed |
| 4 | `packages/svelte/src/styles/theme-class.svelte.test.ts`, which proves the class follows the preference | exists |
| 5 | [ADR-20260915-4an](20260915-4an-what-a-second-product-needs-from-the-library.md)'s own row for rules 1 and 2 | owed |

## consequences

- `packages/svelte/package.json` `exports` carries `./router` and `./shell`
  today, each with `types`, `svelte` and `default` conditions
- `packages/svelte/eslint.config.js` carries two of
  [ADR-20260914-244](20260914-244-the-library-is-layered-and-an-import-only-goes-down.md)
  rule 6's seven zones, for `src/router/**` and `src/shell/**`. The other
  five, for `sdk`, `domain`, `i18n`, `ui` and `core`, are owed
- `packages/svelte/src/index.ts`, the root barrel, does not re-export
  `./router` or `./shell`. A consumer of either imports the subpath
