import { flushSync } from 'svelte';
import { expect, it } from 'vitest';

import { AppVersion } from './app-version.svelte';

it('names the build the server reports, and nothing when it refuses', async () => {
  const answering = new AppVersion({
    getHealth: () =>
      Promise.resolve({
        status: 'ok',
        version: '0.2.0',
        instance_id: '8f14e45f-ceea-4e7a-9c2b-3d1f6a5b7c90',
      }),
  });
  const refusing = new AppVersion({
    getHealth: () => Promise.reject(new Error('down')),
  });

  await Promise.all([answering.load(), refusing.load()]);
  flushSync();

  expect(answering.version).toBe('0.2.0');
  expect(refusing.version).toBeUndefined();
});
