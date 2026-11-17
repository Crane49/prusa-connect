---
paths:
  - "**/*.ts"
  - "**/*.svelte"
  - "**/*.svelte.ts"
---

# TypeScript strictness and lint gates

These rules apply to every TypeScript and Svelte package in this repository:
`packages/typescript`, its consumers under `testdata/consumers`, the frontend
and `docs/`. The Go core has its own rules. A mistake must not be able to
land. Every rule here is enforced by config in the package, never by prose
alone. `pnpm check` is the single local gate, and CI runs the same steps and
fails on any of them.

## Compiler options, non-negotiable

`strict: true` plus what strict does not include:

| option | what it catches |
|---|---|
| `noUncheckedIndexedAccess` | `arr[0]` is `T \| undefined`, so an index that may be empty is checked |
| `noImplicitOverride` | a method that hides a parent method by accident |
| `noFallthroughCasesInSwitch` | a missing `break` |
| `noUnusedLocals` and `noUnusedParameters` | dead code at the declaration site |
| `verbatimModuleSyntax` | type imports that survive into runtime |

`exactOptionalPropertyTypes` is not required. The types `openapi-typescript`
writes into `packages/typescript/src/generated/core-api.ts` declare optional
fields as `x?: T | undefined`, so the option turns every generated write into
an error that only a patch to generated code removes.

## Banned types

- `any` is forbidden (`no-explicit-any` error). The `no-unsafe-*` family (assignment, member-access, call, return, argument) keeps it from entering through an untyped dependency
- bare `object`, `{}` and `Function` are forbidden. They say almost nothing about a value
- `unknown` is permitted, but only at trust boundaries: API responses, `JSON.parse`, `postMessage`, anything crossing into our code from outside. It must be narrowed by a validator or a type guard before use
- `unknown` is never stored in a field, returned from a public function, or passed onward as `unknown`
- why `unknown` is not banned: it is the safe replacement for `any` because the compiler forces narrowing. Banning it pushes people back to `any` or to lying with a cast, which is worse
- non-null assertions (`!`) are banned in `src`. A test may use one after an explicit length or shape assertion. Prefer type guards
- prefer `satisfies` to `as`. An `as` cast needs a one-line comment saying why

## The gates

| gate | what it must be |
|---|---|
| ESLint | flat config, `typescript-eslint` with `projectService` on. Without it the `no-unsafe` rules pass everything |
| `eslint-plugin-svelte` | the recommended set, a11y rules enabled, never silenced as a whole |
| `svelte-check` | blocking. It type-checks inside `.svelte` files, which `tsc` alone does not |
| stylelint | the BEM class pattern, no `!important`, no hex, rgb or hsl, physical properties rejected (`frontend-css.md`) |
| Prettier | `--check` in CI, never a review conversation, with the Svelte plugin |

A gate that has not been watched rejecting a deliberate violation is not a
gate. When you touch gate config, write a throwaway violation and watch the
gate refuse it.

## Open items

- `packages/typescript` has no ESLint config and no `check` script. Its gates are `tsc`, `publint`, `@arethetypeswrong/cli` and `node --test`
- `packages/typescript/tsconfig.json` does not set `noUnusedLocals` or `noUnusedParameters`
- no package in this repository runs stylelint
