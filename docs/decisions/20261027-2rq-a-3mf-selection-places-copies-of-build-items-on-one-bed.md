---
title: "ADR-20261027-2rq: a 3MF selection places copies of build items on one bed"
description: "The 3MF reader of the threemf extension arranges copies of build items onto as few beds of a setup as hold them, and Rewrite writes such a plate into a PrusaSlicer project by moving items and adding copies, because the PrusaSlicer command line does not arrange a 3MF project."
date: 2026-10-27
status: accepted
supersedes: []
amends:
  - ADR-20261026-w6a
tags:
  - adr
  - modules
---
# ADR-20261027-2rq: a 3MF selection places copies of build items on one bed

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Related: [ADR-20261026-w6a](20261026-w6a-a-3mf-project-is-read-by-a-core-reader-and-one-dialect-per.md)

The reader is the Go package `extensions/threemf/threemf` of the threemf
extension, and the gates below are its tests. A `Selection` of
ADR-20261026-w6a prints some copies that already stand on
one bed. A program that prints objects in numbers, such as "this clip twenty
times", needs more. It needs copies of an object that stand on no bed
together, or more copies than the project holds. PrusaSlicer does this with
Arrange in its window. Its command line does not do it for a project.
PrusaSlicer 2.9.6 sets `dont_arrange` for any project file it loads, in
`src/CLI/LoadPrintData.cpp`. 3.0.0-alpha12 does the same in
`src/slic3r-app-cli/src/Slic3r/App/CLI/CLIApp.cpp`. `--merge`,
`--duplicate` and `--center` arrange plain models and lose the project's
settings. So the copies are arranged here, before the slicer runs, and the
project the slicer gets already holds them where they stand.

## rules

### arranging

1. `threemf.Arrange` takes pieces and one setup. A piece is a build item and
   a number of copies. It answers plates, each a list of positions. A
   position is a build item and the point where the centre of its box
   stands, in the frame of the setup's bed shape. The same position means
   the same spot on every bed of the setup.
2. A copy keeps the rotation, the scale and the height of its build item. It
   is only moved.
3. A copy is placed by its outline, the convex hull `Read` computed. Two
   copies keep `Gap` between their outlines along an edge normal of one of
   them, so they are never closer than `Gap`. The edge of the bed keeps half
   a gap. `Gap` is 6 mm unless the caller names another. PrusaSlicer's
   arrange keeps 6 mm on an FFF printer.
4. Larger copies are placed first. Each copy takes the lowest, then
   leftmost, spot on the first plate where it fits. The spots tried are the
   edge of the bed and the far sides of the copies already placed. A copy
   that fits no plate starts a new one. When a plate is done, its copies
   move together to the centre of the bed if they still fit there.
5. The same pieces arrange the same way every time. So plates made of the
   same copies are equal, and a caller slices them once.
6. A setup that prints with a wipe tower, in a project that uses more than
   one extruder, keeps a square free for the tower. Its corner is
   `wipe_tower_x` and `wipe_tower_y`, and its side is `wipe_tower_width`.
7. A copy that does not fit an empty bed is `ErrDoesNotFit`, and so are
   copies that need more than `MaxPlates` plates. A piece the project does
   not have, an item named twice, no copies and more than `MaxCopies`
   copies are `ErrSelection`.

### rewriting

8. `Selection.Positions` replaces `Selection.Items`. A selection names one or
   the other.
9. The first position of a build item moves the item to that spot of the
   chosen bed. Each further position of the same item adds a build item
   after the last one. The new item takes the attributes of the item it
   copies, is moved, and is printable. It carries no attribute with a
   namespace prefix, so it takes no `p:UUID` or `p:path` of that item.
10. Every item that has no position is not printable, as an item outside a
    selection is today.
11. Each copy must stand inside the outline of the bed shape, and no two
    copies may overlap. Otherwise the selection is `ErrSelection`.
12. A PrusaSlicer 2 project moves the chosen bed onto bed 0 as before, and
    the added copies move with it. A PrusaSlicer 3 project keeps the chosen
    bed where it stands. Its build is rewritten only for a selection with
    positions.
13. The project's printer, print and filament settings stay as they are.
    Only which copies print, and where they stand, changes.

## enforcement

| rule | gate |
|---|---|
| 1, 3, 4 | `TestArrangePutsCopiesOntoAsFewPlatesAsHoldThem`, `TestArrangeMixesObjectsLargestFirst` |
| 5 | `TestArrangeMakesEqualPlatesOfEqualCopies` |
| 7 | `TestArrangeRefusesWhatItCannotPlace` |
| 9, 10, 12 | `TestRewritePlacesCopiesOnTheChosenBed`, for PrusaSlicer 2.9.6 and 3.0.0-alpha12 projects |
| 11 | `TestRewriteRefusesPositionsTheBedCannotHold` |
| 9, 12, 13 | `TestRewrittenCopiesSliceWithPrusaSlicer`, which needs the slicer binaries |
| 6 | none. Review holds it |

`TestRewrittenCopiesSliceWithPrusaSlicer` slices the copies with the real
PrusaSlicer 2.9.6 and 3.0.0-alpha12 and counts the objects the G-code labels.
It needs the binaries, so no gate runs it.

## rejected

| option | why not |
|---|---|
| `prusa-slicer --merge` over the objects exported as STL | the copies would lose the project's settings, modifiers, painting and object settings |
| turn a copy to fit more on a bed | a part is turned for its strength and its supports, and a person chooses that |
| nest the meshes exactly | PrusaSlicer itself arranges by the outline, and the hull is already in `Project` |
| an `Items` list that names one item several times | an item has one transform, and a copy needs a spot of its own |

## open items

- the depth of a wipe tower depends on the purge volumes, which are not
  read. The square of rule 6 can be too small for a tower of many colours
