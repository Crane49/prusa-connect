import { describe, expect, expectTypeOf, it } from 'vitest';

import type { PrintOutcome, PrintRequest } from '../../domain/actions';
import {
  landingPathOf,
  NO_PRINTER_STORAGE,
  NoPrinterStorage,
  PRINTER_STORAGES_UNKNOWN,
  printOnEach,
  printStoredFile,
  startPrint,
  type DeviceStorage,
  type NoPrinterStorageCode,
  type PrintClient,
} from './print';

const usb: DeviceStorage = {
  free_space: 1_000_000,
  mountpoint: '/usb',
  name: 'usb',
  read_only: false,
  type: 'USB',
};

const rom: DeviceStorage = {
  free_space: null,
  mountpoint: '/rom',
  name: 'rom',
  read_only: true,
  type: 'FLASH',
};

describe('landingPathOf', () => {
  // The printer addresses every file under one of its own storages, so a bare
  // name would name the tree above them and reach no storage at all.
  it('puts the file under the first storage that takes a write', () => {
    expect(landingPathOf([usb], 'bracket.bgcode')).toBe('usb/bracket.bgcode');
    expect(landingPathOf([rom, usb], 'bracket.bgcode')).toBe(
      'usb/bracket.bgcode',
    );
  });

  it('answers nothing for a printer with nowhere to write', () => {
    expect(landingPathOf([rom], 'bracket.bgcode')).toBeUndefined();
    expect(landingPathOf([], 'bracket.bgcode')).toBeUndefined();
  });
});

const fakeClient = (
  storages: readonly DeviceStorage[],
  reported = true,
): { client: PrintClient; sent: unknown[]; asked: unknown[] } => {
  const sent: unknown[] = [];
  const asked: unknown[] = [];
  const client: PrintClient = {
    listDeviceStorages: (request) => {
      asked.push(request);

      return Promise.resolve({ items: [...storages], reported });
    },
    printDeviceFile: (request) => {
      sent.push(request);

      return Promise.resolve({
        command: { command_id: 'cmd-1', ok: true },
        path: 'usb/bracket.bgcode',
      });
    },
  };

  return { client, sent, asked };
};

describe('printStoredFile', () => {
  it('names where the file lands, storage-prefixed as the route asks', async () => {
    const { client, sent } = fakeClient([rom, usb]);
    const outcome = await printStoredFile(client, {
      deviceId: 'printer-1',
      name: 'bracket.bgcode',
      source: 'models/bracket.bgcode',
      storage: 'local',
    });

    expect(sent).toEqual([
      {
        body: {
          path: 'usb/bracket.bgcode',
          source: 'models/bracket.bgcode',
          storage: 'local',
        },
        path: { device_id: 'printer-1' },
      },
    ]);
    expect(outcome.ok).toBe(true);
  });

  // A drive put in a moment ago is only known once the printer is asked again.
  it('asks the printer again rather than reading what it said before', async () => {
    const { client, asked } = fakeClient([usb]);
    await printStoredFile(client, {
      deviceId: 'printer-1',
      name: 'bracket.bgcode',
      source: 'models/bracket.bgcode',
      storage: 'local',
    });

    expect(asked).toEqual([
      { path: { device_id: 'printer-1' }, query: { refresh: true } },
    ]);
  });

  // Nothing is sent, because there is nowhere on the machine to put the file.
  // A printer that answered with no storage it can write to needs a drive put
  // in it, so that is what it is told.
  it.each([
    ['locks every storage it listed', [rom]],
    ['answered with no storage at all', []],
  ])('refuses a printer that %s', async (_, storages) => {
    const { client, sent } = fakeClient(storages);

    await expect(
      printStoredFile(client, {
        deviceId: 'printer-1',
        name: 'bracket.bgcode',
        source: 'models/bracket.bgcode',
        storage: 'local',
      }),
    ).rejects.toMatchObject({ code: NO_PRINTER_STORAGE });
    expect(sent).toEqual([]);
  });

  // A printer that has not answered yet, which one that has only just
  // connected has not. Telling that reader to find a USB drive would send them
  // after a drive that is already in the machine.
  it('tells a printer that has not reported its storages apart', async () => {
    const { client, sent } = fakeClient([usb], false);

    await expect(
      printStoredFile(client, {
        deviceId: 'printer-1',
        name: 'bracket.bgcode',
        source: 'models/bracket.bgcode',
        storage: 'local',
      }),
    ).rejects.toMatchObject({ code: PRINTER_STORAGES_UNKNOWN });
    expect(sent).toEqual([]);
  });

  // The code is one of the two this module publishes and never any other
  // string, so a screen that branches on it is told at compile time when it
  // has missed one.
  it('narrows the code to the two it can carry', () => {
    const thrown = new NoPrinterStorage('MK4S', true);

    expectTypeOf(thrown.code).toEqualTypeOf<NoPrinterStorageCode>();
    expect(thrown.code).toBe(NO_PRINTER_STORAGE);
    expect(new NoPrinterStorage('MK4S', false).code).toBe(
      PRINTER_STORAGES_UNKNOWN,
    );
  });
});

describe('the print request the domain declares', () => {
  it('keeps the shape the route takes', () => {
    const request: PrintRequest = {
      deviceId: 'printer-1',
      path: 'usb/bracket.bgcode',
      source: 'models/bracket.bgcode',
      storage: 'local',
    };

    expect(request.path.includes('/')).toBe(true);
  });
});

describe('startPrint', () => {
  // A file the printer holds needs no storage listing, and asking for one
  // would make a print wait on a printer that has not reported its storages.
  it('starts a file the printer holds by its path alone', async () => {
    const asked: string[] = [];
    const { client, sent } = fakeClient([usb]);
    const counting: PrintClient = {
      ...client,
      listDeviceStorages: (request) => {
        asked.push('storages');

        return client.listDeviceStorages(request);
      },
    };

    await startPrint(counting, { deviceId: 'mk4', path: 'usb/bracket.bgcode' });

    expect(asked).toEqual([]);
    expect(sent).toEqual([
      { path: { device_id: 'mk4' }, body: { path: 'usb/bracket.bgcode' } },
    ]);
  });

  it('lands a file from this server under the printer storage', async () => {
    const { client, sent } = fakeClient([rom, usb]);

    await startPrint(client, {
      deviceId: 'mk4',
      path: 'bracket.bgcode',
      source: 'models/bracket.bgcode',
      storage: 'local',
    });

    expect(sent).toEqual([
      {
        path: { device_id: 'mk4' },
        body: {
          path: 'usb/bracket.bgcode',
          source: 'models/bracket.bgcode',
          storage: 'local',
        },
      },
    ]);
  });
});

describe('printOnEach', () => {
  const outcome = (ok: boolean): PrintOutcome => ({
    commandId: ok ? 'cmd' : undefined,
    faultCode: ok ? undefined : 'core.printer/BUSY',
    machineReason: ok ? undefined : 'printing already',
    ok,
    path: 'usb/bracket.bgcode',
    transferId: undefined,
  });

  const requestsFor = (ids: readonly string[]): PrintRequest[] =>
    ids.map((deviceId) => ({ deviceId, path: 'bracket.bgcode' }));

  it('talks to no more printers at once than the limit', async () => {
    let open = 0;
    let most = 0;
    const print = async (): Promise<PrintOutcome> => {
      open += 1;
      most = Math.max(most, open);
      await new Promise((resolve) => setTimeout(resolve, 5));
      open -= 1;

      return outcome(true);
    };

    const attempts = await printOnEach(
      print,
      requestsFor(['a', 'b', 'c', 'd', 'e', 'f', 'g']),
      3,
    );

    expect(most).toBe(3);
    expect(attempts).toHaveLength(7);
  });

  it('answers each printer in order, a refusal and a throw beside a start', async () => {
    const thrown = new Error('gone');
    const print = (request: PrintRequest): Promise<PrintOutcome> => {
      if (request.deviceId === 'thrown') return Promise.reject(thrown);

      return Promise.resolve(outcome(request.deviceId === 'started'));
    };

    const attempts = await printOnEach(
      print,
      requestsFor(['refused', 'started', 'thrown']),
    );

    expect(attempts).toEqual([
      {
        deviceId: 'refused',
        failure: {
          code: 'core.printer/BUSY',
          status: undefined,
          params: {},
          message: 'printing already',
          traceId: undefined,
        },
      },
      { deviceId: 'started' },
      { deviceId: 'thrown', failure: thrown },
    ]);
  });
});
