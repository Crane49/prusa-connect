import type { StreamEvent } from '@prusa/connect';
import { describe, expect, it } from 'vitest';

import { createLiveTelemetry } from './live-telemetry.svelte';
import type { TelemetryClient } from './services/telemetry';
import { fakeEvents, fakeStream, hubEvent, noEvents } from './stubs';

const sampled = (deviceId: string, path: string, value: unknown): StreamEvent =>
  hubEvent(
    'com.prusa.connect.device.telemetry.sampled',
    { path, value },
    `device:${deviceId}`,
  );

const stubClient = (events = noEvents): TelemetryClient => ({ events });

describe('LiveTelemetry', () => {
  it('starts with no reading and no frame', () => {
    const store = createLiveTelemetry(stubClient(), 'printer-1');

    expect(store.rows).toEqual([]);
    expect(store.paths).toEqual([]);
    expect(store.frames).toBe(0);
    expect(store.deviceId).toBe('printer-1');
  });

  it('applies a frame and keeps the newest value at that path', () => {
    const store = createLiveTelemetry(stubClient(), 'printer-1');

    store.apply(sampled('printer-1', '/tools/0/nozzle_temp', 21.5));
    store.apply(sampled('printer-1', '/tools/0/nozzle_temp', 200.1));

    expect(store.value('/tools/0/nozzle_temp')).toBe(200.1);
    expect(store.frames).toBe(2);
    expect(store.rows).toHaveLength(1);
  });

  it('sorts the state tree by path, which is the order a table draws', () => {
    const store = createLiveTelemetry(stubClient(), 'printer-1');

    store.apply(sampled('printer-1', '/speed', 100));
    store.apply(sampled('printer-1', '/axis/z', 0.2));
    store.apply(
      sampled(
        'printer-1',
        '/tools/0/material_package_instance/package/material/abbreviation',
        'PLA',
      ),
    );

    expect(store.paths).toEqual([
      '/axis/z',
      '/speed',
      '/tools/0/material_package_instance/package/material/abbreviation',
    ]);
    expect(
      store.value(
        '/tools/0/material_package_instance/package/material/abbreviation',
      ),
    ).toBe('PLA');
  });

  it('ignores a frame about another printer', () => {
    const store = createLiveTelemetry(stubClient(), 'printer-1');

    store.apply(sampled('printer-2', '/tools/0/nozzle_temp', 200));

    expect(store.rows).toEqual([]);
    expect(store.frames).toBe(0);
  });

  it('ignores a value that is neither a number, a word nor a flag', () => {
    const store = createLiveTelemetry(stubClient(), 'printer-1');

    store.apply(sampled('printer-1', '/heaters', { nozzle: 200 }));

    expect(store.rows).toEqual([]);
  });

  it('ignores a frame with no path', () => {
    const store = createLiveTelemetry(stubClient(), 'printer-1');

    store.apply(sampled('printer-1', '', 200));

    expect(store.rows).toEqual([]);
  });

  it('keeps a bounded history per path, for a small chart', () => {
    const store = createLiveTelemetry(stubClient(), 'printer-1', {
      history: 3,
    });

    for (const value of [1, 2, 3, 4, 5]) {
      store.apply(sampled('printer-1', '/tools/0/nozzle_temp', value));
    }

    expect(store.history).toBe(3);
    expect(
      store.points('/tools/0/nozzle_temp').map((point) => point.value),
    ).toEqual([3, 4, 5]);
  });

  it('keeps no history for a path that never reads a number', () => {
    const store = createLiveTelemetry(stubClient(), 'printer-1');

    store.apply(sampled('printer-1', '/state', 'printing'));

    expect(store.points('/state')).toEqual([]);
    expect(store.value('/state')).toBe('printing');
  });

  // svelte-5.md: the map is replaced whole, so one frame is one
  // recomputation for its readers and not one per path.
  it('replaces the map rather than editing it in place', () => {
    const store = createLiveTelemetry(stubClient(), 'printer-1');
    store.apply(sampled('printer-1', '/speed', 100));
    const before = store.values;

    store.apply(sampled('printer-1', '/speed', 120));

    expect(store.values).not.toBe(before);
    expect(before['/speed']?.value).toBe(100);
  });

  it('reads the stream for one printer and closes it on teardown', async () => {
    const stream = fakeStream();
    const store = createLiveTelemetry(
      stubClient(fakeEvents(stream)),
      'printer-1',
    );
    const stop = store.listen();

    await stream.push(sampled('printer-1', '/temperature/bed', 60));

    expect(store.value('/temperature/bed')).toBe(60);
    stop();
    expect(stream.closed).toBe(true);
  });

  it('empties itself for a page moving to another printer', () => {
    const store = createLiveTelemetry(stubClient(), 'printer-1');
    store.apply(sampled('printer-1', '/speed', 100));

    store.clear();

    expect(store.rows).toEqual([]);
    expect(store.frames).toBe(0);
    expect(store.points('/speed')).toEqual([]);
  });

  it('takes a point instant from the envelope and not from this browser', () => {
    const store = createLiveTelemetry(stubClient(), 'printer-1');

    store.apply({
      type: 'com.prusa.connect.device.telemetry.sampled',
      payload: { path: '/tools/0/nozzle_temp', value: 215 },
      envelope: {
        subject: 'device:printer-1',
        time: '2026-09-23T10:00:00.000Z',
      },
    } as unknown as StreamEvent);

    expect(store.points('/tools/0/nozzle_temp')).toEqual([
      { at: Date.parse('2026-09-23T10:00:00.000Z'), value: 215 },
    ]);
  });

  // A frame with no usable time still has to land: an origin that sends none,
  // and a malformed one, are both frames a person is watching arrive.
  it('falls back to this browser for a frame that carries no usable time', () => {
    const store = createLiveTelemetry(stubClient(), 'printer-1');
    const before = Date.now();

    store.apply({
      type: 'com.prusa.connect.device.telemetry.sampled',
      payload: { path: '/tools/0/nozzle_temp', value: 215 },
      envelope: { subject: 'device:printer-1', time: 'not a time' },
    } as unknown as StreamEvent);

    const [point] = store.points('/tools/0/nozzle_temp');
    expect(point?.at).toBeGreaterThanOrEqual(before);
  });
});

describe('LiveTelemetry for one person', () => {
  // The SDK reconnects after a drop only while the cookie still belongs to
  // the person named here.
  it('names the person it reads for when it opens the stream', () => {
    const stream = fakeStream();
    const asked: unknown[] = [];
    // The cast names the generic signature a test cannot write out.
    const events = ((options: unknown) => {
      asked.push(options);

      return stream.subscription;
    }) as unknown as TelemetryClient['events'];
    const store = createLiveTelemetry({ events }, 'printer-1', {
      principal: 'user:ada',
    });

    const stop = store.listen();
    stop();

    expect(asked).toEqual([expect.objectContaining({ principal: 'user:ada' })]);
  });
});
