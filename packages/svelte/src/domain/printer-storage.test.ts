import { describe, expect, it } from 'vitest';

import {
  type DeviceStorage,
  type DeviceStorages,
  uploadBlockOf,
} from './printer-storage';

const usb = (readOnly = false): DeviceStorage => ({
  free_space: 1024,
  mountpoint: '/usb',
  name: 'usb',
  read_only: readOnly,
  type: 'USB',
});

/** An error the SDK throws for an HTTP answer: it carries the status. */
const answered = (status: number): Error =>
  Object.assign(new Error(`HTTP ${String(status)}`), { status });

describe('uploadBlockOf', () => {
  it.each([
    [
      'a writable drive',
      { storages: { items: [usb()], reported: true } },
      'uploads',
      undefined,
    ],
    [
      'no drive after an answer',
      { storages: { items: [], reported: true } },
      'uploads',
      'noStorage',
    ],
    [
      'a null list after an answer',
      { storages: { items: null, reported: true } },
      'uploads',
      'noStorage',
    ],
    [
      'only a locked drive',
      { storages: { items: [usb(true)], reported: true } },
      'uploads',
      'noStorage',
    ],
    [
      'no answer yet',
      { storages: { items: [], reported: false } },
      'uploads',
      'storagesUnknown',
    ],
    [
      'a list from an earlier connection',
      { storages: { items: [usb()], reported: false } },
      'uploads',
      'storagesUnknown',
    ],
    ['the first read still on its way', {}, 'uploads', undefined],
    [
      'a read the printer did not answer in time',
      { error: answered(504) },
      'uploads',
      'storagesUnknown',
    ],
    [
      'a read refused for a full command queue',
      { error: answered(409) },
      'uploads',
      'storagesUnknown',
    ],
    [
      'a read that never reached the server',
      { error: new TypeError('Failed to fetch') },
      'uploads',
      'storagesUnknown',
    ],
    [
      'a person who may not operate the printer',
      { error: answered(403) },
      'uploads',
      undefined,
    ],
    [
      'a printer that is not there',
      { error: answered(404) },
      'uploads',
      undefined,
    ],
    [
      'a failed ask again over an answer with a drive',
      { storages: { items: [usb()], reported: true }, error: answered(504) },
      'uploads',
      undefined,
    ],
    [
      'no server storage',
      { storages: { items: [usb()], reported: true } },
      undefined,
      'noServerStorage',
    ],
  ] satisfies [
    string,
    { storages?: DeviceStorages; error?: Error },
    string | undefined,
    string | undefined,
  ][])('%s', (_, printer, server, block) => {
    expect(uploadBlockOf(printer, server)).toBe(block);
  });
});
