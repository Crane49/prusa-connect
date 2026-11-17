---
paths:
  - "**/*.svelte"
  - "**/*.stories.svelte"
---

# Storybook

The designer works from the Storybook. A component or a screen state with no
story does not exist for design. So every component and every state a screen
draws has a story. [ADR-20261023-r3x](../../docs/decisions/20261023-r3x-every-component-and-every-screen-state-has-a-story.md)
states the rules, and two tests hold them.

## When you write a story

- you add a component that a barrel `index.ts` exports: add
  `<name>.stories.svelte` beside it, in the same commit
- you change how a component looks or add a prop that changes how it looks:
  add or update the story for that look
- you add a screen to `SCREENS`: add `<screen>.stories.svelte` beside it
- you add a branch a screen draws on data state, such as an `{#if}` on a
  request that is loading, failed, refused or empty: add the story for it
- a state you cannot show is named in the story file's top comment, one line
  each: `Omitted Refused: every signed-in person may read this page.`

## How to write one

- a library component: draw it over fakes from `packages/svelte/src/test/`.
  Import the `.svelte` file by its relative path, the way
  `primitives/card/card.stories.svelte` does
- an app screen: mount the real app with `AppAt` from
  `frontend/test/story/app-at.svelte`, and change the server's answers with
  `overrides`. `frontend/test/story/api.ts` has `pending`, `failed`, `refused`
  and `emptyList`. The Storybook section of `frontend/README.md` shows one
- a screen's story names are exact: `Loading`, `Failed`, `Refused`, and
  `Empty` for a screen that is a list. Other states take any plain name

## Gates to run

| command | what it refuses |
|---|---|
| `pnpm --dir packages/svelte exec vitest run --project node src/stories-cover-components.test.ts` | a library component with no story, and a baseline line whose component now has one |
| `pnpm --dir frontend exec vitest run --project node src/lib/stories-cover-components.test.ts` | an app component with no story |
| `pnpm --dir frontend exec vitest run --project node src/lib/screens-storied.test.ts` | a screen with no story file, or a screen story file missing a state |
| `make frontend-storybook` | a story that does not build |

`packages/svelte/stories.baseline` lists the library components with no story
yet. Delete a line when you write the story. Never add one.
