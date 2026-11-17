---
title: "ADR-20261105-74w: every listing ticks rows through one selection"
description: "How a person ticks several rows of a listing, and the one primitive every listing uses for it."
date: 2026-11-05
status: accepted
supersedes: []
amends: []
tags:
  - adr
---
# ADR-20261105-74w: every listing ticks rows through one selection

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md)

A listing lets a person tick rows and act on them together: delete files,
move them, send a print to several printers. Each listing used to keep its
own list of ticks. These rules follow what mail clients, Google Drive, Shopify
Polaris and IBM Carbon do, so the gestures a person already knows work here.

## rules

1. A listing that lets a person tick several rows keeps the ticks in one
   `Selection` from `@prusa/connect-ui/primitives`, keyed by the row's
   id. It never keeps its own array or set of ticked ids.
2. A row is ticked with its tick box, drawn by `SelectCheckbox`. A press on
   the row or the tile opens it, and never ticks it.
3. Shift with a press ticks or unticks every row from the last row pressed to
   this one, in the order the listing draws them. The whole range takes the
   new state of the row pressed.
4. A table's header holds a `SelectAllCheckbox` over the rows of the page on
   screen. It shows a dash when only some of them are ticked.
5. While some rows are ticked, the `BulkBar` offers "Select all N": every
   row of the folder or search, on every page.
6. A search, another folder or another drive is a new list, so the screen
   clears the selection. A tick the person can no longer see is never acted
   on: `Selection.keys` holds only the ticked rows still in the listing.
7. A listing whose ticks must survive a change of view, such as the beds of
   a 3MF project, gives `Selection` the rows of every view and draws one view.

## enforcement

`selection.svelte.test.ts` and `select-checkbox.svelte.test.ts` hold the
range, the header box and the pruning. Each listing's screen test holds its
own wiring. No gate refuses a listing that keeps its own ticks: a reviewer
points at rule 1.

## rejected

| option | why not |
|---|---|
| a press on a file tile ticks it, as in a desktop file manager | a press on a row already opens it, and one gesture cannot do both |
| the header box ticks every row on every page | a person cannot see what they ticked on other pages; the bulk bar offers it in a second step |
| keep the ticks hidden by a search and act on them | the person deletes files they cannot see |
| a long press on a touch screen starts ticking | the tick boxes and "Select all" cover touch; added when testers ask |
