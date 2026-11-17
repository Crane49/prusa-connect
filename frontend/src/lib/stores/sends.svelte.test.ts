import type { DataFault, TransferMoved } from '@prusa/connect-ui/data';
import { flushSync } from 'svelte';
import { describe, expect, it } from 'vitest';

import {
  Sends,
  type SendsRunning,
  type SendsStorage,
  type SendsTransfers,
} from './sends.svelte';

interface Upload {
  storage: string;
  path: string;
  total: number;
  sent: number;
  state: 'sending' | 'done' | 'failed' | 'cancelled';
  fault: undefined | { code: string };
}

// The fakes hold their rows in `$state`, as the real stores do, so the rows
// the store derives follow what a test writes into them.
const fakes = () => {
  const uploads: Upload[] = $state([]);
  let finishUpload:
    ((ok: boolean, state?: Upload['state']) => void) | undefined;
  // Like the Storage store, a second upload of the same key aborts the first,
  // which answers undefined and records nothing.
  const pending = new Map<string, () => void>();
  // The temporary copies asked for, and how to answer each one.
  const temporaries: { storage: string; name: string }[] = [];
  const finishTemporaries: ((ok: boolean) => void)[] = [];
  let reportTemporary: ((sent: number) => void) | undefined;
  const storage: SendsStorage = {
    get uploads() {
      return uploads as never;
    },
    upload: (storage, path, body) =>
      new Promise((resolve) => {
        const key = `${storage}/${path}`;
        pending.get(key)?.();
        pending.set(key, () => {
          resolve(undefined);
        });
        const row: Upload = {
          storage,
          path,
          total: body.size,
          sent: 0,
          state: 'sending',
          fault: undefined,
        };
        uploads.push(row);
        const live = uploads[uploads.length - 1]!;
        finishUpload = (ok, state = ok ? 'done' : 'failed') => {
          pending.delete(key);
          live.state = state;
          live.fault =
            state === 'failed'
              ? { code: 'core.http/BODY_TOO_LARGE' }
              : undefined;
          resolve(ok ? { bytes: body.size, path } : undefined);
        };
      }),
    cancelUpload: () => {
      finishUpload?.(false, 'cancelled');
    },
    uploadTemporary: (storage, name, body, onProgress, signal) =>
      new Promise((resolve) => {
        temporaries.push({ storage, name });
        const copy = `.pcl-incoming/${String(temporaries.length)}-copy/${name}`;
        signal?.addEventListener('abort', () => {
          resolve({ fault: { code: 'aborted' } as never });
        });
        reportTemporary = (sent) => onProgress?.(sent, body.size);
        finishTemporaries.push((ok) => {
          resolve(
            ok
              ? { saved: { bytes: body.size, path: copy, temporary: true } }
              : { fault: { code: 'core.storage/WRITE_REFUSED' } as never },
          );
        });
      }),
  };

  // Every transfer the server is running to a printer the person may read,
  // as the store of running transfers holds them. A settled transfer leaves.
  interface Running {
    transferId: string;
    deviceId: string;
    printerName: string;
    storage: string;
    source: string;
    path: string;
    bytesTotal: number;
    bytesSent: number;
    state: string;
    mine: boolean;
    startedBy: string | undefined;
    canStop: boolean;
  }
  const rows: Running[] = $state([]);
  // The files a settled frame said landed whole, by what they moved.
  const landed: string[] = $state([]);
  const keyOf = (moved: TransferMoved): string =>
    [moved.deviceId, moved.storage, moved.source, moved.path].join('|');
  const stopped: string[] = [];
  const stopping: string[] = $state([]);
  const running: SendsRunning = {
    get rows() {
      return rows;
    },
    progress: (id) => {
      const row = rows.find((one) => one.transferId === id);
      return row === undefined || row.bytesTotal <= 0 ? null : 0.5;
    },
    landedAt: (moved) => landed.includes(keyOf(moved)),
    awaitLanding: (moved) => {
      const at = landed.indexOf(keyOf(moved));
      if (at >= 0) landed.splice(at, 1);
    },
    forget: (moved) => {
      const at = landed.indexOf(keyOf(moved));
      if (at >= 0) landed.splice(at, 1);
    },
    stop: (id) => {
      stopped.push(id);
      return Promise.resolve();
    },
    stopping: (id) => stopping.includes(id),
  };

  const finishSends: ((ok: boolean) => void)[] = [];
  // Whether the printer refuses the start of the next print it takes.
  let refuse = false;
  const sent: string[] = [];
  const printed: string[] = [];
  const sources: string[] = [];
  let error: DataFault | undefined;
  const waiting: { storage: string; source: string; path: string }[] = $state(
    [],
  );
  const relay =
    (deviceId: string, kind: 'send' | 'print') =>
    (storage: string, source: string, path: string): Promise<never> => {
      if (kind === 'print') printed.push(path);
      sent.push(path);
      sources.push(source);
      // The server opens the transfer, and its started frame reaches the
      // store of running transfers while the send waits for the answer.
      const transferId = `t${String(sent.length)}`;
      rows.unshift({
        transferId,
        deviceId,
        printerName: `Printer of ${deviceId}`,
        storage,
        source,
        path,
        bytesTotal: 7,
        bytesSent: 0,
        state: 'sending',
        mine: true,
        startedBy: undefined,
        canStop: true,
      });
      return new Promise((resolve) => {
        // The settled frame drops the row before the answer arrives.
        finishSends.push((ok) => {
          const at = rows.findIndex((one) => one.transferId === transferId);
          if (at >= 0) rows.splice(at, 1);
          // A print answers how it ended, a send the transfer it made.
          const answered =
            kind === 'print'
              ? {
                  answer: { ok: !refuse },
                  fault: refuse
                    ? fault('core.printer/COMMAND_REFUSED')
                    : undefined,
                }
              : { transfer: {} };
          resolve((ok ? answered : undefined) as never);
        });
      });
    };
  const transfersOf = (deviceId: string): SendsTransfers => ({
    get waiting() {
      return waiting;
    },
    get error() {
      return error;
    },
    send: relay(deviceId, 'send'),
    print: relay(deviceId, 'print'),
  });

  return {
    storage,
    transfersOf,
    running,
    uploads,
    rows,
    stopped,
    stopping,
    sent,
    printed,
    sources,
    waiting,
    finishUpload: (ok: boolean) => finishUpload?.(ok),
    temporaries,
    // Answers every temporary copy still going up.
    finishTemporary: (ok: boolean) => {
      for (const finish of finishTemporaries.splice(0)) finish(ok);
    },
    reportTemporary: (sent: number) => reportTemporary?.(sent),
    // A transfer of the same file to the same place that ended done earlier.
    landEarlier: (moved: TransferMoved) => {
      landed.push(keyOf(moved));
    },
    // Ends a transfer the server was still running, as its settled frame does.
    endTransfer: (transferId: string, state: string) => {
      const at = rows.findIndex((one) => one.transferId === transferId);
      const ended = rows[at];
      if (ended === undefined) return;
      if (state === 'done') landed.push(keyOf(ended));
      rows.splice(at, 1);
    },
    refuseStart: () => {
      refuse = true;
    },
    // Answers every send still waiting, with `fault` when one is refused.
    finishSend: (ok: boolean, fault?: DataFault) => {
      error = ok ? undefined : fault;
      for (const finish of finishSends.splice(0)) finish(ok);
    },
  };
};

const file = new File(['1234567'], 'cube.bgcode', { lastModified: 1 });
const fault = (code: string, params: DataFault['params'] = {}): DataFault => ({
  code,
  status: 409,
  params,
  message: undefined,
  traceId: undefined,
});

// A transfer the server is running that this tab did not start.
const elsewhere = (
  over: Partial<{
    transferId: string;
    deviceId: string;
    mine: boolean;
    startedBy: string | undefined;
    canStop: boolean;
    bytesTotal: number;
  }> = {},
) => ({
  transferId: 't-other',
  deviceId: 'p9',
  printerName: 'XL',
  storage: 'local',
  source: 'bracket.bgcode',
  path: 'usb/bracket.bgcode',
  bytesTotal: 7,
  bytesSent: 3,
  state: 'sending',
  mine: false,
  startedBy: undefined,
  canStop: false,
  ...over,
});

const tick = (): Promise<void> =>
  new Promise((resolve) => setTimeout(resolve, 0));

const sendsOf = (f: ReturnType<typeof fakes>): Sends =>
  new Sends(f.storage, f.transfersOf, f.running);

describe('Sends', () => {
  it('uploads, then sends to the printer, and leaves rows once it is done', async () => {
    const f = fakes();
    const sends = sendsOf(f);

    const done = sends.send(file, 'p1', 'Printer 1', 'local', 'usb', {
      keep: '',
    });
    expect(sends.rows[0]?.step).toBe('uploading');
    expect(sends.rows[0]?.mine).toBe(true);
    expect(sends.rows[0]?.canCancel).toBe(true);
    expect(sends.uploading).toBe(true);

    f.uploads[0]!.sent = 3;
    expect(sends.rows[0]?.progress).toBeCloseTo(3 / 7);

    f.finishUpload(true);
    await tick();
    expect(sends.rows).toHaveLength(1);
    expect(sends.rows[0]?.step).toBe('sending');
    expect(sends.rows[0]?.progress).toBe(0.5);
    expect(sends.uploading).toBe(false);
    expect(sends.active).toBe(true);
    expect(f.sent).toEqual(['usb/cube.bgcode']);

    f.finishSend(true);
    expect(await done).toBe('done');
    expect(sends.rows).toEqual([]);
    expect(sends.active).toBe(false);
  });

  it('prints a kept file once it is uploaded, and only sending never prints', async () => {
    const f = fakes();
    const sends = sendsOf(f);

    const printing = sends.send(file, 'p1', 'Printer 1', 'local', 'usb', {
      keep: '',
      print: true,
    });
    f.finishUpload(true);
    await tick();
    expect(sends.rows[0]?.step).toBe('sending');
    expect(f.printed).toEqual(['usb/cube.bgcode']);

    f.finishSend(true);
    expect(await printing).toBe('done');

    const sending = sends.send(file, 'p1', 'Printer 1', 'local', 'usb', {
      keep: '',
    });
    f.finishUpload(true);
    await tick();
    f.finishSend(true);
    expect(await sending).toBe('done');
    expect(f.printed).toEqual(['usb/cube.bgcode']);
    expect(f.sent).toEqual(['usb/cube.bgcode', 'usb/cube.bgcode']);
  });

  it('reads a file the printer took as all there while the send answers', async () => {
    const f = fakes();
    const sends = sendsOf(f);

    const done = sends.send(file, 'p1', 'Printer 1', 'local', 'usb', {
      keep: '',
    });
    f.finishUpload(true);
    await tick();
    f.endTransfer('t1', 'done');
    flushSync();

    expect(sends.rows[0]?.progress).toBe(1);
    f.finishSend(true);
    expect(await done).toBe('done');
  });

  it('does not read an earlier send of the same file as this one landing', async () => {
    const f = fakes();
    const sends = sendsOf(f);
    f.landEarlier({
      deviceId: 'p1',
      storage: 'local',
      source: 'cube.bgcode',
      path: 'usb/cube.bgcode',
    });

    void sends.send(file, 'p1', 'Printer 1', 'local', 'usb', { keep: '' });
    f.finishUpload(true);
    await tick();
    f.endTransfer('t1', 'failed');
    flushSync();

    expect(sends.rows[0]?.step).toBe('sending');
    expect(sends.rows[0]?.progress).toBeNull();
  });

  it('answers refused for a print the printer would not start', async () => {
    const f = fakes();
    const sends = sendsOf(f);
    f.refuseStart();
    const refused: unknown[] = [];

    const printing = sends.send(file, 'p1', 'Printer 1', 'local', 'usb', {
      keep: '',
      print: true,
      onRefused: (ended) => refused.push(ended.fault?.code),
    });
    f.finishUpload(true);
    await tick();
    f.finishSend(true);

    expect(await printing).toBe('refused');
    expect(refused).toEqual(['core.printer/COMMAND_REFUSED']);
  });

  it('prints a temporary copy once it is uploaded', async () => {
    const f = fakes();
    const sends = sendsOf(f);

    const printing = sends.send(file, 'p1', 'Printer 1', 'local', 'usb', {
      print: true,
    });
    f.finishTemporary(true);
    await tick();
    expect(f.printed).toEqual(['usb/cube.bgcode']);

    f.finishSend(true);
    expect(await printing).toBe('done');
  });

  it('never sends a file whose upload was refused, with the Storage row failed', async () => {
    const f = fakes();
    const sends = sendsOf(f);

    const done = sends.send(file, 'p1', 'Printer 1', 'local', 'usb', {
      keep: '',
    });
    f.finishUpload(false);

    expect(await done).toBe('failed');
    expect(sends.rows).toEqual([]);
    expect(f.sent).toEqual([]);
  });

  it('a cancelled upload is cancelled, not failed, and sends nothing', async () => {
    const f = fakes();
    const sends = sendsOf(f);

    const done = sends.send(file, 'p1', 'Printer 1', 'local', 'usb', {
      keep: '',
    });
    const id = sends.rows[0]!.id;
    sends.cancel(id);

    expect(await done).toBe('cancelled');
    expect(sends.rows).toEqual([]);
    expect(f.sent).toEqual([]);
  });

  it('joined uploads: both sends resolve done, and both rows go', async () => {
    const f = fakes();
    const sends = sendsOf(f);

    const first = sends.send(file, 'p1', 'Printer 1', 'local', 'usb', {
      keep: '',
    });
    const same = new File(['1234567'], 'cube.bgcode', { lastModified: 1 });
    const second = sends.send(same, 'p2', 'Printer 2', 'local', 'sd', {
      keep: '',
    });
    expect(f.uploads).toHaveLength(1);
    expect(sends.rows.map((row) => row.printerName)).toEqual([
      'Printer 2',
      'Printer 1',
    ]);

    f.uploads[0]!.sent = 3;
    expect(sends.rows.map((row) => row.progress)).toEqual([3 / 7, 3 / 7]);

    f.finishUpload(true);
    await tick();
    expect(f.sent).toEqual(['usb/cube.bgcode', 'sd/cube.bgcode']);
    // Each job claims the transfer to its own printer, so nothing is drawn
    // twice.
    expect(sends.rows.map((row) => row.id)).toEqual(['job:2', 'job:1']);

    f.finishSend(true);
    expect(await Promise.all([first, second])).toEqual(['done', 'done']);
    expect(sends.rows).toEqual([]);
  });

  it('a different file of the same name replaces the upload, and the first ends cancelled', async () => {
    const f = fakes();
    const sends = sendsOf(f);

    const first = sends.send(file, 'p1', 'Printer 1', 'local', 'usb', {
      keep: '',
    });
    const other = new File(['123'], 'cube.bgcode', { lastModified: 1 });
    const second = sends.send(other, 'p2', 'Printer 2', 'local', 'sd', {
      keep: '',
    });
    expect(f.uploads).toHaveLength(2);
    expect(await first).toBe('cancelled');
    expect(sends.rows).toHaveLength(1);

    f.finishUpload(true);
    await tick();
    expect(f.sent).toEqual(['sd/cube.bgcode']);

    f.finishSend(true);
    expect(await second).toBe('done');
    expect(sends.rows).toEqual([]);
  });

  it('cancelling one of two sends that share an upload cancels both', async () => {
    const f = fakes();
    const sends = sendsOf(f);

    const first = sends.send(file, 'p1', 'Printer 1', 'local', 'usb', {
      keep: '',
    });
    const second = sends.send(file, 'p2', 'Printer 2', 'local', 'sd', {
      keep: '',
    });
    sends.cancel(sends.rows[0]!.id);

    expect(await Promise.all([first, second])).toEqual([
      'cancelled',
      'cancelled',
    ]);
    expect(sends.rows).toEqual([]);
    expect(f.sent).toEqual([]);
  });

  it('uploads a file again once its earlier upload has ended', async () => {
    const f = fakes();
    const sends = sendsOf(f);

    const first = sends.send(file, 'p1', 'Printer 1', 'local', 'usb', {
      keep: '',
    });
    f.finishUpload(false);
    await first;
    void sends.send(file, 'p2', 'Printer 2', 'local', 'sd', { keep: '' });

    expect(f.uploads).toHaveLength(2);
  });

  it('a transfer stopped on the printer page ends cancelled, not failed', async () => {
    const f = fakes();
    const sends = sendsOf(f);

    const done = sends.send(file, 'p1', 'Printer 1', 'local', 'usb', {
      keep: '',
    });
    f.finishUpload(true);
    await tick();
    f.finishSend(
      false,
      fault('connector/TRANSFER_FAILED', { event: 'stopped' }),
    );

    expect(await done).toBe('cancelled');
    expect(sends.rows).toEqual([]);
  });

  it('a refused send ends failed', async () => {
    const f = fakes();
    const sends = sendsOf(f);

    const done = sends.send(file, 'p1', 'Printer 1', 'local', 'usb', {
      keep: '',
    });
    f.finishUpload(true);
    await tick();
    // Refused before any transfer opened: no transfer is running.
    f.rows.shift();
    f.finishSend(false, fault('connector/TRANSFER_BUSY'));

    expect(await done).toBe('failed');
    expect(sends.rows).toEqual([]);
  });

  it('reports no progress while the printer has not said how long the file is', async () => {
    const f = fakes();
    const sends = sendsOf(f);

    void sends.send(file, 'p1', 'Printer 1', 'local', 'usb', { keep: '' });
    f.finishUpload(true);
    await tick();
    f.rows[0]!.bytesTotal = 0;

    expect(sends.rows[0]?.step).toBe('sending');
    expect(sends.rows[0]?.progress).toBeNull();
  });

  it('reports no progress and offers no stop before the server says the transfer started', async () => {
    const f = fakes();
    const sends = sendsOf(f);

    void sends.send(file, 'p1', 'Printer 1', 'local', 'usb', { keep: '' });
    f.finishUpload(true);
    await tick();
    f.rows.shift();

    expect(sends.rows[0]?.step).toBe('sending');
    expect(sends.rows[0]?.progress).toBeNull();
    expect(sends.rows[0]?.canCancel).toBe(false);
  });

  it('a send the printer did not take ends failed', async () => {
    const f = fakes();
    const sends = sendsOf(f);

    const done = sends.send(file, 'p1', 'Printer 1', 'local', 'usb', {
      keep: '',
    });
    f.finishUpload(true);
    await tick();
    f.finishSend(false);

    expect(await done).toBe('failed');
    expect(sends.rows).toEqual([]);
  });

  it('a reset answers cancelled, and a chain from before records nothing', async () => {
    const f = fakes();
    const sends = sendsOf(f);

    const done = sends.send(file, 'p1', 'Printer 1', 'local', 'usb', {
      keep: '',
    });
    sends.reset();
    f.finishUpload(true);

    expect(await done).toBe('cancelled');
    expect(sends.rows).toEqual([]);
    expect(f.sent).toEqual([]);
  });

  it('draws every file the server is sending, whoever started it', () => {
    const f = fakes();
    f.rows.push(
      elsewhere({ transferId: 't-ada', startedBy: 'user:ada' }),
      elsewhere({ transferId: 't-mine', deviceId: 'p8', mine: true }),
    );
    const sends = sendsOf(f);

    expect(sends.rows).toEqual([
      {
        id: 'transfer:t-ada',
        deviceId: 'p9',
        printerName: 'XL',
        name: 'bracket.bgcode',
        step: 'sending',
        progress: 0.5,
        mine: false,
        startedBy: 'user:ada',
        canCancel: false,
        stopping: false,
      },
      expect.objectContaining({ id: 'transfer:t-mine', mine: true }),
    ]);
    expect(sends.active).toBe(true);
    // The browser holds none of these bytes, so closing the tab asks nothing.
    expect(sends.uploading).toBe(false);

    // The file lands, and the row goes with it.
    f.rows.splice(0, 2);
    expect(sends.rows).toEqual([]);
  });

  it('stops a file somebody else started only when the person may', () => {
    const f = fakes();
    f.rows.push(
      elsewhere({ transferId: 't-free', canStop: true }),
      elsewhere({ transferId: 't-held', deviceId: 'p7' }),
    );
    const sends = sendsOf(f);

    sends.cancel('transfer:t-held');
    sends.cancel('transfer:t-free');

    expect(f.stopped).toEqual(['t-free']);
  });

  it('stops its own file on its way to the printer through the transfer the server runs', async () => {
    const f = fakes();
    const sends = sendsOf(f);

    void sends.send(file, 'p1', 'Printer 1', 'local', 'usb', { keep: '' });
    f.finishUpload(true);
    await tick();
    expect(sends.rows[0]?.canCancel).toBe(true);

    sends.cancel(sends.rows[0]!.id);

    expect(f.stopped).toEqual(['t1']);
  });

  it('says a stop is on its way', () => {
    const f = fakes();
    f.rows.push(elsewhere({ canStop: true }));
    f.stopping.push('t-other');
    const sends = sendsOf(f);

    expect(sends.rows[0]?.stopping).toBe(true);
  });

  it('draws a file it is waiting for once, as its own row', async () => {
    const f = fakes();
    const sends = sendsOf(f);

    void sends.send(file, 'p1', 'Printer 1', 'local', 'usb', { keep: '' });
    f.finishUpload(true);
    await tick();

    // The server runs the transfer, and the job waits for it.
    expect(sends.rows.map((row) => row.id)).toEqual(['job:1']);

    // Another file on its way to the same printer, sent from somewhere
    // else, is drawn beside it.
    f.rows.push(
      elsewhere({ transferId: 'other', deviceId: 'p1', bytesTotal: 0 }),
    );
    expect(sends.rows.map((row) => [row.id, row.progress])).toEqual([
      ['job:1', 0.5],
      ['transfer:other', null],
    ]);
  });

  it('a name the printer holds ends taken', async () => {
    const f = fakes();
    const sends = sendsOf(f);

    const done = sends.send(file, 'p1', 'Printer 1', 'local', 'usb', {
      keep: '',
    });
    f.finishUpload(true);
    await tick();
    // The printer refused the name, so no transfer opened and the store
    // waits for the person.
    f.rows.shift();
    f.waiting.push({
      storage: 'local',
      source: 'cube.bgcode',
      path: 'usb/cube.bgcode',
    });
    f.finishSend(false);

    expect(await done).toBe('taken');
    expect(sends.rows).toEqual([]);
  });

  it('a reset forgets the files this tab is sending', () => {
    const f = fakes();
    const sends = sendsOf(f);
    void sends.send(file, 'p1', 'Printer 1', 'local', 'usb', { keep: '' });
    expect(sends.rows).toHaveLength(1);

    sends.reset();

    expect(sends.rows).toEqual([]);
  });

  it('recomputes the rows once for a write, and not at all for a write that changes nothing', () => {
    const f = fakes();
    const sends = sendsOf(f);
    let runs = 0;
    let seen: readonly unknown[] = [];

    const stop = $effect.root(() => {
      $effect(() => {
        seen = sends.rows;
        runs += 1;
      });
    });
    flushSync();
    expect(runs).toBe(1);

    void sends.send(file, 'p1', 'Printer 1', 'local', 'usb', { keep: '' });
    flushSync();
    expect(runs).toBe(2);

    // A cancel of a row this store does not hold writes nothing.
    sends.cancel('job:42');
    flushSync();
    expect(runs).toBe(2);
    expect(seen).toHaveLength(1);

    stop();
  });
});

describe('Sends, kept or not', () => {
  it('sends a temporary copy by default: its own progress, no Storage row, and the source the server answered', async () => {
    const f = fakes();
    const sends = new Sends(f.storage, f.transfersOf, f.running);

    const done = sends.send(file, 'p1', 'Printer 1', 'local', 'usb');
    expect(f.temporaries).toEqual([{ storage: 'local', name: 'cube.bgcode' }]);
    expect(f.uploads).toEqual([]);
    expect(sends.rows[0]?.step).toBe('uploading');
    expect(sends.rows[0]?.progress).toBe(0);

    f.reportTemporary(3);
    expect(sends.rows[0]?.progress).toBeCloseTo(3 / 7);

    f.finishTemporary(true);
    await tick();
    expect(sends.rows[0]?.step).toBe('sending');
    expect(f.sources).toEqual(['.pcl-incoming/1-copy/cube.bgcode']);
    expect(f.sent).toEqual(['usb/cube.bgcode']);

    f.finishSend(true);
    expect(await done).toBe('done');
    expect(sends.rows).toEqual([]);
  });

  it('never shares a temporary copy: the same file for two printers goes up twice', async () => {
    const f = fakes();
    const sends = new Sends(f.storage, f.transfersOf, f.running);

    const first = sends.send(file, 'p1', 'Printer 1', 'local', 'usb');
    const second = sends.send(file, 'p2', 'Printer 2', 'local', 'sd');
    expect(f.temporaries).toHaveLength(2);

    f.finishTemporary(true);
    await tick();
    expect(f.sources).toEqual([
      '.pcl-incoming/1-copy/cube.bgcode',
      '.pcl-incoming/2-copy/cube.bgcode',
    ]);

    f.finishSend(true);
    expect(await first).toBe('done');
    expect(await second).toBe('done');
  });

  it('stops a temporary copy on its way: cancelled, and nothing sent', async () => {
    const f = fakes();
    const sends = new Sends(f.storage, f.transfersOf, f.running);

    const done = sends.send(file, 'p1', 'Printer 1', 'local', 'usb');
    sends.cancel(sends.rows[0]!.id);

    expect(await done).toBe('cancelled');
    expect(sends.rows).toEqual([]);
    expect(f.sent).toEqual([]);
  });

  it('a refused temporary copy fails and sends nothing', async () => {
    const f = fakes();
    const sends = new Sends(f.storage, f.transfersOf, f.running);

    const done = sends.send(file, 'p1', 'Printer 1', 'local', 'usb');
    f.finishTemporary(false);

    expect(await done).toBe('failed');
    expect(f.sent).toEqual([]);
  });

  it('a temporary copy refused for a name the printer holds is taken, matched by its own source', async () => {
    const f = fakes();
    const sends = new Sends(f.storage, f.transfersOf, f.running);

    const done = sends.send(file, 'p1', 'Printer 1', 'local', 'usb');
    f.finishTemporary(true);
    await tick();
    f.waiting.push({
      storage: 'local',
      source: '.pcl-incoming/1-copy/cube.bgcode',
      path: 'usb/cube.bgcode',
    });
    f.finishSend(false, fault('core.printer/FILE_NAME_TAKEN'));

    expect(await done).toBe('taken');
  });

  it('a reset stops a temporary copy on its way', async () => {
    const f = fakes();
    const sends = new Sends(f.storage, f.transfersOf, f.running);

    const done = sends.send(file, 'p1', 'Printer 1', 'local', 'usb');
    sends.reset();

    expect(await done).toBe('cancelled');
    expect(f.sent).toEqual([]);
  });

  it('keeps a file in the folder chosen and sends it from there', async () => {
    const f = fakes();
    const sends = new Sends(f.storage, f.transfersOf, f.running);

    const done = sends.send(file, 'p1', 'Printer 1', 'local', 'usb', {
      keep: 'prints/brackets',
    });
    expect(f.temporaries).toEqual([]);
    expect(f.uploads[0]?.path).toBe('prints/brackets/cube.bgcode');

    f.finishUpload(true);
    await tick();
    expect(f.sources).toEqual(['prints/brackets/cube.bgcode']);

    f.finishSend(true);
    expect(await done).toBe('done');
  });
});
