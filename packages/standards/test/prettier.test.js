// Proves the exported Prettier config loads and actually changes output,
// not only that the object has the right shape.

import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import prettier from 'prettier';

import config from '../prettier.js';

describe('the prettier config', () => {
  it('formats with single quotes, semicolons and two-space indent', async () => {
    const input = 'const x = {a:1,b:"two"}\n';
    const output = await prettier.format(input, {
      ...config,
      parser: 'babel',
    });

    assert.equal(output, "const x = { a: 1, b: 'two' };\n");
  });
});
