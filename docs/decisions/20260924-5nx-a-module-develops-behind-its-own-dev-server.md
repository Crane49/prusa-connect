---
title: "ADR-20260924-5nx: a module develops behind its own dev server"
description: "Why a module runs behind its own Vite dev server that proxies a core, what stays bare so the page holds one Svelte runtime, and why the core has no dev mode for module bundles."
date: 2026-09-24
status: accepted
tags:
  - adr
  - modules
---
# ADR-20260924-5nx: a module develops behind its own dev server

- status: accepted
- date: 2026-09-24

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md)

A module is developed behind its own Vite dev server. The dev server proxies
a core and answers the module's installed entry from source, so an edit
reaches the screen through HMR and nothing about the core's trust changes.

## rules

1. `moduleDev()` in `@prusa/connect-ui/build` is the only dev loop a
   module is offered.
2. The dev server is the page's origin. The core is reached through its
   proxy, and `PCL_CORE_URL` names it. What `PCL_CORE_URL` names serves the
   built app, a core or `vite preview`, because a second Vite dev server
   claims the same paths.
3. The namespace and entry come from the proxied `/api/v1/resources`. The
   manifest never carries a namespace.
4. Every specifier in `SHARED_SPECIFIERS` stays bare in what the dev server
   sends. The page holds one Svelte runtime.
5. Components compile with `hmr: true, dev: false`, because the host's
   runtime is a production build.
6. The core has no dev mode for module bundles. The digest check and
   `contributes` hold during development as they do in use.
7. `frontend/`'s dev server reads the in-repo packages from `src/` through
   `resolve.alias`. The aliases are derived from the packages' `exports` maps,
   and those maps name only files the published package carries.

## rejected

| option | why not |
|---|---|
| an override of the entry URL in the host | [it adds a trust question](#an-override-of-the-entry-url-in-the-host) |
| watch, rebuild, reload | [it weakens the digest check](#watch-rebuild-and-reload) |
| an export condition that points at `src/` | it ships in `exports`, and the published package carries no `src/` |

### An override of the entry URL in the host

single-spa's import-map-overrides works this way. It needs a loader change, a
core dev flag with an API, a visible badge, CORS, and Chrome Local Network
Access handling for a LAN core. That is three layers of change and a new trust
question. The only gain is that the author opens the device's address.

### Watch, rebuild and reload

Grafana's `@grafana/create-plugin` and Directus's `EXTENSIONS_AUTO_RELOAD` work
this way. The core serves only a bundle whose digest `module add` recorded. So
this needs a core mode that reads a bundle off disk unverified. That weakens
the trust model, and it still ends in a full page reload.

## evidence

- Grafana (`@grafana/create-plugin`): webpack watch plus livereload, SystemJS
  import map for shared deps. No HMR.
- Directus (`@directus/extensions-sdk`): `build -w`, `EXTENSIONS_AUTO_RELOAD`,
  manual refresh. No HMR.
- Strapi, Sanity, Payload: true HMR only because a plugin is compiled into the
  host's own dev bundle. Not our architecture.
- Vite's client dev server has no externals (vitejs/vite#6582). The working
  pattern is `vite-plugin-externalize-dependencies`: `resolveId` returns
  `external: true`, a transform strips the `/@id/` prefix, and the dependency
  is kept out of dependency optimisation.
- Svelte 5 HMR needs `hmr`, `HMR` and `cleanup_styles` from
  `svelte/internal/client`. They carry no `DEV` guard and work on a
  production runtime. The host's `svelte-internal-client-*.js` chunk exports
  all three, because `frontend/vite/import-map.ts` emits shared chunks with
  `preserveSignature: 'strict'`. Verified on `frontend/dist`.
- A second Svelte runtime on the page breaks context, reactivity across the
  boundary and event delegation. Every Svelte module must come from the host.
