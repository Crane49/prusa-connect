# UI integration

A UI change lands in an application that already exists. The bar is "could
somebody tell this was added later", and "does this feature work" is only
the entry ticket. A screen that works but looks imported from another product
is a regression, and it is the most common way an agent-built interface fails
review.

Read the app before you draw. The reference is the running application,
never a general idea of what the component should look like.

## Before writing any markup

Do all of these. Search before you write, the same rule the code follows.

1. open the screen it will live on, plus the screens nearest to it in the navigation. Read them; do not skim the filenames
2. search the primitives folder (`lib/ui/`, the component library under `packages/`). A primitive is a small shared component. Button, input, select, checkbox, dialog, table, empty-state, skeleton: if one exists, use it. A raw `<button>` beside a `ui/button` is drift, and the two will differ in colour, size and focus ring within a week
3. read the tokens file. Colours, spacing scale, radii, typography, motion. A `#3b82f6` or `12px` written beside a token is the same defect as a magic number in business logic
4. find the shells the screen must register with: route and nav registry, permission or capability gating, i18n catalogue, theme (light and dark), layout container
5. name the pattern you are copying. If there is no neighbour to copy, say so and say what you based it on instead. That is the moment to check the direction before building it

If a neighbour does the wrong thing, flag it and fix it separately. Never
quietly introduce a competing pattern beside it.

Then ask the question one level up, before the markup: which other screens
show this same object, and do they owe this too? A panel built on the one
page it was requested for, while the same printer or job is displayed in
several other places, is not finished. Write that inventory into the task.

## What integrating means

| axis       | the requirement                                                                                                                                                                                                                                                                          |
| ---------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| primitives | composed from the existing ones, no raw controls. The shared ESLint preset refuses them, see Primitives below |
| tokens     | every colour, length, radius and duration comes from the scale                                                                                                                                                                                                                           |
| states     | loading, empty, error, refused-by-permission and partial are all designed. None falls through                                                                                                                                                                                            |
| copy       | goes through the i18n catalogue with a real key, in every language it carries. No strings written in the component                                                                                                                                                                       |
| filters    | search and filters only from `ListingSearch` and `ListingToolbar`; see Filters below                                                                                                                                                                                                     |
| theme      | correct in light and dark, both checked                                                                                                                                                                                                                                                  |
| a11y       | reachable and usable by keyboard, focus visible, labels bound, live regions only where an announcement is wanted                                                                                                                                                                         |
| density    | matches the surrounding screens. A spacious card beside a dense table looks unfinished                                                                                                                                                                                                   |
| responsive | works at the widths the app ships: embedded panes and sidebars, and a desktop viewport                                                                                                                                                                                                   |

### Filters

Every place with filters has a button that resets them. A search box has a
clear X and takes Escape. While any filter is on, "Clear filters" shows. A
filtered result with no rows offers "Clear search and filters"
(`ListingNoMatch`). `scripts/check-listing-search.sh` refuses a raw
`type="search"`; the pickers that are not listings are exempt, listed in the
script.

### Primitives

A raw element that a primitive covers fails lint. The rule is
`svelte/no-restricted-html-elements`, with selectors for `animate-spin` and
`role="alert"`, in the shared preset `packages/standards/eslint/shared.js`.
Find the thing you want in this table and use the primitive.

| you want                                         | use                                                                                                            |
| ------------------------------------------------ | -------------------------------------------------------------------------------------------------------------- |
| a button                                         | `Button` (`variant="link"` for a text link, `size="inline-icon"` for a small icon button), `Toggle` for on/off |
| a heading-size button                            | `TitleButton`                                                                                                  |
| a pressable or linked card                       | `Tile`                                                                                                         |
| the inside of a dashboard card                   | `TilePanel` (heading, `subline`, `trailing`)                                                                   |
| a small pressable or linked label                | `Chip`                                                                                                         |
| a state or count label                           | `Badge` (`mono`, `size`, `numeric`)                                                                            |
| a full-width notice bar with actions             | `Banner`                                                                                                       |
| a card with a tone                               | `Card` (`tone`)                                                                                                |
| a status card with a figure and an edge          | `StatusCard` (`figure`, `edge`)                                                                                |
| a text input, checkbox, radio, switch, file drop | `Input`, `Checkbox`, `RadioGroup`, `Switch`, `FileDropzone`                                                    |
| a colour field or a colour swatch list           | `ColorInput`, `SwatchPicker`                                                                                   |
| a searchable dropdown                            | `Combobox`                                                                                                     |
| a select                                         | `Select`, `SelectFacet` for a listing facet                                                                    |
| a multi-line input                               | `Textarea`                                                                                                     |
| a label for a field                              | `Label` (`variant`), `Field`                                                                                   |
| a modal                                          | `Dialog`, `AlertDialog`, `Sheet`                                                                               |
| an expandable section                            | `Collapsible` (`CollapsibleTrigger` `variant` and `tone`)                                                      |
| a divider                                        | `Separator`                                                                                                    |
| a table, and a sortable column head              | `Table`, `SortableTableHead`                                                                                   |
| a frame around `DataRow` lines                   | `RowList`                                                                                                      |
| tabs that change the route                       | `NavTabs`                                                                                                      |
| tabs on a detail page, with a count              | `DetailTabs` (`count`)                                                                                         |
| actions in the page title                        | `PageHeader`, with `Button` size `md`                                                                          |
| a trailing control in a listing bar              | `ListingToolbar` (`trailing`)                                                                                  |
| a loading spinner                                | `Spinner`                                                                                                      |
| an alert or a fault with actions                 | `Alert`, `FaultAlert` (`actions`)                                                                              |
| a compact empty state                            | `EmptyState` (`size="sm"`)                                                                                     |

No caller class changes how a primitive looks: not its padding, radius,
colour, height or font. If you need another look, add a named variant to the
primitive, with a story. Layout classes (`flex-1`, `w-full`, `gap-2`, margins)
are fine.

If no primitive fits, extend one or add one, with a story. Do not hand-build
the element in the page.

Some code has a real reason to use a raw element. Put a disable on the line
above it, with the reason:

```svelte
<!-- eslint-disable-next-line svelte/no-restricted-html-elements -- ROW_LINK button over a DataRow -->
```

The accepted reasons are:

- the code is inside a primitive
- the browser element has a capability no primitive has, such as the kiosk
  fullscreen `<dialog>` or a native colour input
- the composition is documented: a Badge `child`, or a ROW_LINK
- a native radio inside its card label: radio-group semantics and arrow keys
  that no primitive gives (the appearance view cards, the setup wizard's
  choice cards)

Three sites stand as exceptions by name:

- `printer-switcher.svelte`: the two-up head switcher draws a printer picture
  and an underline that neither `ToggleGroup` nor `Tabs` does (spec-approved).
- `choose-material.svelte` in the materials add dialog: a full-width pressable
  picker row that no primitive draws (spec-approved).
- `rail.svelte`'s collapsed section trigger: it sits inside the rail's own
  `Tooltip` and must match the sibling nav-link rows, which Button's boxed
  sizes and its own label tooltip do not.

The gate checks that a disable exists, not that it carries a `-- reason`, and
`<svelte:element this="button">` gets past it. Review both by eye.

An unused disable fails lint too. A reason such as "faster" or "looks
different" is not accepted. Make the primitive do it.

## A blank panel is not one answer

A blank panel says "we could not learn this". That is a different statement
from "there is nothing here", and usually a different one from "this step has
not happened yet". Model each state that exists and give it its own
sentence. Collapsing them prints a false claim in the one place a user trusts
most.

The same rule for a value: a figure with no basis, a figure we could not
fetch, and a real zero are different facts. Render each one.

## Hierarchy before decoration

The question the screen exists to answer must be answerable at a glance,
without reading every row. A generic list of every field, correct and evenly
weighted, answers nothing. It moves the work of finding the important thing
back to the person reading it.

- lead with the answer, support it with the evidence
- rank by consequence, and let the ranking be visible: position, weight, colour
- colour encodes state through one variable per state, so a state nobody styled renders visibly unstyled instead of silently taking a default branch
- data density is a feature. Whitespace that pushes the next fact off the screen is a defect

## Verify it in a real browser

Anything whose symptom is a measurement cannot be seen by a unit test or an
assertion on the compiled stylesheet. Overflow, clipping, a column that does
not fill, a head that scrolls away, focus landing off-screen are all
measurements. Those tests confirm a declaration exists. They say nothing
about how the box behaves.

- drive the real app and look at the screen, at more than one width
- assert on the measurement itself when you pin it, and check each rule separately by removing it and watching the test fail. A redundant rule looks necessary forever if you remove several at once
- read the console. A framework state error or a CSP refusal is fatal in Chromium and no unit assertion reads it

## The review is required here

UI is the place where "it works" and "it is right" differ most, so a UI
change carries the adversarial review in `adversarial-review.md`. The
reviewer is asked one extra question: does this look and behave like it
belongs in this application, given the neighbouring screens and never the
diff alone.
