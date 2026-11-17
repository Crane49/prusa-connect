---
title: "ADR-20261103-22f: a page that frames the reference says so in its address"
description: "The reference page leaves its row of document links out when the address carries the embedded parameter as well as when the browser sends Sec-Fetch-Dest: iframe, because a browser sends that header only to a secure origin."
date: 2026-11-03
status: accepted
supersedes: []
amends: [ADR-20261027-zyf]
tags:
  - adr
  - modules
  - api
---
# ADR-20261103-22f: a page that frames the reference says so in its address

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Amends [ADR-20261027-zyf](20261027-zyf-a-module-s-api-document-is-read-in-the-core-s-reference-page.md) rule 9

ADR-20261027-zyf rule 9 leaves the row of document links out of a framed
reference page, and learns of the frame from `Sec-Fetch-Dest: iframe`. A
browser sends the `Sec-Fetch-*` headers only to a secure origin: HTTPS or
localhost. A server on plain HTTP at a LAN address gets no header. It draws the
row inside the frame, and `Core API` there opens a page that refuses the frame.

## rules

1. The page leaves the switcher out when the browser says it loads the page in
   a frame (`Sec-Fetch-Dest: iframe`), and also when the address carries the
   `embedded` parameter. This replaces the first sentence of ADR-20261027-zyf
   rule 9.
2. A page that frames the reference adds `embedded=1` to the address in the
   frame's `src`. A link that opens the reference outside the frame does not.

## enforcement

| rule | gate |
|---|---|
| 1 | `TestTheReferenceSwitcherListsTheDocumentsTheReaderMayRead` in `internal/transport/http/httpapi` |
| 2 | `slicer-page.svelte.test.ts` in `extensions/slicer/ui` holds the frame's `src` |

## rejected

| option | why not |
|---|---|
| `target="_top"` on the `Core API` link | the frame keeps a row the rule says it leaves out, and a click leaves the module's page |
| trust `Sec-Fetch-Dest` alone | a server on plain HTTP at a LAN address never gets it |
