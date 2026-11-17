// Every component a barrel under src/lib exports has a story (ADR-20261023-r3x). The
// app has no baseline: every such component has one.
import { fileURLToPath } from 'node:url';

import { expect, test } from 'vitest';

import {
  missingMessage,
  unstoried,
} from '../../../packages/svelte/src/test/story-coverage';

const ROOT = fileURLToPath(new URL('../..', import.meta.url));

test('every barrel component has a story', () => {
  expect(unstoried(ROOT, ['src/lib'], ['src']).map(missingMessage)).toEqual([]);
});
