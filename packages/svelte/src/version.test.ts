import { readFileSync } from 'node:fs';

import { describe, expect, it } from 'vitest';

import { LIBRARY_VERSION } from './version';

describe('LIBRARY_VERSION', () => {
  it('is the version package.json publishes', () => {
    const manifest: unknown = JSON.parse(
      readFileSync(new URL('../package.json', import.meta.url), 'utf8'),
    );
    // A parsed JSON file is `unknown` until something narrows it (typescript.md).
    const version =
      typeof manifest === 'object' &&
      manifest !== null &&
      'version' in manifest &&
      typeof manifest.version === 'string'
        ? manifest.version
        : undefined;

    expect(version).toBe(LIBRARY_VERSION);
  });
});
