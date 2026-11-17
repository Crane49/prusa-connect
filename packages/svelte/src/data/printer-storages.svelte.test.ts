import type { StreamEvent } from '@prusa/connect';
import { describe, expect, it } from 'vitest';

import type { DeviceStorage, DeviceStorages } from '../domain/printer-storage';
import {
  PrinterStorages,
  type PrinterStoragesClient,
} from './printer-storages.svelte';

const usb: DeviceStorage = {
  free_space: 1024,
  mountpoint: '/usb',
  name: 'usb',
  read_only: false,
  type: 'USB',
};

// A client that answers each printer from a table and keeps every request.
const tableClient = (
  answers: Record<string, DeviceStorages>,
): { client: PrinterStoragesClient; asked: unknown[] } => {
  const asked: unknown[] = [];
  const client: PrinterStoragesClient = {
    listDeviceStorages: (request) => {
      asked.push(request);
      const answer = answers[request.path.device_id];

      return answer === undefined
        ? Promise.reject(new Error('no such printer'))
        : Promise.resolve(answer);
    },
  };

  return { client, asked };
};

describe('PrinterStorages', () => {
  // The picker opens on a fleet that is mostly ready. A printer known to have
  // a drive is not asked again, so opening it puts no frame on that wire.
  it('asks again only the printers not known to take a file', async () => {
    const { client, asked } = tableClient({
      bare: { items: [], reported: true },
      ready: { items: [usb], reported: true },
    });
    const store = new PrinterStorages(client);

    await store.check(['bare', 'ready', 'gone']);
    expect(asked).toHaveLength(3);
    expect(asked[0]).toEqual({
      path: { device_id: 'bare' },
      query: { refresh: true },
    });
    expect(store.noStorage('bare')).toBe(true);
    expect(store.noStorage('ready')).toBe(false);
    // A failed read is not a printer without a drive.
    expect(store.noStorage('gone')).toBe(false);

    await store.check(['bare', 'ready']);
    expect(asked).toHaveLength(4);
  });

  // A printer that has not answered is never refused on a guess.
  it('refuses nothing before the printer has answered', async () => {
    const { client } = tableClient({ fresh: { items: [], reported: false } });
    const store = new PrinterStorages(client);

    await store.check(['fresh']);
    expect(store.noStorage('fresh')).toBe(false);
  });

  it('takes a drive put in from the storages event', () => {
    const { client } = tableClient({});
    const store = new PrinterStorages(client);
    let listener: ((event: StreamEvent) => void) | undefined;
    const stop = store.watch({
      subscribe: (next) => {
        listener = next;

        return () => {
          listener = undefined;
        };
      },
    });
    const event = (items: DeviceStorage[]): StreamEvent =>
      // The cast stands in for the narrowed union the SDK yields for one type.
      ({
        type: 'com.prusa.connect.device.storages.changed',
        id: '1',
        payload: { device_id: 'mk4', items, reported: true },
        envelope: { subject: 'device:mk4' },
      }) as StreamEvent;

    listener?.(event([]));
    expect(store.noStorage('mk4')).toBe(true);

    listener?.(event([usb]));
    expect(store.noStorage('mk4')).toBe(false);

    stop();
  });
});
