---
title: "ADR-20261029-pgw: an arranged copy turns only when it fits no bed as it stands"
description: "Arrange turns a copy by a right angle about the vertical axis only when it fits no empty bed as it stands, and keeps the default wipe tower place free when the settings name none."
date: 2026-10-29
status: accepted
supersedes: []
amends: [ADR-20261027-2rq]
tags:
  - adr
  - 3mf
---
# ADR-20261029-pgw: an arranged copy turns only when it fits no bed as it stands

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Amends [ADR-20261027-2rq](20261027-2rq-a-3mf-selection-places-copies-of-build-items-on-one-bed.md)

ADR-20261027-2rq keeps every copy as its build item stands. It refuses to turn a copy to fit more on a bed, because a person turns a part for its strength and its supports. A part that fits the bed only lying across was then refused outright, and a person had to turn it in the slicer and save the project again.

A turn about the vertical axis changes neither the layers nor the supports, so it is not the turn that rule protects. The settings of a setup that prints with a wipe tower may also name no place for it. Arrange then kept nothing free, although PrusaSlicer puts the tower at its default place.

## rules

1. A copy that fits no empty bed as it stands is turned by a right angle about the vertical axis through the centre of its box, when it fits turned. Rule 2 of ADR-20261027-2rq holds for every other copy, so no copy is turned to fit more on a bed.
2. A position carries the turn as `rotate`, 0 or 90 degrees. `Rewrite` writes it into the transform of the copy's build item, turning it about the centre of the item's box before it moves. Any other value is refused.
3. A setup may print with a wipe tower in a project of more than one extruder, and name no `wipe_tower_x` or `wipe_tower_y`. It then keeps the place PrusaSlicer gives the tower free: 180 and 140 millimetres from the bed's origin, as wide as `wipe_tower_width` or 60 millimetres.

## enforcement

`TestArrangeTurnsACopyThatFitsOnlyTurned` holds rule 1, `TestRewriteTurnsATurnedCopy` rule 2 and `TestArrangeKeepsTheDefaultWipeTowerFree` rule 3, all in `extensions/threemf/threemf`.

## rejected

| option | why not |
|---|---|
| turn any copy that would fill a bed better | it is the turn ADR-20261027-2rq refuses, and a person chooses that |
| turn by any angle | a copy that fits only at an angle is rare, and a right angle keeps the outline a box |
| keep nothing free when the settings name no tower | PrusaSlicer puts the tower at its default place, and a copy there would be sliced into it |
