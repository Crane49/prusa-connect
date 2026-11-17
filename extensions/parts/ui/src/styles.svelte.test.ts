import { describe, expect, it } from 'vitest';

// The build scopes this module's utilities to its slot, and a scoped rule
// outranks the host's rule of the same weight. A utility this module emits
// therefore also wins on every library component drawn inside the page, over
// the library's own container variants of the same property.
//
// Two utilities did exactly that, found by measuring the page in the running
// app: the flex column stacked the detail header and the key-value list at
// every width, and the four-step gap shrank the gap of every Card. The module
// stacks with grid instead, and this holds its source to that. Tailwind reads
// class names out of comments too, which is why a comment counts.

const sources = import.meta.glob(
  ['./**/*.svelte', './**/*.ts', '!./**/*.test.ts'],
  {
    query: '?raw',
    import: 'default',
    eager: true,
  },
);

/**
 * The utilities this module may not emit, as Tailwind would read them. Each
 * name is put together here rather than written out, because this file is in
 * the tree Tailwind reads and a written name would emit the very rule.
 */
const FORBIDDEN = [
  new RegExp(`(?<![\\w-])${['flex', 'col'].join('-')}(?![\\w-])`),
  new RegExp(`(?<![\\w/-])${['gap', '4'].join('-')}(?![\\w.-])`),
];

describe('the module source', () => {
  it('is read at all', () => {
    expect(Object.keys(sources).length).toBeGreaterThan(20);
  });

  it('emits no utility that would outrank a library component inside the page', () => {
    const found = Object.entries(sources).flatMap(([file, text]) =>
      FORBIDDEN.filter((pattern) => pattern.test(text)).map(
        (pattern) => `${file}: ${pattern.source}`,
      ),
    );

    expect(found).toEqual([]);
  });
});
