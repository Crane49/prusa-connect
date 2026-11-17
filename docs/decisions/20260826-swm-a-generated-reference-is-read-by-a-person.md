---
title: "ADR-20260826-swm: a generated reference is read by a person"
description: "A vocabulary page renders one anchored entry per item, addressed by its own identifier, and puts prose outside table cells. Generation is not an excuse for a layout nobody can read."
date: 2026-08-26
status: accepted
tags:
  - adr
  - documentation
  - accessibility
  - generation
---
# ADR-20260826-swm: a generated reference is read by a person

- status: accepted
- date: 2026-08-26

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Builds on [ADR-20260825-bhe](20260825-bhe-every-vocabulary-is-a-gated-published-resource.md)

[ADR-20260825-bhe](20260825-bhe-every-vocabulary-is-a-gated-published-resource.md) rule 7 says a
vocabulary generates a published reference. It says nothing about that reference
being readable, so six generators each chose a table and produced **435 entries
across 39 tables, of which 55 can be linked to**. The error code page is seven
columns wide with 291 characters of prose in one cell, and its minimum rendered
width is roughly twice the width of a phone.

This record adds the missing half of rule 7. The measurements, the prior art and
the criticism are in earlier research.

## rules

1. An entry in a published vocabulary is a **heading**, not a table row. One
   entry, one heading, one anchor.
2. The heading text carries the entry's identifier **unchanged and first**, so
   the string a reader arrived with is the string they see and the first thing
   they scan.
3. The anchor is **derived from the identifier by one pure documented
   function**, shared by every generator, and never from the prose. A reader
   holding `core.printer/GCODE_NOT_ALLOWED` from a 409 response constructs
   `#core-printer-gcode-not-allowed` without visiting the page.
4. The anchor function is **covered by a test that fails when an anchor
   changes**. An anchor that moves is a broken link in somebody's incident
   notes, and nothing else in the build would notice.
5. A one-sentence **summary is the first thing under the heading**, before any
   facts block. It is what a search engine shows and what a scanner reads.
6. An entry's fixed facts are a **labelled list**, and every value must survive
   being read aloud. A status carried only by a colour is refused: see
   WCAG 2.2 SC 1.4.1.
7. **Prose never goes in a table cell.** A cell holds a short value: an
   identifier, a number, a flag, an enum member.
8. A table is permitted for exactly two jobs: a **navigational index** over
   entries that are documented in full below it, and a genuine comparison where
   rule 7 already holds. Anything else is a list or a set of headings.
9. **Heading levels never skip.** A group is `<h2>`, an entry is `<h3>`, and an
   entry's own sections are `<h4>`. Starlight builds its table of contents from
   `<h2>` and `<h3>`, so an entry is reachable from it by construction.
10. A page **reflows to 320 CSS pixels** with no horizontal scrolling of the
    document, which satisfies WCAG 2.2 SC 1.4.10 and the owner's 375 px target
    together. A table that survives rule 8 scrolls inside its own container and
    keeps its `<th>` and `<td>` markup.
11. A generator emits **markdown structure, not hand-rolled HTML**. Measured:
    a raw HTML heading keeps its id and never reaches the table of contents,
    because `rehypeRaw` runs after heading collection. Hand-rolled markup costs
    the navigation route 71.6 % of screen reader users take.
12. Where markdown cannot express something the rules require, the gap is
    closed **once in the docs pipeline** and used by every generator, not worked
    around per generator.
13. A rule here binds **every** generated reference page, present and future. A
    new vocabulary inherits this record along with
    [ADR-20260825-bhe](20260825-bhe-every-vocabulary-is-a-gated-published-resource.md) rule 12.

## what needs the owner

Three calls are genuinely his and the rules above are written so that a
different answer changes the implementation and not the principle.

| call | the default taken | what a different answer costs |
|---|---|---|
| a rehype plugin in `docs/astro.config.mjs` to set explicit heading anchors | take it: about 30 lines, no new dependency, and it is the only measured route to rule 3 that keeps rule 9 | without it, anchors stay `#coreprintergcode_not_allowed` and rule 3 is unmet |
| whether large pages keep a compact index table above the entries | keep it on pages over about 20 entries, because the scan task is real and rule 8 permits it | dropping it makes the capabilities page a 151-heading scroll with no overview |
| converting the pages to `.mdx` to reach Starlight's `Badge` and `Card` | do not: it adds `@astrojs/mdx` for styling only, and markdown lists already satisfy rule 6 | a prettier fact strip, at the cost of a build dependency and six file renames |

## enforcement

| rule | gate |
|---|---|
| 1, 2, 5, 6, 7, 8, 9 | the six `-check` targets. The committed page is diffed against the generated one, so a rendering change that is not regenerated fails CI |
| 3, 4 | `cmd/generate-error-codes/anchor_test.go`, a table-driven test pinning the exact anchor for a fixed set of identifiers, plus a duplicate-anchor check that stops the generator rather than publishing two sections at one link |
| 10 | `make docs-tables-check` over the built HTML. Its coverage is partial; see open items |
| 11, 12 | review, and rule 1's diff catches the visible result |
| 13 | [ADR-20260825-bhe](20260825-bhe-every-vocabulary-is-a-gated-published-resource.md) rule 12 already requires a new vocabulary to ship its page |

`docs/scripts/check-table-regions.mjs` holds one part of rule 10: every table in
the built site sits inside a labelled, focusable scroll region. It runs from
`make docs-tables-check` and from the `docs` CI job. Nothing checks for a
skipped heading level or an unbreakable string, and a person still checks reflow
at 320 and 375 px.

## rejected

| option | why not |
|---|---|
| keep the tables and wrap them in horizontal scroll containers | preserves semantics, not readability. SC 1.4.10's data-table exception covers tabular data, not prose put in a cell |
| the usual responsive-table CSS that restacks cells on narrow screens | changing `display` on a table removes its semantics: *"screen readers no longer consider this to be a table"*. It trades a mobile problem for an accessibility problem |
| hand-roll HTML per entry for a richer layout | measured: a raw HTML heading never enters the table of contents, so it breaks rule 9 to satisfy rule 2 |
| accept the default github-slugger anchors | `core.printer/GCODE_NOT_ALLOWED` becomes `#coreprintergcode_not_allowed`, which drops the domain separator and cannot be constructed by a reader holding the code |
| convert the six pages to `.mdx` for Starlight components | a build dependency for styling, when the accessibility requirement is met by a markdown list |
| one shared renderer across the six generators | [ADR-20260825-bhe](20260825-bhe-every-vocabulary-is-a-gated-published-resource.md) already rejected it on shape grounds. Share the rules and the anchor function, not the renderer |
| hand-write the pages | 128 capability keys a person transcribes are wrong within a release. The owner's constraint is the balance: generate the enumerable part, hand-write the argument around it |
| restructure the site into Diátaxis quadrants | its own users' loudest objection is the extra click it puts in front of reference, and these pages are already one click from the sidebar |
| a colour-coded status pill | SC 1.4.1 refuses colour as the only carrier. A pill whose text says the same thing is fine and is not worth the extra work |

## open items

- **Two of rule 10's three parts have no gate**. A skipped heading level and an
  unbreakable string outside a wrapped element are both checked by a person.
  `docs/scripts/check-table-regions.mjs` holds the scroll region and nothing
  else.
- **The semantics claim in the rejected table has not held since Chrome 151**.
  `Accessibility.getFullAXTree` over `/docs/prusa-command-architecture/` reports
  `table 1, row 54, cell 424, columnheader 8` under three configurations. As
  Starlight ships it, with `display: table` forced, and with table, rows and
  cells all forced to `display: block`. The wrapper is still the right fix. It
  repairs the keyboard defect below, and it covers the engines nobody measured
  here. The cell states the claim without a browser or a date against it.
- **The scroll container was unreachable by keyboard, and the wrapper repairs
  that**. `/docs/prusa-command-architecture/` at 320 CSS px carried a 1479 px
  table inside a 288 px box. No `tabindex` sat on it or on any ancestor.
  1191 px of content answered a pointer and nothing else. That is
  WCAG 2.2 SC 2.1.1, and it applied to every table on the site.
- A generated page cannot carry a worked example, and every documentation set
  people praise has one. Rules 1 to 13 make an entry findable and readable, and
  do not give it an example. Closing that needs either an example field on the
  descriptors or hand-written prose beside the generated block, and neither is
  decided here.
- The capabilities page will have about 151 `<h3>` entries and therefore a table
  of contents of the same length. Rule 8's index table is the mitigation, and
  whether it is enough is unmeasured.
- `docs/decisions/README.md` does not index 0005 to 0012. The index names that
  gap and the old backlog owns it.
