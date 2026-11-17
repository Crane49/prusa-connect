---
title: "ADR-20260917-a7d: a detail screen is one kit"
description: "Every screen about one subject composes one header and one tab strip it does not write, the title has one face, every part draws its own skeleton, and an absent thing is answered by which kind of absence it is."
date: 2026-09-17
status: accepted
tags:
  - adr
  - frontend
  - svelte
---
# ADR-20260917-a7d: a detail screen is one kit

- status: accepted
- date: 2026-09-17

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | ADRs: [20260914-244](20260914-244-the-library-is-layered-and-an-import-only-goes-down.md), [20260916-yta](20260916-yta-the-shell-is-a-published-tier.md), [20260917-j7s](20260917-j7s-a-screen-takes-a-port-and-a-listing-is-one-kit.md)

[ADR-20260917-j7s](20260917-j7s-a-screen-takes-a-port-and-a-listing-is-one-kit.md) settled what
a listing screen composes. It said nothing about the other kind of screen:
the printer, the job, the file and the material each draw one subject.

Those four already agree, by copy, and no rule says so. All four carry a trail,
a picture of the subject, an `h1`, lines under it, and in one case the acts
offered on it. One of them had reached the point of naming its own container
`detail-header` by hand.

This record makes the agreement a kit, and settles three questions the four
screens answer differently today: which face a title takes, what a part draws
while it loads, and what the interface says about a thing that is not built.

## rules

1. A detail screen **composes the kit and writes none of it**:

   | part | owns |
   |---|---|
   | `PageTrail` | where the page sits and the way back out |
   | `DetailHeader` | the subject block: one picture, one `h1`, the lines under it, the acts |
   | `DetailTabs` | the tab strip, its overflow, and one panel per tab |

   The first two are every detail screen's. The strip is drawn only where the
   subject has more faces than fit at once: the printer has five, so it has a
   strip; a print and a file each have three parts that stack inside one card,
   and a strip over them is a control that hides what already fits. The design
   is what settles the question for a given subject, not this table.

2. `DetailHeader` takes the subject's parts as four snippets, `media`,
   `titleAside`, `meta` and `actions`, and the title as a string. `titleAside`
   is the one fact that belongs on the title's own line, drawn after it and
   wrapping with it: the printer's state badge, which under the name sits a
   second glance away from the thing it describes. The screen fills them and
   sets no geometry: the container query, the breakpoint where the acts take
   their own line, and the picture's size belong to the part.
3. The title has **one face**. It is the interface's sans face and it wraps. It
   never truncates, because a name the page is about is a name every character
   of which belongs on screen, and it never switches to mono, because two faces
   in one component is two design languages.
4. The trail is **not** inside the header. Three screens reach their parent by
   href. The filament module's Materials page reaches its parent by putting its
   listing back. So the crumb is the screen's own call, and `PageTrail` takes
   both.
5. `DetailTabs` is handed the open tab and hands back the one pressed. Which
   tab is open is the screen's to hold; the printer's detail holds it in the
   path, so a tab survives a reload and can be linked to.
6. A tab with **no panel draws the strip's fallback**, so a screen ships its
   frame before its tabs and is never broken in between. Which fallback is
   rule 9's question: the skeleton while a panel is on its way, the not-built
   answer where this build draws none. A skeleton that will not resolve is a
   page a person reads as broken.
7. **Every part draws its own loading state**, and it is the part's own shape:
   a header skeleton is a picture, a title and two lines, not a grey page. A
   part that can be read separately can be waited for separately.
8. **The printer's detail is the reference.** Where two detail screens disagree
   about the shape of the header, the strip or the skeleton, the printer's is
   the one that is right and the other one changes. This settles how a part is
   drawn, never whether a subject has one; that is the design's to say, and
   where the design and this record disagree about a shape, the design wins.
9. **Absence has three answers, and the interface says which one it is:**

   | kind | answer |
   |---|---|
   | no data, such as a printer with no camera | the design's own fallback, silently. A placeholder says "loading", which is a different and wrong thing |
   | no feature: the product does not build this yet | `EmptyState` where a panel has room for one, its own shared heading, and a sentence naming what is absent. In a chip or a row, the same sentence in a popover |
   | the server refused, as a history endpoint with no provider does | `describeFault`, which carries the registry code and the server's own line |

10. A not-built sentence names **what is absent**, in one sentence, and never
    promises a date. "This server does not publish a material catalogue yet."
    is the form; "coming soon" is not.

## enforcement

| rule | gate |
|---|---|
| 1, 2, 4 | review. The cheapest gate is a test refusing a `src/shell/**` component that declares its own `@container/detail-header`, and it is not written |
| 3 | `packages/svelte/src/primitives/detail-header/detail-header.svelte.test.ts`, which refuses `truncate`, `text-ellipsis` and `font-mono` on the `h1`. The class fixture cannot hold it: it is a copy of the React `components/ui` tree, and the React header is a layout component taking printer props rather than snippets |
| 6, 7 | a `*.svelte.test.ts` per screen, in real Chromium, asserting the fallback a tab with no panel draws and asserting the loaded state is absent while a part waits |
| 9, 10 | `packages/svelte/src/i18n/voice.test.ts` holds the sentence to the controlled vocabulary; the shared heading is one key, so a second wording has nowhere to live |

Rules 5 and 8 have no gate. Rule 8 is a tie-break for review and cannot have
one.

## rejected

| option | why not |
|---|---|
| one `DetailScaffold` with a slot per region | the same inversion [ADR-20260917-j7s](20260917-j7s-a-screen-takes-a-port-and-a-listing-is-one-kit.md) refused for listings: it puts the varying part in the frame and the fixed part in the caller. Eight slots is a frame the caller fills, not a part the caller uses |
| a `titleFace` prop, mono for a file name | the fork was real, because a file name is a value a person compares and copies, but it was chosen per screen and not per kind, so it became two design languages. Wrapping keeps every character, which is what the mono argument was actually for |
| mono for every title | mono is the value face. A printer's name is language, and the rest of the tree sets language in sans |
| `DetailHeader` owning the trail | the parent of one material on the filament module's Materials page is the same screen with its listing put away, not a URL. A part that takes an href would refuse that screen |
| marking every absent thing | a printer with no camera is not an unfinished product, and a wall of tiles saying so reads as a broken instance |
| extracting the header after four screens wrote one | three of the four are unwritten. Extracting later means writing it four times and reconciling four spellings |
| a separate skeleton component per screen | the skeleton is the part's own shape. A screen that writes it draws a skeleton that stops matching the part the first time the part changes |

## open items

- The not-built sentence appears in the old backlog for the camera's live view, the log
  dump, the firmware update and the printer's network. Each is a feature with no API, not a screen with a bug.
- Rules 1, 2, 4, 5 and 8 have no gate, as the enforcement section says.
- The live camera view is the old backlog.
  The refreshing snapshot is not: the core serves it and the library already
  holds the store.
