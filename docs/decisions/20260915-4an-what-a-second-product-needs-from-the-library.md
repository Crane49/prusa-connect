---
title: "ADR-20260915-4an: what a second product needs from the library"
description: "The route matcher becomes a core-tier subpath while the route table stays in the application, a data store accepts a result it did not fetch so a product can render on a server, and a font family is a token the library names but never ships."
date: 2026-09-15
status: accepted
tags:
  - adr
  - frontend
  - svelte
  - ssr
  - tokens
---
# ADR-20260915-4an: what a second product needs from the library

- status: accepted. It extends [ADR-20260914-244](20260914-244-the-library-is-layered-and-an-import-only-goes-down.md)
- date: 2026-09-15

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Builds on [ADR-20260914-244](20260914-244-the-library-is-layered-and-an-import-only-goes-down.md), [ADR-20260911-4p6](20260911-4p6-a-module-extends-the-ui-through-one-library-and-four-slots.md)

A second application can be built on this library and this SDK, in a
repository of its own. It renders on a server, this one does not. It serves the same screens
under its own paths. It uses a font this repository may not publish.

Rule 38 of
[ADR-20260911-4p6](20260911-4p6-a-module-extends-the-ui-through-one-library-and-four-slots.md)
and recommendation 25 of earlier research already put that product in its own
repository. Rule 1 of
[ADR-20260914-244](20260914-244-the-library-is-layered-and-an-import-only-goes-down.md) sets
the tiers, and rule 4 sets the three ways a screen varies. Three things those
records do not answer stop the second product from starting:

- where the route matcher lives
- how a store is filled when a server filled it
- how a licensed font reaches a page from outside this repository

## rules

### the router

1. The route matcher is a `core` tier module and publishes as `./router`. It
   matches a pattern, reads and writes history, and reports the current match.
   It contains no route table, no path literal and no navigation label.
2. The route table stays in the application.
   [ADR-20260914-244](20260914-244-the-library-is-layered-and-an-import-only-goes-down.md)
   rule 3 does not change. A table names paths, navigation groups, icons and
   catalogue keys, and each product spells those differently.
3. The matcher is a class the application constructs, never a module
   singleton. Two products on one page, or one product rendering on a server,
   would otherwise share one match. This is the rule `svelte-5.md` already
   sets for a store.
4. A product may use its own matcher instead. A product whose frame routes on
   a server has no use for this one, so `./router` is offered and never
   required.
5. The published subpath set of
   [ADR-20260914-244](20260914-244-the-library-is-layered-and-an-import-only-goes-down.md)
   rule 8 becomes eleven, with `./router` added.

### rendering on a server

6. A product on this library may render on a server. The core's own
   application does not. Its Go binary embeds the built application at
   `internal/transport/http/httpapi/appui_full.go` and serves the files, and a
   printer does not run a second runtime beside that binary.
7. A store in `./data` accepts a result it did not request. The store contract
   gains one method that sets the loaded state from a value, beside the
   existing method that goes to the SDK for it.
8. A screen reads a store and never learns how the store was filled. A product
   that renders on a server resolves the value in its own `load` step and sets
   it. A product that renders in a browser asks the store to fetch on mount.
   The screen is the same file in both.
9. The rules that make a component safe to render on a server already exist:
   `frontend-structure.md` rule on SSR, and `svelte-5.md` on per-instance
   state. This record adds no new one. It states that they now have a reader.

### fonts

10. `tokens.css` names a font family and ships no font file. `--font-sans` and
    `--font-mono` are tokens, and a consumer overrides them.
11. A licensed font reaches a page from a private package that depends on this
    one. That package redefines `--font-sans` and brings its own `@font-face`
    rules. This is the shape a skin already uses to redefine `--primary`.
12. No font file whose licence forbids redistribution is committed to this
    repository. The repository is going open source, and a closed product
    does not live in it.
13. The private package is a consumer of this one, not a tier of it. Rule 7
    of [ADR-20260914-244](20260914-244-the-library-is-layered-and-an-import-only-goes-down.md)
    rules about the tiers inside this package. It does not reach a package
    downstream of it.
14. Until the private package exists, the public one names a font whose
    licence allows redistribution.

## enforcement

| rule | gate | state |
|---|---|---|
| 1, 2 | a restricted-import zone that refuses a route table under `core` | owed |
| 3 | a test that constructs two matchers and proves one match does not reach the other | owed |
| 5 | `packages/svelte/build/specifiers.test.ts`, which reads the `exports` map | exists |
| 7, 8 | a store test that sets a value, reads the loaded state, and proves no request was made | owed |
| 9 | the rules named, and the reviews they already have | no gate |
| 10, 12, 14 | a test that refuses a font file under `packages/svelte` and under the application | owed |
| 11 | a fixture package that redefines `--font-sans` and renders | owed |
| 13 | review | no gate |

## consequences

- `frontend/src/lib/router.svelte.ts` moves into the library. Its 284 lines
  stop being a file the second product copies
- `frontend/src/lib/routes.ts` stays where it is. Its 227 lines are this
  product's own paths, groups, icons and catalogue keys
- every store in `./data` gains one method. A store that only fetches cannot
  serve a product that renders on a server
- the second product writes a `load` step per screen it serves. The screens
  themselves it imports
- this branch already follows rules 10 and 11. `tokens.css` names a family and
  ships no file, `frontend/src/styles/global.css` says why, and the only font
  file tracked here is `frontend/public/fonts/inter/InterVariable.woff2`. The
  rules record what is built rather than ask for a change
- the three Atlas Grotesk files are on the old `main` branch, which becomes
  `legacy/main`. They do not reach this branch, and a gate keeps it that way

## rejected

| option | why not |
|---|---|
| the matcher stays in each application | 284 lines copied, and a bug fixed twice |
| the route table moves into the library | paths, navigation and icons differ per product, so the table would become a configuration surface larger than the table |
| the core's own application renders on a server | its Go binary would need a JavaScript runtime beside it on a printer |
| a screen fetches on mount only | a server would render an empty page, which is what a product renders on a server to avoid |
| a screen reads a flag that names the product | [ADR-20260914-244](20260914-244-the-library-is-layered-and-an-import-only-goes-down.md) rule 4 names the three ways a screen varies, and a flag is a fourth that grows one branch per difference |
| the licensed font ships in this package | the repository is going open source |
| a separate package for the screens | [ADR-20260914-244](20260914-244-the-library-is-layered-and-an-import-only-goes-down.md) rule 7 rejected one package per tier, and screens are a tier |

## open items

- `frontend/src/lib/router.svelte.ts` has not been read line by line for code
  that only this application needs. Rule 1 assumes the file is a matcher and
  nothing else, and the move proves or disproves that
- no store in `./data` sets a value it did not fetch, so rule 7 has nothing to
  test yet
- the three Atlas Grotesk files stay reachable on `legacy/main`, and removing
  them from a branch does not remove them from its history. Whether that
  matters once the repository is public, and who may authorise a rewrite, is
  not recorded
- rule 14 says the public package names a redistributable font and does not
  say which one. The application already serves Inter, and the record leaves
  the library's default to the task that removes the other font
