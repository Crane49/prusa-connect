import { describe, expect, it } from 'vitest';

import {
  type DeviceInfo,
  narrow,
  previousRange,
  type SettledJob,
  summarise,
  WHOLE_FLEET,
} from './job-stats';

const HOUR = 3_600_000;
const at = (iso: string) => new Date(iso).getTime(); // local wall time when no offset

const job = (
  over: Partial<SettledJob> & Pick<SettledJob, 'started' | 'ended'>,
): SettledJob => ({
  deviceId: 'a',
  jobId: 'a:1',
  name: 'benchy.bgcode',
  outcome: 'finished',
  machineState: 'FINISHED',
  ...over,
});
const devices: DeviceInfo[] = [
  { id: 'a', name: 'MK4-01', model: 'MK4S', tags: ['garage'] },
  { id: 'b', name: 'XL-01', model: 'XL', tags: [] },
];
// Monday 2026-09-21 00:00 local to Monday 2026-09-28 00:00 local.
const week = { from: at('2026-09-21T00:00'), to: at('2026-09-28T00:00') };

describe('the test runner', () => {
  it('runs in the pinned zone', () => {
    expect(Intl.DateTimeFormat().resolvedOptions().timeZone).toBe(
      'Europe/Prague',
    );
  });
});

describe('summarise', () => {
  it('reads an empty fleet and an empty week without NaN', () => {
    const stats = summarise([], [], week);
    expect(stats.kpis).toEqual({
      jobs: 0,
      successRate: undefined,
      printHours: 0,
      utilization: 0,
      averageJobMs: undefined,
    });
    expect(stats.utilization).toEqual([]);
    expect(stats.byDevice).toEqual([]);
  });

  it('counts success over decided jobs only', () => {
    const jobs = [
      job({ started: at('2026-09-22T10:00'), ended: at('2026-09-22T11:00') }),
      job({
        started: at('2026-09-22T12:00'),
        ended: at('2026-09-22T13:00'),
        outcome: 'failed',
        machineState: 'ERROR',
      }),
      job({
        started: at('2026-09-22T14:00'),
        ended: at('2026-09-22T15:00'),
        outcome: 'unknown',
        machineState: '',
      }),
    ];
    const stats = summarise(jobs, devices, week);
    expect(stats.kpis.jobs).toBe(3);
    expect(stats.kpis.successRate).toBe(0.5);
    expect(stats.kpis.printHours).toBe(3);
    expect(stats.kpis.averageJobMs).toBe(HOUR);
  });

  it('fills the hour a job covers and splits a job across hours', () => {
    const jobs = [
      job({ started: at('2026-09-22T10:30'), ended: at('2026-09-22T12:00') }),
    ]; // Tuesday
    const row = summarise(jobs, devices, week).utilization.find(
      (r) => r.deviceId === 'a',
    )!;
    expect(row.cells[1 * 24 + 10]).toBeCloseTo(0.5);
    expect(row.cells[1 * 24 + 11]).toBeCloseTo(1);
    expect(row.cells[1 * 24 + 12]).toBe(0);
  });

  it('clips a job that began before the range and one that runs past it', () => {
    const jobs = [
      job({ started: at('2026-09-20T23:00'), ended: at('2026-09-21T01:00') }),
      job({
        jobId: 'a:2',
        started: at('2026-09-27T23:00'),
        ended: at('2026-09-28T02:00'),
      }),
    ];
    const stats = summarise(jobs, devices, week);
    const row = stats.utilization.find((r) => r.deviceId === 'a')!;
    expect(row.cells[0]).toBeCloseTo(1); // Monday 00:00
    expect(row.cells[6 * 24 + 23]).toBeCloseTo(1); // Sunday 23:00
    expect(stats.kpis.printHours).toBeCloseTo(2);
  });

  it('averages a 30-day cell over the weeks it covers', () => {
    const range = { from: at('2026-09-07T00:00'), to: at('2026-09-21T00:00') }; // two Mondays
    const jobs = [
      job({ started: at('2026-09-08T10:00'), ended: at('2026-09-08T11:00') }),
    ];
    const row = summarise(jobs, devices, range).utilization.find(
      (r) => r.deviceId === 'a',
    )!;
    expect(row.cells[1 * 24 + 10]).toBeCloseTo(0.5);
  });

  it('reads a 25-hour DST day without shifting the grid', () => {
    // 2026-10-25: clocks go back at 03:00 CEST to 02:00 CET in Prague.
    const range = { from: at('2026-10-19T00:00'), to: at('2026-10-26T00:00') };
    const jobs = [
      job({ started: at('2026-10-25T00:00'), ended: at('2026-10-25T06:00') }),
    ];
    const row = summarise(jobs, devices, range).utilization.find(
      (r) => r.deviceId === 'a',
    )!;
    for (let hour = 0; hour < 6; hour++)
      expect(row.cells[6 * 24 + hour]).toBeCloseTo(1);
    expect(row.cells[6 * 24 + 6]).toBe(0);
  });

  it('measures fleet utilization over every printer in the fleet', () => {
    const jobs = [job({ started: week.from, ended: week.from + 84 * HOUR })]; // half the week, one of two printers
    expect(summarise(jobs, devices, week).kpis.utilization).toBeCloseTo(0.25);
  });

  it('leaves a printer that left the fleet out of utilization', () => {
    const jobs = [
      job({
        deviceId: 'gone',
        started: week.from,
        ended: week.from + 84 * HOUR,
      }),
    ];
    const kpis = summarise(jobs, devices, week).kpis;
    expect(kpis.utilization).toBe(0);
    expect(kpis.printHours).toBe(84);
  });

  it('keeps a printer that left the fleet under its id and an unknown model', () => {
    const jobs = [
      job({
        deviceId: 'gone',
        jobId: 'gone:1',
        started: at('2026-09-22T10:00'),
        ended: at('2026-09-22T11:00'),
        outcome: 'failed',
        machineState: 'ERROR',
      }),
    ];
    const stats = summarise(jobs, devices, week);
    expect(stats.byDevice).toContainEqual(
      expect.objectContaining({ key: 'gone', label: 'gone', failed: 1 }),
    );
  });

  it('ranks failures and counts them by machine state', () => {
    const jobs = [
      job({
        deviceId: 'b',
        jobId: 'b:1',
        started: at('2026-09-22T10:00'),
        ended: at('2026-09-22T11:00'),
        outcome: 'failed',
        machineState: 'ERROR',
      }),
      job({
        deviceId: 'b',
        jobId: 'b:2',
        started: at('2026-09-23T10:00'),
        ended: at('2026-09-23T11:00'),
        outcome: 'cancelled',
        machineState: 'STOPPED',
      }),
      job({ started: at('2026-09-24T10:00'), ended: at('2026-09-24T11:00') }),
    ];
    const stats = summarise(jobs, devices, week);
    expect(stats.byDevice[0]).toMatchObject({
      key: 'b',
      label: 'XL-01',
      jobs: 2,
      failed: 1,
      cancelled: 1,
      successRate: 0,
    });
    expect(stats.byMachineState).toEqual([
      { state: 'ERROR', count: 1 },
      { state: 'STOPPED', count: 1 },
    ]);
    expect(stats.recentFailures.map((j) => j.jobId)).toEqual(['b:2', 'b:1']);
  });

  it('compares with the period before', () => {
    const before = previousRange(week);
    expect(before).toEqual({
      from: week.from - (week.to - week.from),
      to: week.from,
    });
    const jobs = [
      job({ started: before.from + HOUR, ended: before.from + 2 * HOUR }),
    ];
    const stats = summarise(jobs, devices, week);
    expect(stats.kpis.jobs).toBe(0);
    expect(stats.previous?.jobs).toBe(1);
  });

  it('has nothing to compare with when the period before holds no jobs', () => {
    const jobs = [
      job({ started: week.from + HOUR, ended: week.from + 2 * HOUR }),
    ];
    expect(summarise(jobs, devices, week).previous).toBeUndefined();
  });
});

describe('the trend', () => {
  it('has one entry per local day, and splits a print that runs over midnight', () => {
    const jobs = [
      job({ started: at('2026-09-22T22:00'), ended: at('2026-09-23T02:00') }),
      job({
        jobId: 'a:2',
        started: at('2026-09-23T10:00'),
        ended: at('2026-09-23T11:00'),
        outcome: 'failed',
      }),
    ];
    const { trend } = summarise(jobs, devices, week);
    expect(trend).toHaveLength(7);
    expect(trend[0]?.start).toBe(at('2026-09-21T00:00'));
    expect(trend[1]).toEqual({
      start: at('2026-09-22T00:00'),
      jobs: 0,
      successRate: undefined,
      printHours: 2,
    });
    expect(trend[2]).toEqual({
      start: at('2026-09-23T00:00'),
      jobs: 2,
      successRate: 0.5,
      printHours: 3,
    });
  });

  it('starts at the first midnight of a period that starts mid-day, and keeps a DST day whole', () => {
    // Sunday 2026-10-25 is 25 hours long in Prague.
    const range = { from: at('2026-10-24T15:00'), to: at('2026-10-26T09:00') };
    const { trend } = summarise(
      [job({ started: at('2026-10-25T01:00'), ended: at('2026-10-25T05:00') })],
      devices,
      range,
    );
    expect(trend.map((day) => day.start)).toEqual([
      at('2026-10-25T00:00'),
      at('2026-10-26T00:00'),
    ]);
    expect(trend[0]?.printHours).toBe(5);
  });
});

describe('the files', () => {
  it('groups jobs by name, most printed first, then most failed', () => {
    const jobs = [
      job({
        name: 'hook',
        started: at('2026-09-22T10:00'),
        ended: at('2026-09-22T11:00'),
      }),
      job({
        name: 'hook',
        jobId: 'a:2',
        started: at('2026-09-23T10:00'),
        ended: at('2026-09-23T12:00'),
        outcome: 'cancelled',
      }),
      job({
        name: 'clip',
        started: at('2026-09-24T10:00'),
        ended: at('2026-09-24T10:30'),
      }),
      job({
        name: 'arm',
        started: at('2026-09-24T11:00'),
        ended: at('2026-09-24T11:30'),
        outcome: 'failed',
      }),
      // Ended before the week: not counted.
      job({
        name: 'clip',
        started: at('2026-09-20T10:00'),
        ended: at('2026-09-20T11:00'),
      }),
    ];
    const { files } = summarise(jobs, devices, week);
    expect(files.map((row) => row.name)).toEqual(['hook', 'arm', 'clip']);
    expect(files[0]).toEqual({
      name: 'hook',
      prints: 2,
      finished: 1,
      failed: 0,
      cancelled: 1,
      successRate: 0.5,
      printMs: 3 * HOUR,
      lastEnded: at('2026-09-23T12:00'),
    });
  });
});

describe('narrow', () => {
  const jobs = [
    job({ started: at('2026-09-22T10:00'), ended: at('2026-09-22T11:00') }),
    job({
      deviceId: 'b',
      started: at('2026-09-22T10:00'),
      ended: at('2026-09-22T11:00'),
    }),
    job({
      deviceId: 'gone',
      started: at('2026-09-22T10:00'),
      ended: at('2026-09-22T11:00'),
    }),
  ];

  it('keeps everything, gone printers too, for the whole fleet', () => {
    const scoped = narrow(jobs, devices, WHOLE_FLEET);
    expect(scoped.jobs).toBe(jobs);
    expect(scoped.devices).toBe(devices);
  });

  it('keeps the chosen printers, the tagged ones, or both at once', () => {
    expect(
      narrow(jobs, devices, { printers: ['b'], tag: '' }).jobs.map(
        (j) => j.deviceId,
      ),
    ).toEqual(['b']);
    expect(
      narrow(jobs, devices, { printers: [], tag: 'garage' }).devices.map(
        (d) => d.id,
      ),
    ).toEqual(['a']);
    expect(
      narrow(jobs, devices, { printers: ['b'], tag: 'garage' }).jobs,
    ).toEqual([]);
  });
});
