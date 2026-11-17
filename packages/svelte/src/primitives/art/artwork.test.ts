import { readFileSync, statSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

import { describe, expect, it } from 'vitest';

// The drawing files themselves, held to what makes artwork a quiet layer.
// The component names a file and the application serves
// it, so nothing here is bundled and nothing here is type-checked. This is the
// gate that reads the files.
//
// A drawing is one colour, `currentColor`, because it is drawn as a mask over
// the element's own text colour. That is what lets one file stand in the light
// and the dark theme. A second tone comes from opacity on that one colour and
// never from a second colour, which is what the colour rule below holds.
import { ART_NAMES } from './types';
import { ART_FILE } from './variants';

const here = dirname(fileURLToPath(import.meta.url));
const folder = join(here, '../../../../../frontend/public/img/art');

/** What a person can wait for. Anything larger is a picture, not a drawing. */
const LIMIT = 8 * 1024;

const COLOUR = /(?:fill|stroke|stop-color|color)\s*=\s*"([^"]*)"/g;
const ALLOWED = ['currentColor', 'none'];

const drawings = ART_NAMES.map((name) => {
  const path = join(folder, ART_FILE[name]);

  return { name, path, text: readFileSync(path, 'utf8') };
});

describe('the drawings', () => {
  it.each(drawings)('$name says its box', ({ text }) => {
    expect(text).toMatch(/<svg[^>]*viewBox="[\d\s.-]+"/);
  });

  it.each(drawings)('$name is drawn and not photographed', ({ text }) => {
    expect(text).not.toMatch(/<image\b/);
    expect(text).not.toMatch(/base64/);
  });

  it.each(drawings)('$name names one colour and no other', ({ text }) => {
    const named = [...text.matchAll(COLOUR)].map((hit) => hit[1] ?? '');
    const off = named.filter(
      (value) => !ALLOWED.includes(value) && !value.startsWith('url(#'),
    );

    expect(off).toEqual([]);
  });

  it.each(drawings)('$name is small enough to arrive at once', ({ path }) => {
    expect(statSync(path).size).toBeLessThan(LIMIT);
  });
});
