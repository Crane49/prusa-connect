---
paths:
  - "**/*.svelte"
  - "**/*.svelte.ts"
---

# Svelte data and state

Server state against client state, stores, prop drilling, and never fetching
the same data twice. Syntax and reactivity rules live in `svelte-5.md`. This
rule is about where data lives and how it flows.

## Server state is separate from client state

- server state is anything fetched: lists, entities, the current user. It is a cache of someone else's data, so it has staleness, refetching and sharing concerns
- server state lives in a data store (`.svelte.ts`) fed by a service, never in per-component `$state` plus `onMount` fetches scattered across the tree
- client state is form drafts, open and closed, selections. It is colocated component `$state`. Lift it only when two siblings genuinely share it

## The layering: service, store, component

| layer | file shape | owns |
|---|---|---|
| service | plain `.ts` in `src/lib/services/` | the request: which SDK operation, its parameters, the response type |
| store | class in `.svelte.ts` in `src/lib/stores/` | the result plus `loading` and `error`, in-flight dedupe, aborting stale requests |
| component | `.svelte` | triggering loads (`onMount`, event handlers) and rendering the three states |

- a service calls `@prusa/connect` and nothing else. It is stateless and framework-free: testable with a fake `PrusaConnect`, reusable by any store
- stores never build URLs or parse responses. Components never call services directly
- every request rides the SDK (`frontend-structure.md`). The store layer wraps it and never replaces it

## One flight per piece of data

Two components needing the same data must cost one request, never two.

- the store is the dedupe point. It is a module-level instance, so every consumer reads the same `$state`. A second `load()` while one is in flight returns the pending flight instead of firing again
- design the API so one bundled call replaces n calls when a panel always needs them together
- no query library by default. Runes give property-level subscription, so a store field update re-renders only what reads it. That is the fine-grained reactivity a React app needs selectors for
- if cache invalidation rules, stale-while-revalidate or cross-store entity sharing start being hand-rolled, stop and raise it. That is the signal to adopt a query layer, never to grow a bespoke one

## Stores

- class-based, runes, native getters and setters. The full pattern is in `svelte-5.md`. Private `#fields` hold the `$state`. The public surface is getters for reads and setters or methods for validated writes
- export one instance per domain (`export const fleet = new FleetStore()`), never the class for consumers to instantiate ad hoc
- never destructure a store. `const { rows } = fleet` reads once and goes dead. Pass the store itself and read `fleet.rows` at use sites
- keep stores small and per-domain. A single app-wide store recreates the prop-drilling problem one level up
- server-owned collections use `$state.raw` and are replaced wholesale on refetch (`svelte-5.md`). A row-level edit goes through a targeted method that reassigns
- derived values (`counts`, `filtered`) are `$derived` in the store, computed once for all consumers, never recomputed per component

## Prop drilling

- passing a prop through three or more levels of components that do not use it is the threshold
- restructure first: composition, passing the assembled subtree as a snippet or `children`. Context second: `setContext` and `getContext` with a typed key. Module store last
- drilling one or two levels is fine and beats premature context
- context when the state is per-subtree (two dialog instances must not share it). A module store when the state is genuinely app-global (session, locale, app chrome)

## Loading, error, empty

Every data store exposes all three states explicitly: `loading`, `error`,
and an empty result distinguishable from "not loaded yet". Components never
infer "loading" from `undefined`. Abort superseded requests with an
`AbortController` the store owns, so a stale response cannot overwrite a
newer one.

## Effects are not a data layer

- never fetch in `$effect`. Loads are triggered by `onMount`, SvelteKit `load`, or an event handler
- never use `$effect` to sync one store into another. That coupling belongs in a `$derived` or in the method that does the write
- `$effect` touching data is for the outside world only: persisting a choice to `localStorage`, wiring a chart library, or consuming the SDK's event subscription with `close()` in the cleanup
