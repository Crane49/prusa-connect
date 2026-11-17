---
paths:
  - "**/*.svelte"
  - "**/*.svelte.ts"
  - "**/*.css"
---

# Frontend structure

How frontend code is organised on disk. Syntax rules are in `svelte-5.md`
and `frontend-css.md`. This rule is about files, folders, how styles ship,
and how a page reaches the core. It applies to the component library under
`packages/` and to the Svelte application under `frontend/`.

## Component folders

One folder per component, kebab-case, everything in the same place:

```
src/lib/<domain>/<component-name>/
  <component-name>.svelte          the component, its utilities in its markup
  index.ts                         barrel: named export plus type re-exports
  <component-name>.svelte.test.ts  colocated tests
  <component-name>.stories.svelte  colocated stories, required by storybook.md
```

- `<domain>` groups by feature (`fleet`, `device`, `events`). Shared primitives, the small shared components such as a button, go under `lib/ui/<name>/` in an app and under `src/primitives/<name>/` in the library, in the shape `svelte-5.md` shows
- one exported component per file. A file holding a second exported component, or passing about 200 lines, should be split. Tiny private leaf subcomponents used only by their parent may stay in the parent's file
- code that is not UI never lives with components: services in `lib/services/`, stores in `lib/stores/`, types in `lib/types/`, design tokens and global styles in `styles/`

## The core is reached through the SDK

A page talks to the core through `@prusa/connect`, the package built
from `packages/typescript`. That is ADR-20260909-c7x rules 2 and 8.

- one `PrusaConnect` instance per app, constructed in one module, and every service receives it. Nothing else knows the base URL or the token
- no hand-written client. No `fetch`, `XMLHttpRequest` or third-party HTTP package against `/api/v1` anywhere in a component, a store or a service
- the event stream is read through the SDK's `events()` subscription, never through `EventSource`. `EventSource` sets no header and cannot read an HTTP status, and ADR-20260909-c7x rule 8 rejects it for that reason
- a module's page reaches the module's own routes the same way, through the SDK generated from its fragment (ADR-20260909-c7x rule 9)
- a type the page needs from the API comes from the package's exports, never from a copy

## Styles are Tailwind utilities, and the theme is one file

- a component is composed from Tailwind utilities in its markup, on the rules in `frontend-css.md`. Never CSS-in-JS, styled-components, or styles written in TS or JS
- a group of utilities that repeats becomes a component, or an `@apply` class in the component's own `<style>` block. No stylesheet file beside a component, and no global stylesheet of component rules
- the theme is one file, `styles/tokens.css`: every colour, spacing, radius, font size, duration and easing as a custom property, registered with `@theme`. The library ships it as `@prusa/connect-ui/tokens.css`
- an app has one more stylesheet, `styles/global.css`: `@import 'tailwindcss'`, one `@source` for its own tree, one `@import` per library, the reset, the focus ring and reduced motion. A library's `app.css` carries its theme and the scan of its own components, so no path in an app reaches into `node_modules`. `@source` takes a path and never a package name: a bare specifier compiles without a warning and scans nothing
- Tailwind emits only the utilities the scanned sources use, so a component's CSS costs what its markup names, and a lazy-loaded component has no stylesheet of its own to split
- watch the built CSS. A restructure or a new component must not grow the bundle past the route budget in `svelte-testing.md`. Compare the built CSS size before and after

## SSR

SSR is server-side rendering: the page is drawn on the server first.

- component code must not assume a browser at module scope. No `window`, `document` or `localStorage` outside effects or event handlers. The same component may render on the server in a consumer that uses SSR
- a client-only app may relax this. A shared or library component never does
