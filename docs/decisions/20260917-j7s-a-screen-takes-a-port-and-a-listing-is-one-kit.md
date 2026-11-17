---
title: "ADR-20260917-j7s: a screen takes a port and a listing is one kit"
description: "A screen in the published tier declares the values it needs instead of reading a store, and every listing screen composes one set of named shapes rather than writing its own pager, facets and field picker."
date: 2026-09-17
status: superseded in part by ADR-20261001-pdr
tags:
  - adr
  - frontend
  - svelte
---
# ADR-20260917-j7s: a screen takes a port and a listing is one kit

- status: superseded in part by ADR-20261001-pdr. Rule 7 gives way for the printers screen, where the server counts the facets, and the rest stands
- date: 2026-09-17

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Superseded in part by [ADR-20261001-pdr](20261001-pdr-a-listing-says-how-it-may-be-filtered.md) | ADRs: [20260914-244](20260914-244-the-library-is-layered-and-an-import-only-goes-down.md), [20260915-4an](20260915-4an-what-a-second-product-needs-from-the-library.md), [20260916-yta](20260916-yta-the-shell-is-a-published-tier.md)

[ADR-20260915-4an](20260915-4an-what-a-second-product-needs-from-the-library.md) says a second
product reuses this library, and
[ADR-20260916-yta](20260916-yta-the-shell-is-a-published-tier.md) says the frame renders from
props and reads no store. Neither says what a whole screen takes, and a screen
is the thing a second product actually wants.

This record settles two things the printers listing established and the nine
screens of the old backlog
repeat: what a screen is handed, and what a listing screen is allowed to write
for itself.

The second half exists because the machinery a listing needs is the same
machinery every time. Paging, facet chips with counts, a field picker and a
view switch appear on the printers, jobs, materials, labels, storage and people
screens. Written per screen that is six copies of the same bug.

## rules

1. A screen declares a **port**: an interface in its own `types.ts` naming the
   values and callbacks it needs. It does not name a store, a class or an SDK
   client, and it does not import from `./data` for anything but a type.
2. A port is satisfied **structurally**. This product hands its store in
   because the store's shape matches; another product hands in an object it
   built itself. Nothing in a screen asks which product is running.
3. A port carries **data in and intent out**. A callback says what the reader
   did (`onSignIn`, `onAdd`), never how to do it. The caller decides what a
   click performs, so cloud and local differ without the screen knowing.
4. A refusal travels as it was **thrown**, typed `unknown`, and is drawn
   through `describeFault`. A screen writes no English error sentence, because
   the registry code is what carries the translation and the server's own line.
5. A listing screen **composes the kit and writes none of it**:

   | part | owns |
   |---|---|
   | `ListingState` | the rows after filtering and sorting, the current page, which fields show, which view is on, the facets |
   | `PageHeader` | the title and the one act the screen offers |
   | `ListingToolbar` | the facet chips, the field picker, the view switch |
   | `Pager` | the page controls and the count |

6. The kit and the screen meet at **named shapes**, and a screen passes them
   whole rather than spreading them: `FacetDimension`, `PagerControls`,
   `VisibleFields`, `ListingView`, `ListField`. A new listing screen that needs
   a shape the kit does not have changes the kit once, for everybody.
7. A facet counts what it **would bring**, not what it kept: the count beside a
   value excludes its own dimension from the filter. A chip reading zero is a
   dead end the reader can see before clicking it.
8. Layout that changes with width uses a **container query**, never a viewport
   query and never a measured width in JavaScript. A screen drawn inside a
   narrow panel is narrow, whatever the window is.
9. What a reader chose and **no server holds** lives in the browser through
   `Preferences`, keyed by the screen. Fields, view and page size persist;
   filters do not.
10. A screen's states are the **storybook's states**. The React storybook is
    the specification, so a state it draws and the Svelte screen does not is an
    unfinished screen, and the empty, loading and refused states count.

## enforcement

| rule | gate |
|---|---|
| 1, 2 | `packages/svelte/eslint.config.js` zone for `src/shell/**`, plus `src/test/tiers.test.ts`. Partial: [ADR-20260916-yta](20260916-yta-the-shell-is-a-published-tier.md) records the five zones still owed |
| 4 | `packages/svelte/src/i18n/catalogue.test.ts` refuses a key the library asks for and does not hold, and an English literal has no key at all |
| 4, 10 | `packages/svelte/src/i18n/voice.test.ts` holds every English line to the controlled vocabulary and the label and sentence rules |
| 10 | a `*.svelte.test.ts` per screen, run in real Chromium, with `expectNoViolations` from `src/test/axe` |

Rules 3, 5, 6, 7, 8 and 9 have no gate. They are review criteria, and they
decay without one. The cheapest gate that would catch rule 5 is a test that
refuses a `src/shell/**` component declaring its own page-size or
current-page state, and it is not written.

## rejected

| option | why not |
|---|---|
| a screen reads the store directly | the store is this product's. A second product would fork the screen to change one field |
| context instead of props | the tree inside a screen is one level deep. A card gets the row it draws and the field test it asks, which is two props and no drilling |
| a generic `<Listing>` component every screen fills with snippets | the screens differ in what a row is, not in how paging works. Inverting it puts the varying part in the frame and the fixed part in the caller |
| each screen writes its own pager and facets | six copies, and the facet counting rule 7 describes was got wrong the first time it was written |
| a viewport media query for the table and card switch | a screen is not always the window. A module's page draws the same listing inside a panel |
| a density toggle the reader sets | the fleet's size already says it. One printer wants the room, forty do not, and `homeShapeFor` derives it with a preference only for the person who disagrees |

## open items

- `ListingState` carries a comparator the screen fixes, and no sorting the
  reader picks and no search. Jobs, materials and storage need both.
  The old backlog
  row 0.
- Rules 3, 5, 6, 7, 8 and 9 have no gate, as the enforcement section says.
- The camera view on a listing is a stub.
  The old backlog.
