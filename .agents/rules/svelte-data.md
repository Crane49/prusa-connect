---
paths:
  - "**/*.svelte"
  - "**/*.svelte.ts"
---

# Svelte data and state

Where data lives and how it flows: data from the server against data the page
owns, stores, passing props down, and never fetching the same data twice.
Syntax and reactivity rules are in `svelte-5.md`.

## Server state is separate from client state

- server state is anything fetched: lists, entities, the current user. It is a copy of someone else's data, so it can go stale, needs refetching, and is shared
- server state lives in a data store (`.svelte.ts`) fed by a service, never in per-component `$state` plus `onMount` fetches scattered across the tree
- client state is form drafts, open and closed, selections. It stays as `$state` in the component that owns it. Lift it only when two siblings share it

## The layering: service, store, component

| layer | file shape | owns |
|---|---|---|
| service | plain `.ts` in `src/lib/services/` | the request: which SDK operation, its parameters, the response type |
| store | class in `.svelte.ts` in `src/lib/stores/` | the result plus `loading` and `error`, removing duplicate requests, aborting stale ones |
| component | `.svelte` | triggering loads (`onMount`, event handlers) and rendering loading, error and the result |

- a service calls `@prusa/connect` and nothing more. It holds no state and knows no framework, so a test can pass it a fake `PrusaConnect`, and any store can reuse it
- stores never build URLs or parse responses. Components never call services directly
- every request goes through the SDK (`frontend-structure.md`). The store layer wraps it and never replaces it

## One flight per piece of data

A flight is one request in progress. Two components needing the same data
must cost one request, never two.

- the store is where duplicates are removed. It is a module-level instance, so every consumer reads the same `$state`. A second `load()` while one is in flight returns the pending flight instead of firing again
- design the API so one bundled call replaces n calls when a panel always needs them together
- no query library by default. Runes give property-level subscription, so a store field update re-renders only what reads it. That is the fine-grained reactivity a React app needs selectors for
- if you start writing cache invalidation rules, stale-while-revalidate or entity sharing across stores yourself, stop and raise it. That is the signal to adopt a query layer, never to grow a home-made one

## Stores

- class-based, runes, native getters and setters. The full pattern is in `svelte-5.md`. Private `#fields` hold the `$state`. The public interface is getters for reads and setters or methods for validated writes
- export one instance per domain (`export const fleet = new FleetStore()`), never the class for consumers to instantiate as they like
- never destructure a store. `const { rows } = fleet` reads once and goes dead. Pass the store itself and read `fleet.rows` where it is used
- keep stores small and per-domain. A single app-wide store recreates the prop-drilling problem one level up
- collections the server owns use `$state.raw` and are replaced as a whole on refetch (`svelte-5.md`). A row-level edit goes through a targeted method that reassigns
- derived values (`counts`, `filtered`) are `$derived` in the store, computed once for all consumers, never recomputed per component

## Prop drilling

Prop drilling is passing a value down through components that do not use it,
only so that a deeper one can.

- three or more levels of components that do not use the prop is the threshold
- restructure first: composition, passing the assembled subtree as a snippet or `children`. Context second: `setContext` and `getContext` with a typed key. Module store last
- drilling one or two levels is fine and beats premature context
- context when the state is per-subtree (two dialog instances must not share it). A module store when the state is app-global (session, locale, app chrome)

## Loading, error, empty

Every data store exposes each of these explicitly: `loading`, `error`,
and an empty result a reader can tell apart from "not loaded yet". Components
never infer "loading" from `undefined`. Abort superseded requests with an
`AbortController` the store owns, so a stale response cannot overwrite a
newer one.

## Effects are not a data layer

- never fetch in `$effect`. Loads are triggered by `onMount`, SvelteKit `load`, or an event handler
- never use `$effect` to sync one store into another. That coupling belongs in a `$derived` or in the method that does the write
- `$effect` touching data is for the outside world only: persisting a choice to `localStorage`, wiring a chart library, or consuming the SDK's event subscription with `close()` in the cleanup
