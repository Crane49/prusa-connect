---
title: "ADR-20260930-rq4: tags stay out of the interface for now"
description: "The interface draws no printer tag, no Tags page and no tag filter while one flag in the library is off. The server keeps serving tags, and turning the flag on brings every part back."
date: 2026-09-30
status: accepted
tags:
  - adr
  - frontend
  - svelte
---
# ADR-20260930-rq4: tags stay out of the interface for now

- status: accepted
- date: 2026-09-30

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md)

Printers carry plain tags, as the owner chose on 2026-09-24, and the server
serves them under ADR-20260826-5yj. A product review of the interface on 2026-09-30
asked for tags to be hidden everywhere until the product is ready to show them.
The server is not changed. A tag set through the API stays on the printer, and
the interface shows it again once the flag is on.

## rules

1. `FEATURES` in `packages/svelte/src/domain/features.ts` is the one switch.
   `FEATURES.tags` is `false`.
2. While `FEATURES.tags` is `false`, the rail has no Tags entry, the app answers
   no `/tags` page, the printer's Info tab draws no tags section, and the
   Printers page offers no tag filter.
3. Every place that hides something reads `FEATURES` and names this record.
   Nothing is hidden by deleting it, and the tag components keep their own
   tests.

## enforcement

None beyond review. `grep -rn FEATURES packages/svelte/src frontend/src` lists
every place the flag hides something.

## rejected

| option | why not |
|---|---|
| delete the tag screens and components | the owner's decision still stands, and a flag brings them back with one line |
| hide tags on the server | a script or another client that sets tags through the API would lose them |
