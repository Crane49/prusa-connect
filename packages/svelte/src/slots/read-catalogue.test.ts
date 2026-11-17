// A catalogue read that never answers must not hold up the modules after it.
import { afterEach, describe, expect, it, vi } from 'vitest';

import { readCatalogueWithin } from './read-catalogue';

// A fetch that never answers, and gives up only when its signal aborts.
const silent = (): typeof fetch => (_input, init) =>
  new Promise((_resolve, reject) => {
    init?.signal?.addEventListener('abort', () => {
      reject(new DOMException('aborted', 'AbortError'));
    });
  });

describe('reading a module catalogue', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('gives up on a request that never answers at the deadline', async () => {
    vi.stubGlobal('fetch', silent());

    await expect(
      readCatalogueWithin('/locales/en.json', undefined, 20),
    ).resolves.toBeUndefined();
  });

  it('stops when the run signal aborts, before the deadline', async () => {
    vi.stubGlobal('fetch', silent());
    const run = new AbortController();

    const read = readCatalogueWithin('/locales/en.json', run.signal, 60_000);
    run.abort();

    await expect(read).resolves.toBeUndefined();
  });
});
