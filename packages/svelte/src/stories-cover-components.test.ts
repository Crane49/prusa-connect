// Every component a barrel exports has a story (ADR-20261023-r3x). The baseline lists
// the components that have none yet, and only shrinks.
import { fileURLToPath } from 'node:url';

import { expect, test } from 'vitest';

import {
  baseline,
  missingMessage,
  staleMessage,
  unstoried,
} from './test/story-coverage';

const ROOT = fileURLToPath(new URL('..', import.meta.url));
const BASELINE = 'stories.baseline';

const gaps = unstoried(
  ROOT,
  ['src/primitives', 'src/shell', 'src/slots', 'src/data'],
  ['src'],
);
const allowed = baseline(`${ROOT}${BASELINE}`);

test('every barrel component has a story', () => {
  expect(
    gaps.filter((path) => !allowed.includes(path)).map(missingMessage),
  ).toEqual([]);
});

test('the baseline lists only components still without a story', () => {
  expect(
    allowed
      .filter((path) => !gaps.includes(path))
      .map((path) => staleMessage(path, `packages/svelte/${BASELINE}`)),
  ).toEqual([]);
});
