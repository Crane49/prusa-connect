# UI integration

A UI change lands in an application that already exists. The bar is "could
somebody tell this was added later", and "does this feature work" is only
the entry ticket. A screen that works and looks imported from another product
is a regression, and it is the single most common way an agent-built
interface fails review.

Read the app before you draw. The reference is the running application,
never a generic idea of what the component should look like.

## Before writing any markup

Do all of these. Grep before you write, the same rule the code follows.

1. open the screen it will live on, plus the two screens nearest to it in the navigation. Read them, do not skim the filenames
2. grep the primitives folder (`lib/ui/`, the component library under `packages/`). Button, input, select, checkbox, dialog, table, empty-state, skeleton: if one exists, use it. A raw `<button>` beside a `ui/button` is drift, and the two will diverge on colour, size and focus ring within a week
3. read the tokens file. Colours, spacing scale, radii, typography, motion. A hardcoded `#3b82f6` or `12px` beside a token is the same defect as a magic number in business logic
4. find the shells the screen must register with: route and nav registry, permission or capability gating, i18n catalogue, theme (light and dark), layout container
5. name the pattern you are copying. If there is no neighbour to copy, say so and say what you based it on instead. That is the moment to check the direction before building it

If a neighbour does the wrong thing, flag it and fix it separately. Never
quietly introduce a competing pattern beside it.

Then ask the question one level up, before the markup: which other screens
show this same object, and do they owe this too? A panel built on the one
page it was requested for, while the same printer or job is displayed in
four other places, is not finished. Write that inventory into the task.

## What integrating means

| axis | the requirement |
|---|---|
| primitives | composed from the existing ones. No raw controls, no second button implementation |
| tokens | every colour, length, radius and duration comes from the scale |
| states | loading, empty, error, refused-by-permission and partial are all designed. None falls through |
| copy | goes through the i18n catalogue with a real key, in every language it carries. No hardcoded strings |
| theme | correct in light and dark, both checked |
| a11y | reachable and operable by keyboard, focus visible, labels bound, live regions only where an announcement is wanted |
| density | matches the surrounding screens. A spacious card beside a dense table looks unfinished |
| responsive | works at the widths the app ships: embedded panes and sidebars, and a desktop viewport |

## Absence is an answer, and it has more than two states

A blank panel says "we could not learn this". That is a different statement
from "there is nothing here", and usually a different one from "this step has
not happened yet". Model each state that exists and give it its own
sentence. Collapsing them prints a false claim in the one place a user trusts
most.

The same rule for a value: a figure with no basis, a figure we could not
fetch, and a real zero are three facts. Render three.

## Hierarchy before decoration

The question the screen exists to answer must be answerable at a glance,
without reading every row. A generic list of every field, correct and evenly
weighted, answers nothing. It moves the work of finding the important thing
back to the person reading it.

- lead with the answer, support it with the evidence
- rank by consequence, and let the ranking be visible: position, weight, colour
- colour encodes state through one variable per state, so a state nobody styled renders visibly unstyled instead of silently taking a default branch
- data density is a feature. Whitespace that pushes the third fact off the screen is a defect

## Verify it in a real browser

Anything whose symptom is a measurement cannot be seen by a unit test or a
compiled-stylesheet assertion. Overflow, clipping, a column that does not
fill, a head that scrolls away, focus landing off-screen are all
measurements. Those tests confirm a declaration exists. They say nothing
about how the box behaves.

- drive the real app and look at the screen, at more than one width
- assert on the measurement itself when you pin it, and mutation-check each rule separately. A redundant rule looks necessary forever if you remove three at once
- read the console. A framework state error or a CSP refusal is fatal in Chromium and no unit assertion reads it

## The review is required here

UI is the surface where "it works" and "it is right" diverge most, so a UI
change carries the adversarial review in `adversarial-review.md`. The
reviewer is asked one extra question: does this look and behave like it
belongs in this application, given the neighbouring screens and never the
diff alone.
