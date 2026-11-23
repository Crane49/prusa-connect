import type { CompatiblePrinterParams } from '@prusa/connect-ui/slicer';
import { flushSync } from 'svelte';
import { afterEach, describe, expect, it, vi } from 'vitest';

import type { PrusaSlicerApi } from './api';
import { PrinterPicker, REFRESH_MS } from './select-printer.svelte';
import { farm, fakeSlicer } from './test/fakes';

const listing = (
  answer: () => Promise<{ printers: typeof farm }>,
): Pick<PrusaSlicerApi, 'listSlicerPrinters'> & { calls: number } => {
  const api = {
    calls: 0,
    listSlicerPrinters: () => {
      api.calls += 1;

      return answer();
    },
  };

  return api;
};

const pickers: PrinterPicker[] = [];
afterEach(() => {
  for (const picker of pickers.splice(0)) picker.dispose();
  vi.useRealTimers();
});

const started = (
  api: Pick<PrusaSlicerApi, 'listSlicerPrinters'>,
  inSlicer = true,
) => {
  const fake = fakeSlicer(inSlicer);
  const picker = new PrinterPicker(api, fake.slicer);
  pickers.push(picker);
  picker.start();

  return { picker, fake };
};

const ask = (
  fake: ReturnType<typeof fakeSlicer>,
  params: CompatiblePrinterParams,
): void => {
  fake.slicer.emit('requestcompatibleprinter', params);
  flushSync();
};

describe('the print dialog', () => {
  it('tells the slicer it is ready and reads the printers', async () => {
    const api = listing(() => Promise.resolve({ printers: farm }));
    const { picker, fake } = started(api);

    expect(fake.sent).toEqual([
      { action: 'WEBAPP_READY', componentName: 'PrinterList' },
    ]);
    expect(picker.load).toBe('loading');
    await vi.waitFor(() => {
      expect(picker.load).toBe('ready');
    });
    expect(api.calls).toBe(1);
    expect(picker.request).toBeUndefined();
    expect(picker.candidates).toEqual([]);
  });

  it('offers the printers once the slicer says what the file needs', async () => {
    const { picker, fake } = started(
      listing(() => Promise.resolve({ printers: farm })),
    );
    await vi.waitFor(() => {
      expect(picker.load).toBe('ready');
    });

    ask(fake, {
      printerUuid: 'mk4s-1',
      printerModel: 'MK4S',
      filename: 'bracket.bgcode',
      nozzle_diameter: [0.4],
      material: ['PLA'],
    });

    expect(picker.candidates.map((item) => item.printer.uuid)).toEqual([
      'mk4s-1',
      'mk4s-3',
      'mk4s-2',
      'mk4s-4',
    ]);
    expect(picker.selected).toBe('mk4s-1');
    expect(picker.filename).toBe('bracket.bgcode');
    expect(picker.ready).toBe(true);
  });

  it('refuses a printer that cannot print now', async () => {
    const { picker, fake } = started(
      listing(() => Promise.resolve({ printers: farm })),
    );
    await vi.waitFor(() => {
      expect(picker.load).toBe('ready');
    });
    ask(fake, { printerModel: 'MK4S', filename: 'a.bgcode' });

    picker.select('mk4s-2');
    expect(picker.selected).toBeUndefined();

    picker.select('mk4s-3');
    expect(picker.selected).toBe('mk4s-3');
  });

  it('sends PRINT and closes the dialog once', async () => {
    const { picker, fake } = started(
      listing(() => Promise.resolve({ printers: farm })),
    );
    await vi.waitFor(() => {
      expect(picker.load).toBe('ready');
    });
    ask(fake, {
      printerUuid: 'mk4s-1',
      printerModel: 'MK4S',
      filename: 'a.bgcode',
    });
    picker.filename = '  b.bgcode ';
    fake.sent.length = 0;

    picker.print();
    picker.print();

    const data = {
      set_ready: true,
      position: 0,
      wait_until: undefined,
      filename: 'b.bgcode',
      printer_uuid: 'mk4s-1',
      team_id: 1,
    };
    expect(fake.sent).toEqual([
      { action: 'PRINT', ...data, data },
      { action: 'CLOSE_DIALOG' },
    ]);
    expect(picker.sent).toBe(true);
  });

  it('does not print without a file name', async () => {
    const { picker, fake } = started(
      listing(() => Promise.resolve({ printers: farm })),
    );
    await vi.waitFor(() => {
      expect(picker.load).toBe('ready');
    });
    ask(fake, {
      printerUuid: 'mk4s-1',
      printerModel: 'MK4S',
      filename: 'a.bgcode',
    });
    picker.filename = ' ';
    fake.sent.length = 0;

    picker.print();

    expect(picker.ready).toBe(false);
    expect(fake.sent).toEqual([]);
  });

  it('prints a project of several beds without a file name', async () => {
    const { picker, fake } = started(
      listing(() => Promise.resolve({ printers: farm })),
    );
    await vi.waitFor(() => {
      expect(picker.load).toBe('ready');
    });
    ask(fake, {
      printerUuid: 'mk4s-1',
      printerModel: 'MK4S',
      multiple_beds: true,
    });

    expect(picker.filename).toBe('');
    expect(picker.ready).toBe(true);
  });

  it('closes the dialog on cancel', () => {
    const { picker, fake } = started(
      listing(() => Promise.resolve({ printers: farm })),
    );
    fake.sent.length = 0;

    picker.cancel();

    expect(fake.sent).toEqual([{ action: 'CLOSE_DIALOG' }]);
  });

  it('tells a refusal from a failure, and keeps a list already drawn', async () => {
    const refused = started(
      listing(() =>
        Promise.reject(Object.assign(new Error('no'), { status: 403 })),
      ),
    ).picker;
    await vi.waitFor(() => {
      expect(refused.load).toBe('refused');
    });

    let fail = false;
    const flaky = started(
      listing(() =>
        fail
          ? Promise.reject(new Error('down'))
          : Promise.resolve({ printers: farm }),
      ),
    ).picker;
    await vi.waitFor(() => {
      expect(flaky.load).toBe('ready');
    });
    fail = true;
    await flaky.refresh();

    expect(flaky.load).toBe('ready');
    expect(flaky.fault).toBeDefined();
  });

  it('reads the printers again after each read ends, and stops on dispose', async () => {
    vi.useFakeTimers();
    const api = listing(() => Promise.resolve({ printers: farm }));
    const { picker, fake } = started(api);

    await vi.advanceTimersByTimeAsync(REFRESH_MS * 2);
    expect(api.calls).toBe(3);

    picker.dispose();
    await vi.advanceTimersByTimeAsync(REFRESH_MS * 2);
    expect(api.calls).toBe(3);

    // The slicer's request after dispose is not heard.
    ask(fake, { printerModel: 'MK4S' });
    expect(picker.request).toBeUndefined();
  });

  it('waits for a slow read before it starts the next', async () => {
    vi.useFakeTimers();
    let release: (() => void) | undefined;
    const api = listing(
      () =>
        new Promise((resolve) => {
          release = () => {
            resolve({ printers: farm });
          };
        }),
    );
    started(api);

    // The first read hangs for three refreshes: nothing else is asked.
    await vi.advanceTimersByTimeAsync(REFRESH_MS * 3);
    expect(api.calls).toBe(1);

    release?.();
    await vi.advanceTimersByTimeAsync(REFRESH_MS - 1);
    expect(api.calls).toBe(1);
    await vi.advanceTimersByTimeAsync(1);
    expect(api.calls).toBe(2);
  });

  it('says it is outside the slicer, and posts nothing', () => {
    const { picker, fake } = started(
      listing(() => Promise.resolve({ printers: farm })),
      false,
    );

    expect(picker.inSlicer).toBe(false);
    expect(fake.sent).toEqual([]);
  });
});
