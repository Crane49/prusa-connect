import { flushSync } from 'svelte';
import { describe, expect, it } from 'vitest';

import type { LogsApi } from './api';
import { DownloadStore } from './download.svelte';
import { exportStream } from './export';
import { MetricsStore } from './metrics.svelte';
import { printerHost } from './presentation';
import { portsOf, SetupStore } from './setup.svelte';
import { STATUS } from './test/host';

type Status = Awaited<ReturnType<LogsApi['readStatus']>>;

describe('the setup of a printer', () => {
  it('sends the ports the module was granted, and each stream on or off alone', async () => {
    const sent: unknown[] = [];
    const setup = new SetupStore(
      {
        setUpPrinter: (request) => {
          sent.push(request);

          return Promise.resolve({
            device_id: 'mk4',
            gcode: 'M334 192.168.1.5 0 13514',
            confirm: 'yes',
          });
        },
      },
      { getUiSettings: () => Promise.reject(new Error('unused')) },
      'localhost',
    );

    await setup.send(
      { deviceId: 'mk4', host: ' 192.168.1.5 ', syslog: true, metrics: false },
      portsOf(STATUS as Status),
    );
    flushSync();

    expect(sent).toEqual([
      {
        path: { device_id: 'mk4' },
        body: {
          host: '192.168.1.5',
          syslog: true,
          metrics: false,
          syslog_port: 13514,
          metrics_port: 8514,
        },
      },
    ]);
    expect(setup.sent?.gcode).toBe('M334 192.168.1.5 0 13514');
  });

  it('writes the host printers dial, else the host of this page', async () => {
    expect(printerHost('https://192.168.1.5:8443', 'pcl.local')).toBe(
      '192.168.1.5',
    );
    expect(printerHost(undefined, 'pcl.local')).toBe('pcl.local');
    expect(printerHost('not an address', 'pcl.local')).toBe('pcl.local');

    const setup = new SetupStore(
      { setUpPrinter: () => Promise.reject(new Error('unused')) },
      { getUiSettings: () => Promise.reject(new Error('down')) },
      'pcl.local',
    );
    await setup.loadHost();
    expect(setup.host).toBe('pcl.local');
  });

  it('falls back to the firmware ports where the module was granted none', () => {
    expect(portsOf(undefined)).toEqual({ syslog: 13514, metrics: 8514 });
  });
});

describe('the metrics', () => {
  it('chooses the first metric, then draws its first field', async () => {
    const asked: unknown[] = [];
    const metrics = new MetricsStore({
      listMetricNames: () =>
        Promise.resolve({
          names: [
            {
              name: 'temp_bed',
              fields: ['v'],
              last: { v: 60.1 },
              last_at: '2026-10-19T14:00:00Z',
              count: 3,
            },
          ],
          truncated: false,
        }),
      readMetricSeries: (request) => {
        asked.push(request.query);

        return Promise.resolve({
          name: 'temp_bed',
          field: 'v',
          points: [{ at: '2026-10-19T14:00:00Z', value: 60, min: 59, max: 61 }],
        });
      },
    });

    await metrics.load({ origin: { kind: 'device', id: 'mk4' } });
    flushSync();

    expect(metrics.chosen).toBe('temp_bed');
    expect(metrics.points).toHaveLength(1);
    expect(asked).toEqual([
      expect.objectContaining({ device: 'mk4', name: 'temp_bed', field: 'v' }),
    ]);
  });
});

describe('the metric list', () => {
  it('says when the module listed only some of the names', async () => {
    const metrics = new MetricsStore({
      listMetricNames: () => Promise.resolve({ names: [], truncated: true }),
      readMetricSeries: () =>
        Promise.resolve({ name: '', field: 'v', points: [] }),
    });

    await metrics.load({});
    flushSync();

    expect(metrics.truncated).toBe(true);
  });
});

describe('a download', () => {
  it('asks for every page in turn and joins them into one file', async () => {
    const cursors: (string | undefined)[] = [];
    const api = {
      exportRecords: (request: { query: { cursor?: string } }) => {
        cursors.push(request.query.cursor);
        const at =
          request.query.cursor === undefined ? 0 : Number(request.query.cursor);

        return Promise.resolve(
          at < 2
            ? { text: `page ${String(at)}\n`, next: String(at + 1) }
            : { text: 'last\n' },
        );
      },
    } as unknown as Pick<LogsApi, 'exportRecords'>;

    const file = await exportStream(api, { stream: 'syslog' });

    expect(cursors).toEqual([undefined, '1', '2']);
    expect(await file.text()).toBe('page 0\npage 1\nlast\n');
  });

  it('starts again once from the first page when a purge moved the records', async () => {
    const cursors: (string | undefined)[] = [];
    let refused = false;
    const api = {
      exportRecords: (request: { query: { cursor?: string } }) => {
        cursors.push(request.query.cursor);
        if (request.query.cursor === '1' && !refused) {
          refused = true;

          return Promise.reject(
            Object.assign(new Error('moved'), {
              code: 'buddy-debug-logs/CURSOR_STALE',
            }),
          );
        }

        return Promise.resolve(
          request.query.cursor === undefined
            ? { text: 'first\n', next: '1' }
            : { text: 'last\n' },
        );
      },
    } as unknown as Pick<LogsApi, 'exportRecords'>;

    const file = await exportStream(api, { stream: 'syslog' });

    expect(cursors).toEqual([undefined, '1', undefined, '1']);
    expect(await file.text()).toBe('first\nlast\n');
  });

  it('saves the file under a name that says the stream, and keeps a refusal', async () => {
    const saved: string[] = [];
    const ok = new DownloadStore(
      { exportRecords: () => Promise.resolve({ text: 'x' }) },
      (_blob, name) => saved.push(name),
    );
    await ok.run('metrics', '2026-10-19T00:00:00Z');
    expect(saved[0]).toMatch(/^printer-metrics-.*\.lp$/u);

    const refused = new DownloadStore(
      { exportRecords: () => Promise.reject(new Error('down')) },
      () => undefined,
    );
    await refused.run('syslog', '2026-10-19T00:00:00Z');
    expect(refused.fault).toBeInstanceOf(Error);
  });
});
