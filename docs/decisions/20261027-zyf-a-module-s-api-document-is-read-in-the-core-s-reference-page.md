---
title: "ADR-20261027-zyf: A module's API document is read in the core's reference page"
description: "The reference page at /docs renders a module's served OpenAPI document with ?module=<namespace>, lists the core and every module document the reader may read, and the module form may be framed by a page of the same origin."
date: 2026-10-27
status: accepted
supersedes: []
amends: []
tags:
  - adr
  - modules
  - api
---
# ADR-20261027-zyf: A module's API document is read in the core's reference page

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Related: [ADR-20260909-c7x](20260909-c7x-a-client-sdk-is-generated-from-the-core.md)

A module that serves routes ships an OpenAPI fragment, and the host serves it
rebased under the module's mount at `/api/v1/modules/<namespace>/openapi.json`
(ADR-20260909-c7x rule 9). The core renders its own document at `/docs` with
the Scalar bundle it embeds. A person reading a module's routes had only the
raw JSON. This record lets the same page render a module's document, so every
API on the server reads the same way. The page also lets a reader move between
the core's document and the module documents.

## rules

1. `GET /docs?module=<namespace>` renders the document the host serves at
   `/api/v1/modules/<namespace>/openapi.json`, with the same renderer, the same
   page and the same content security policy as the core's document, except
   for rule 3.
2. A value of `module` that is not a valid namespace answers 404. Only a value
   that passes that check is written into the page.
3. The page for a module's document may be framed by a page of the same
   origin, `frame-ancestors 'self'`. The core's own page keeps
   `frame-ancestors 'none'`.
4. The renderer fetches the document with the reader's own credential. Before
   the page starts the renderer for a module, it makes the check of rule 7.
   When the reader may not read the document, or there is none, the page
   answers 404 with a short message in place of the renderer. The switcher of
   rule 7 stays, so the reader can go back to the core's document.
5. A build that leaves the reference page out answers 404 for both forms.
6. A module's page that shows its routes frames this page, and does not load a
   renderer of its own.
7. Above the document the page draws a switcher. Its first entry is the core's
   document, `Core API`. Then come the enabled modules whose document the
   reader may read, sorted by title. Each one shows the title in its manifest,
   or its instance name when the manifest has no title. A module is listed only
   when `GET /api/v1/modules/<namespace>/openapi.json` would serve this reader
   its document, and the page decides that with the same function the route
   uses. A module without a document is not listed.
8. Each entry is a plain link to `/docs` or to `/docs?module=<namespace>`. The
   address names the document on show, a deep link opens that document
   selected, and the browser's back and forward move between documents. The
   page answers `Cache-Control: no-store`, because the list depends on the
   reader.
9. The page leaves the switcher out when the browser loads it in a frame
   (`Sec-Fetch-Dest: iframe`), because a link to the core's page would open a
   page that refuses the frame. It also leaves it out when the core's document
   is the only entry.

## enforcement

| rule | gate |
|---|---|
| 1, 2, 3 | `TestTheReferencePageRendersAModuleDocument` in `internal/transport/http/httpapi` |
| 4, 7, 8, 9 | `TestTheReferenceSwitcherListsTheDocumentsTheReaderMayRead` in `internal/transport/http/httpapi` |
| 7, 8 in a browser | `frontend/test/modules/api-reference.test.ts`, run by `make example-spools-check` against the example modules |
| 5 | `docsui_noreference.go` answers 404 for the page, whatever its query |
| 6 | none. Review holds it |

## rejected

| option | why not |
|---|---|
| merge every module's fragment into `/openapi.json` | the core's document is one cached copy for every reader. See [one document per module](#one-document-per-module) |
| a second page per module, such as `/docs/modules/<namespace>` | it is one more path to keep beside the own paths of the API, for the same page with one attribute changed |
| a module bundle that ships its own copy of the renderer | about 1 MiB in every module, and a second version of the renderer to review |
| render the routes with the module's own components | two formats for one kind of document, and each module would draw them differently |
| frame the core's page too | nothing needs it, and `'none'` is the stronger default |
| the renderer's own document selector, with several `sources` | see [the renderer's selector](#the-renderers-selector) |
| build the list in the browser from `/api/v1/resources` | one more served script, and the listing does not make the whole check of rule 7 |

## the renderer's selector

The renderer can take several documents and draw its own selector. It keeps
the choice in the address fragment and changes it with `history.replaceState`,
so back and forward do not move between documents. It also reads its own
`?api=` parameter and rewrites the address, so `?module=` would not survive.
Plain links on the page give the address and the history for free.

## one document per module

The core renders `/openapi.json` once per process and hands every reader the
same bytes. A module's routes merged into it would show to a person who may
not see that module. The document would also change each time a module is
installed or removed, so the cache would have to be dropped. A module's
document is already served per module and checked per reader, which is what
rules 4 and 7 rely on.
