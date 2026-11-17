---
title: "ADR-20261029-dgv: a file page has tabs a module may add to, and primary file actions"
description: "A file's page is a header and tabs. A module adds a tab in the file-tab slot, draws the Contents tab through file-view, and puts a primary file action in the header as a button."
date: 2026-10-29
status: accepted
supersedes: []
amends: [ADR-20260911-4p6, ADR-20261027-wvj]
tags:
  - adr
  - extensions
  - storage
---
# ADR-20261029-dgv: a file page has tabs a module may add to, and primary file actions

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Amends [ADR-20260911-4p6](20260911-4p6-a-module-extends-the-ui-through-one-library-and-four-slots.md), [ADR-20261027-wvj](20261027-wvj-a-module-extends-how-the-core-reads-a-file-and-draws-a-file.md)

A file's page drew its facts, its prints and its contents as one long card. A module's view sat at the bottom of it, under a list most people never read. The page offered nothing a module could add beside that view. A module could only put an item in the menu, so the one act that mattered for a file, such as slicing a model, sat behind three dots.

The page now works like a printer's page. The core draws a plain page for any file. A module that knows a file type draws more of it, and a module that can do something with a file offers it where people look.

## rules

1. A file's page is a header and a strip of tabs. The header holds the picture, the name, one line with the size and the date, a badge only for a fact such as read-only, and the actions.
2. The page's own tabs are Contents, Prints and Details, in that order. Contents opens first.
3. Contents draws a module's `file-view` for the file's type, or the core's own part. The page draws no card around a module's view, so the view lays out its own cards. The view's title is read only by a screen reader, because the tab already names it.
4. Prints is on the page only where the app lists the prints of a file. A page never draws a tab that only says the list is missing.
5. Details holds the facts the storage answered: where the file is, its size, the date it changed and its checksum.
6. `file-tab` is a slot on the storage screen, in the area `file-tabs`, of the section form, handed the file reference. Each entry is a tab after the page's own, in install order, and may name `file_types`.
7. The open tab is in the address as `?tab=`. Contents leaves it out, and a module tab is `<namespace>.<id>`. A tab that is not on the page opens Contents.
8. An entry of `file-action` may set `primary: true`. On the file's own page it is a button in the header, after the core's Print. In the listing and in every other menu it stays a menu item.
9. Only a slot whose table row allows it takes `primary`. Today that is `file-action`, and a manifest that sets it on another slot is refused.
10. The core decides nothing about slicing. It draws no badge that says a file needs slicing, because only a module can slice it.

## enforcement

`TestAFilePageTakesATabAndAPrimaryAction` and `TestOnlyAFileActionIsPrimary` hold rules 6, 8 and 9 in the manifest. The slot gates hold the generated table, the reference and the scaffold template for rule 6. The `FileDetailScreen` tests hold rules 1 to 5, 7 and 10. The `SlotActions` test holds rule 8 in the browser, and the every-slot test finds the `file-tab` outlet on its screen.

## rejected

| option | why not |
|---|---|
| a `file-section` slot under the contents | a long page of sections is what this record replaces. A tab keeps each part apart |
| a slot of its own for a primary action | it would repeat `file-action` with one key changed, and a module would declare one act twice |
| `primary` on `device-action` too | the printer's header has no room planned for it yet. The table row can allow it later |
| a slicing badge or button in the core | the core cannot slice, so it would promise what only a module does |
| the open tab in the path | the file's path already fills the rest of the address |
