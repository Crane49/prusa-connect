import { readFileSync, statSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

import { describe, expect, it } from 'vitest';

// The artwork itself, on the model of `art/artwork.test.ts`. The component
// names a file and the application serves it, so nothing here is bundled and
// nothing here is type-checked. This is the gate that reads the files.
//
// A model whose file is missing draws a broken image on every card in the
// fleet, and no browser test catches it: a component test never loads the URL.
//
// `art` holds its drawings to one colour and a small size, because a drawing
// is a quiet layer. These are the opposite: full-colour renders of a machine,
// between 8 kB and 156 kB each, and that is why the component loads them by
// URL instead of inlining them.
import { PRINTER_MODELS, printerArtworkFile } from './types';

const here = dirname(fileURLToPath(import.meta.url));
const folder = join(here, '../../../../../frontend/public/img/printers');

const artwork = PRINTER_MODELS.map((model) => ({
  model,
  path: join(folder, printerArtworkFile(model)),
}));

const PNG_SIGNATURE = Buffer.from([
  0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
]);

describe('the printer artwork', () => {
  it.each(artwork)('$model has a file to draw', ({ path }) => {
    expect(statSync(path).isFile()).toBe(true);
  });

  it.each(artwork)('$model is what its extension says', ({ path }) => {
    const file = readFileSync(path);
    if (path.endsWith('.png')) {
      expect(file.subarray(0, 8).equals(PNG_SIGNATURE)).toBe(true);
    } else {
      expect(file.toString('utf8')).toMatch(/<svg[\s>]/u);
    }
  });
});
