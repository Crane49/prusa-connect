---
title: "ADR-20261023-r3x: every component and every screen state has a story"
description: "Every component a barrel exports has a story, and every screen's story file shows its loading, failed and refused states, and its empty state when the screen is a list. Two tests refuse a gap."
date: 2026-10-23
status: accepted
tags:
  - adr
  - frontend
---
# ADR-20261023-r3x: every component and every screen state has a story

- status: accepted
- date: 2026-10-23

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Rule file: [storybook](../../.agents/rules/storybook.md)

The designer works from the Storybook. A story is one drawing of a component
or a screen in one state, kept in a `.stories.svelte` file. A component with no
story, or a screen state with no story, does not exist for design. Nobody draws
it, and nobody reviews how it looks. These rules keep the Storybook complete,
and two tests refuse a change that leaves a gap.

## rules

1. Every `.svelte` component that a barrel `index.ts` exports has a story. The
   rule covers `frontend/src/lib/` and the `primitives`, `shell`, `slots` and
   `data` folders of `packages/svelte/src/`.
2. A story file covers a component when it imports the component's `.svelte`
   file or the barrel of the component's folder. A story-only wrapper counts
   too: a `.svelte` file that no barrel exports, which the story imports and
   which imports the component. An import through a package name does not
   count, so a library component needs a story in the library.
3. A private leaf subcomponent that its folder's barrel does not export needs
   no story of its own. Its parent's stories show it.
4. Every screen in `SCREENS` has a story file beside it, except a screen that
   only sends the browser to another address.
5. A screen's story file has stories named exactly `Loading`, `Failed` and
   `Refused`: the request still open, answered with a server error, and
   answered with 403.
6. A screen whose page is a list of records also has a story named `Empty`.
   The list of those screens is `LISTINGS` in
   `frontend/src/lib/screens-storied.test.ts`.
7. Some states need a person to type or click first, and some a screen does
   not draw. Such a state is named in the story file's top comment, on a line
   of its own: `Omitted Refused: every signed-in person may read this page.`
8. A change to a component, a screen, or a branch a screen draws on data
   state updates its stories in the same commit.
9. `packages/svelte/stories.baseline` lists the library components with no
   story yet, each with its reason. A line is deleted when the component gets
   a story. A line is never added.

## enforcement

| gate | what it holds |
|---|---|
| `packages/svelte/src/stories-cover-components.test.ts` | rules 1, 2 and 9 for the library, against the baseline |
| `frontend/src/lib/stories-cover-components.test.ts` | rules 1 and 2 for the app, with no baseline |
| `frontend/src/lib/screens-storied.test.ts` | rules 4 to 7 |

Rule 8 has no gate. A test cannot tell a new branch from an old one, so a
reviewer holds it.

## rejected

| option | why not |
|---|---|
| require a sibling `<name>.stories.svelte` for every component | a compound part such as `card-title` is shown by its folder's story, and would need a file that repeats it |
| count a component drawn anywhere inside a story, at any depth | one screen story would cover every part it draws, and the parts' own states would go unseen |
| read the built Storybook's index for coverage | the build takes minutes and needs the library built first, so the check would leave the unit test run |
| a `<script module>` export to declare an omitted state | an export is code in the story bundle for a fact only the test reads. A comment line is enough |

## open items

- the components in `packages/svelte/stories.baseline` have no story yet
