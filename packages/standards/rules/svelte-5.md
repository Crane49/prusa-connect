---
paths:
  - "**/*.svelte"
  - "**/*.svelte.ts"
---

# Svelte 5

The house Svelte rules. They apply to the component library under `packages/`
and to the Svelte application under `frontend/`. Data-flow rules live in
`svelte-data.md`. Files, folders and CSS shipping live in
`frontend-structure.md` and `frontend-css.md`.

## Runes mode only

- use `$state`, `$derived` and `$effect`. These are the only reactive primitives
- no legacy `$:` reactive declarations
- no `svelte/store` imports. `writable`, `derived` and `readable` are banned
- no `svelte/legacy` imports. A component written against the legacy API is ported, never wrapped

## State management

- use `.svelte.ts` files for reactive state, with getters and setters or class-based stores
- stores are not destructurable. Access `store.value`, never `const { value } = store`
- use plain `.ts` files for non-reactive utilities

### Store shape: classes are the house choice

- shared stores are classes in `.svelte.ts`. `#` private fields hold the `$state`. Native `get` and `set` accessors are the only public surface
- dependencies, such as a service function, come in through the constructor. That is what makes a store testable without module mocks
- a `createStore()` factory returning an object literal with `get` and `set` accessors is legitimate Svelte 5 too: closure privacy, no `this`. Use it only for a small ephemeral per-component state helper, never for a shared module store
- the two shapes must not appear at random. Shared means class, local helper means factory. Say so in a comment if it is not obvious
- every mutable piece of state is a native `set` accessor that validates. An illegal value cannot be assigned at all: reject, keep the current state, persist nothing
- state that is deliberately read-only from outside, such as a server-owned collection, exposes the getter alone, with a comment saying it is read-only by design

### Store over props

Shared state is read from a store and never threaded through props. Prop
drilling is a React habit this rule exists to delete.

- props are for what a component genuinely owns: its configuration, variants, content. Never for relaying someone else's state downward
- a callback drilled through intermediate components becomes a store method the leaf calls directly
- a module-level singleton store is right for genuinely app-wide state: locale, app chrome, auth identity, the fleet
- `setContext` and `getContext` are required the moment state must be per-subtree or per-instance: two device panes side by side, an embedded widget. A singleton silently couples the instances
- a library component may render under SSR in a consumer, where a module singleton also leaks state across requests. The per-instance rule stands for both reasons

## Reactivity patterns

Build on Svelte's native reactivity. Prefer plain JS native getters and
setters, `{ get value() {…}, set value(v) {…} }` in object literals or
classes, over hand-rolled change tracking or React-style update plumbing.

- `$state` objects and arrays are already deep proxies. Mutate them directly (`state.items.push(x)`, `state.user.name = '…'`). Do not use immutable spread or clone-on-write patterns to trigger updates
- you cannot export a reassigned `$state` binding from a module. Export an object or a class instance and expose the value through a getter (`store.value`). Mutate through a method or a setter
- when you need validation or computed access on read or write, reach for a `Proxy` or a class with `get` and `set`, never extra `$derived` or `$effect` wiring
- do not use `$effect` to copy one piece of state into another. That is `$derived`. `$effect` is only for side effects that reach outside the reactive graph: DOM, external libraries, logging
- pass `$state.snapshot(value)`, a plain non-proxy clone, when handing reactive state to non-reactive or external code
- large server-owned collections (device rows, event lists) go in `$state.raw`. The rows are replaced wholesale on refetch and never mutated in place. Reassign to update
- deep proxying every row of such a collection buys nothing. It costs proxy creation plus fine-grained tracking on thousands of objects

## TypeScript

- always `<script lang="ts">`
- no TypeScript in markup. `as` casts, type annotations and `{@const x: T = …}` are not valid in the template. Declare a typed `let` or `$derived` in `<script>` and render that instead
- type props through the `$props()` destructure with an interface or type. Type or cast `$state` only inside `<script>`

## Component structure

Order within a component: `<script>`, then markup, then `<style>`.

Within `<script>`: imports, props, local state, derived, handlers, effects.

## Props and composition

- props through `$props()` with a typed interface. Two-way binding through `$bindable()`. Never `export let`
- slots are removed. Use snippets: `{#snippet name()}…{/snippet}` plus `{@render name()}`. Default content comes through the `children` prop and `{@render children?.()}`
- no prop drilling three levels or deeper. Use context or stores instead

## Data fetching

- service layer (plain `.ts`), then store (holds the result in `.svelte.ts`), then component (triggers the fetch)
- every request to the core goes through `@prusa/connect`, the SDK under `packages/typescript`. `PrusaConnect` owns the base URL, the token and the event stream. The rule is in `frontend-structure.md`, from ADR-20260909-c7x rules 2 and 8
- never call the global `fetch`, `XMLHttpRequest`, `EventSource` or a third-party HTTP package in a service or a component
- do not set `Authorization` headers, tokens or refresh logic yourself. The SDK owns that. A browser signs in with one navigation to `/api/v1/auth/login` (ADR-20260909-c7x rule 7)
- never fetch in `$effect`. Use `onMount` or `load` functions

## SvelteKit and app state

- import from `$app/state`, never `$app/stores`. `$app/stores` is deprecated from SvelteKit 2.12. `page`, `navigating` and `updated` are reactive objects with no `$` prefix
- DOM and external-library integration: prefer attachments (`{@attach fn}`) over actions (`use:`). Attachments are reactive and re-run when their dependencies change
- a client-only app sets `export const ssr = false` in the root layout and uses `adapter-static` with a fallback page. It has no server code
- the `frontend-structure.md` SSR constraints still apply to shared primitives, because a library component may render in a consumer that has SSR

## Events

- DOM events are attributes: `onclick={handler}`, never `on:click`. A modifier such as `preventDefault` is a line inside the handler
- component events are callback props: `onselect`, `onchange`. Never `createEventDispatcher`
- typed callback props in the component's props interface

## Accessibility

- semantic HTML elements
- an icon-size `Button` takes `label`. The label names the control and is the text of the tooltip it shows
- `aria-live` on dynamic content regions
- keyboard navigation for all interactive elements
- focus management on route changes and on modal open and close

## UI primitives

Shared primitives live under `src/primitives/<name>/` in the library under
`packages/svelte`, and under `src/lib/ui/<name>/` in an app: one kebab-case
folder per component. This is the Svelte shape of the house convention in
`frontend-structure.md`.

```
primitives/button/
  index.ts               pure barrel: re-exports only. No markup, no logic, no styles
                         export { default as Button } from './button.svelte'
                         export { default as ButtonGroup } from './button-group.svelte'
                         export type { ButtonVariant, ButtonSize } from './types'
  button.svelte          the component itself, small. Its utilities are in its markup
  button-group.svelte    an independent piece gets its own file
  types.ts               shared types
  variants.ts            the map from a variant name to its utility string
  button.svelte.test.ts  the browser test, on `svelte-testing.md`
```

- every independent small thing gets its own file. Never a component plus its sub-parts plus its variant logic in one file. Prefer more small files over fewer large ones
- the barrel is the only import surface consumers touch (`import { Button } from '@prusa/connect-ui/primitives'`). A component's internal file split can then change without touching call sites
- a component is composed from Tailwind utilities in its markup, on `frontend-css.md`. A group of utilities that repeats becomes a component, or an `@apply` class in the component's own `<style>` block. No stylesheet file beside a component
- a variant is a static map in `variants.ts` from its name to one utility string, chosen by key and set on the element as `data-variant`. Never a string assembled from parts, and never a value the theme lacks
- native platform first: `<dialog>`, `<details>`, `popover`, form validation, CSS `:has()` and container queries before any library. A button never needs a library
- bits-ui is the sanctioned headless library, only where a primitive genuinely needs complex accessibility behaviour the platform does not give: menu, combobox, tooltip, popover, select. It is headless, and it is styled with the same utilities
- when styling a bits-ui part, render your own element through the `child` snippet: `{#snippet child({ props })}<div {...props} class="…">`. The element is then authored in our template and carries the utilities
- open, closed and similar UI state comes back from bits-ui as `data-*` attributes, never as props. Style `[data-state='open']`, `[data-state='delayed-open']` and `[data-disabled]`. Do not mirror them into `$state`

## Embedding

A library component runs inside the core's frontend and inside a module's
page, so it is embeddable by default.

- container queries, no viewport assumptions, hidable chrome (`frontend-css.md`)
- when this repository has an embed mode, its state is read from one store. Never re-parse `location.search` in a component

## i18n

When this repository has a translation catalog:

- the supported-locale list has one home, `src/lib/i18n/locales.json`. The store, the pull script and the key generator all read it. Adding a locale is one edit
- user-facing strings go through the typed `t()` from `src/lib/i18n`. Keys are generated from the English catalog, so an unknown key is a compile error
- plurals go through `plural()` (`Intl.PluralRules`), keyed on the returned CLDR category. Czech has four forms (one, few, many, other), so a hand-rolled `count === 1` branch is always a bug
- a language switch goes through `switchLocale()`, which loads the lazy catalog chunk before flipping the store. No flash of untranslated content. English stays in the entry chunk as the per-key fallback
- anything rendered with `{@html}` goes through `sanitizeHtml()` (DOMPurify) first, no exceptions, translated content included

## Testing runes

- tests that touch runes live in `*.svelte.test.ts` files. The `.svelte.` infix routes them through the Svelte compiler
- wrap effect-driven code in `$effect.root(...)` and flush with `flushSync()` from `svelte`. Dispose the root at the end of the test
- store logic is tested through the store's public surface (getters, setters, methods), never by poking private fields
- required coverage, write-count tests, memory-leak tests and the performance budgets live in `svelte-testing.md`. That file is binding
