---
title: "ADR-20261029-zzg: a module's styles reach only its own markup"
description: "A bundle narrows its scoped styles to the markup under its own data-module and stops at another module's markup inside it, and a component one module draws from another stands under its exporter's name."
date: 2026-10-29
status: accepted
supersedes: []
amends: [ADR-20260914-244]
tags:
  - adr
  - extensions
  - ui
---
# ADR-20261029-zzg: a module's styles reach only its own markup

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Amends [ADR-20260914-244](20260914-244-the-library-is-layered-and-an-import-only-goes-down.md)

ADR-20260914-244 rule 14 wraps a module's CSS in `@scope ([data-module])`. That keeps a module's rules off the host page, and it lets them reach every other module's markup. One module that names `.flex-col` beat the host's `@sm/kv:flex-row` inside another module's view. A settings list of a 3MF project stood in one centred column on a server that also ran a module with that utility.

## rules

1. A bundle that the host serves under `/api/v1/modules/<namespace>/ui/` reads its namespace from its own address. It narrows its styles to `@scope ([data-module="<namespace>"]) to ([data-module]:not([data-module="<namespace>"]))`.
2. So a module's rules reach the markup under its own name, and stop at the markup of another module inside it.
3. A bundle served from any other address, such as a test or the dev server, keeps `@scope ([data-module])`.
4. The element that carries the styles names the namespace in its id, so two modules that ship one stylesheet get one element each.
5. The registry records which module registered an exported component. `ModuleComponent` draws it inside an element with that module's `data-module`, so its own styles reach it and the importing module's do not. An importing bundle changes nothing.
6. Content a module portals out of its slot, such as a dialog, receives the module's utilities only when it carries the module's `data-module` itself.

## enforcement

`narrows its rules to the module the host serves it as` in `build/module.test.ts` runs the bundle's injection for both addresses (rules 1, 3 and 4). `keeps one module rule off the markup of another module inside it` checks the prelude in a browser (rule 2). `draws the component under the name of the module that exported it` holds rule 5.

## rejected

| option | why not |
|---|---|
| the build writes the namespace into the bundle | the build does not know it: the host derives the namespace from the module path at install |
| each importing module passes the exporter's name to `ModuleComponent` | every importer would have to remember it, and one that forgot would draw the component unstyled |
| a lower boundary at every library primitive | a module's class on a card or a button would stop working, because the primitive is its own boundary |

## open items

A module's own utilities still beat a host container variant on a library primitive inside its own markup. A module that names `.flex-col` and draws `KeyValueList` would stack its rows. Today no official module does both.
