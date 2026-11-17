# The Svelte consumer that draws a primitive

A plain Vite app on Svelte 5, with no SvelteKit, that installs both client
packages from the registry fixture and renders a page built from the library's
primitives. `scripts/check-client-sdk-install.sh` runs it, and `make
client-sdk-install-check` runs that.

## What it proves

- `@prusa/connect` and `@prusa/connect-ui` install together from a
  registry at exact versions, and the library's peer on the client resolves
  against the version this tree published
- the `exports` map resolves from an installed package. The components arrive
  from `@prusa/connect-ui/primitives`, the texts from
  `@prusa/connect-ui/i18n`, and the theme from
  `@prusa/connect-ui/tokens.css`. None of the three is a relative path
  into the package
- `dist/` carries every subpath the `exports` map names, so a build that
  dropped a chunk fails here rather than in a module author's project
- Tailwind, set up the way a module author sets it up, compiles the library's
  utilities. The `@source` line is what reaches into `node_modules`
- a primitive runs in a real browser: `Card` draws its heading and `Button`
  draws a label the library's own catalogue holds

The heading is read from a `PrusaConnect` built in session mode, which is how
a page the core serves builds one (ADR-20260911-4p6 rule 20). No request is sent. The
Node consumer beside this one is where the lane proves a server answers.

Merge 0bf80a81 removed the library's shell tier, so this page draws primitives
where it used to draw `AppShell` and `PageLayout`.

This consumer doubles as the reference for a module's bundle: its
`vite.config.ts`, its `src/app.css` and its `package.json` are what a module
author copies.
