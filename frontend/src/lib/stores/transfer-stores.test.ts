// The per-printer transfer stores, and the person each one reads for.
import { describe, expect, it } from 'vitest';

import type { TransfersClient } from '@prusa/connect-ui/data';

import { transferStores } from './transfer-stores';

// A client that records what each stream was opened with and opens nothing.
const recording = () => {
  const asked: unknown[] = [];
  const subscription = {
    lastEventId: undefined,
    close: () => undefined,
    [Symbol.asyncIterator]: async function* () {
      // The stream sends nothing.
    },
  };
  // The cast names the generic signature a test cannot write out.
  const events = ((options: unknown) => {
    asked.push(options);

    return subscription;
  }) as unknown as TransfersClient['events'];
  const client: TransfersClient = {
    listDeviceTransfers: () => Promise.resolve({ items: [] }),
    stopDeviceTransfer: () => Promise.resolve(),
    sendDeviceFile: () => Promise.reject(new Error('not sent in this test')),
    printDeviceFile: () =>
      Promise.reject(new Error('not printed in this test')),
    events,
  };

  return { client, asked };
};

describe('the transfer stores', () => {
  it('build a store after a reset for the person signed in then', () => {
    const { client, asked } = recording();
    let person = 'user:ada';
    const stores = transferStores(client, () => person);

    stores('p1').listen()();
    stores.reset();
    person = 'user:grace';
    stores('p1').listen()();

    expect(asked).toEqual([
      expect.objectContaining({ principal: 'user:ada' }),
      expect.objectContaining({ principal: 'user:grace' }),
    ]);
  });

  it('answer a new store for a printer after a reset', () => {
    const { client } = recording();
    const stores = transferStores(client);
    const before = stores('p1');

    stores.reset();

    expect(stores('p1')).not.toBe(before);
  });
});
