---
title: "ADR-20260925-f73: the library ships one tool subpath"
description: "The library's own eslint/ is gone; every consumer lints against @prusa/frontend-standards, so ./build is the library's only tool subpath."
date: 2026-09-25
status: accepted
tags:
  - adr
  - frontend
  - svelte
---
# ADR-20260925-f73: the library ships one tool subpath

- status: accepted. It extends [ADR-20260914-244](20260914-244-the-library-is-layered-and-an-import-only-goes-down.md) and [ADR-20260916-yta](20260916-yta-the-shell-is-a-published-tier.md)
- date: 2026-09-25

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Builds on [ADR-20260914-244](20260914-244-the-library-is-layered-and-an-import-only-goes-down.md), [ADR-20260916-yta](20260916-yta-the-shell-is-a-published-tier.md)

`@prusa/frontend-standards` now carries the i18n lint gate that
`@prusa/connect-ui`'s own `./eslint` subpath used to. The application,
every module and the library itself read the rule from the standards
package, so nothing imports `./eslint` any more and it is deleted.

## rules

1. `./build` is the library's only tool subpath. This replaces the clause of
   [ADR-20260914-244](20260914-244-the-library-is-layered-and-an-import-only-goes-down.md)
   rule 8 and of
   [ADR-20260916-yta](20260916-yta-the-shell-is-a-published-tier.md) rule 1 that named
   `./build` and `./eslint` as the two tool subpaths.
2. The i18n gate behind
   [`.agents/rules/frontend-i18n.md`](../../.agents/rules/frontend-i18n.md)
   lives in `@prusa/frontend-standards`, registered as
   `frontend-standards/no-literal-text`, and a consumer reads it from there
   the way it reads every other rule the standards package carries.

## enforcement

| rule | gate | state |
|---|---|---|
| 1 | `packages/svelte/build/specifiers.test.ts`, which reads the `exports` map | exists |
| 2 | `packages/standards/eslint/shared.js`, and each consumer's own lint | exists |

## rejected

| option | why not |
|---|---|
| keep `./eslint` as a thin re-export of the standards rule | a second subpath for one rule already living one import away, with nothing left importing the old one |

## consequences

- `packages/svelte/eslint/` is gone: `index.js` and `no-literal-text.js`
- `packages/svelte/package.json` `exports` carries `./build` alone as a tool
  subpath
