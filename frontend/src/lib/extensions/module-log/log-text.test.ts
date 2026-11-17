import { expect, it } from 'vitest';

import { logText } from './log-text';

it('joins a split line to its continuation', () => {
  const at = '2026-10-30T10:00:00Z';

  expect(
    logText({
      lines: [
        { at, text: 'one ', split: true },
        { at, text: 'line' },
        { at, text: 'next' },
      ],
    }),
  ).toBe('one line\nnext');
});
