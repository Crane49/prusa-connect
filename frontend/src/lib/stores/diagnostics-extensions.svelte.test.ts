import { flushSync } from 'svelte';
import { expect, it } from 'vitest';

import { DiagnosticsExtensions } from './diagnostics-extensions.svelte';

it('names the extensions the server lists, and keeps a refusal apart from none', async () => {
  const answering = new DiagnosticsExtensions({
    getDiagnostics: () => Promise.resolve({ extensions: ['Printer logs'] }),
  });
  const none = new DiagnosticsExtensions({
    getDiagnostics: () => Promise.resolve({ extensions: [] }),
  });
  const refusing = new DiagnosticsExtensions({
    getDiagnostics: () => Promise.reject(new Error('forbidden')),
  });

  await Promise.all([answering.load(), none.load(), refusing.load()]);
  flushSync();

  expect(answering.titles).toEqual(['Printer logs']);
  expect(none.titles).toEqual([]);
  expect(refusing.titles).toBeUndefined();
  expect(refusing.error).toBeInstanceOf(Error);
});

it('asks once while a read is in flight', async () => {
  let asked = 0;
  const store = new DiagnosticsExtensions({
    getDiagnostics: () => {
      asked += 1;

      return Promise.resolve({ extensions: [] });
    },
  });

  await Promise.all([store.load(), store.load()]);

  expect(asked).toBe(1);
});
