import type { StreamEvent } from '@prusa/connect';
import { describe, expect, it, vi } from 'vitest';

import {
  createCameras,
  DEVICE_REFRESH_FLOOR_MS,
  FLEET_REFRESH_FLOOR_MS,
} from './cameras.svelte';
import type { Camera, CamerasClient } from './services/cameras';
import { fakeClock, StubApiError, stubUrlBuilder } from './stubs';

const cameraOf = (name: string, deviceId: string, serving = true): Camera => ({
  device_id: deviceId,
  type: 'generic',
  formats: ['image/jpeg'],
  name,
  serving,
  snapshot: true,
  stream: true,
  watch: false,
});

/**
 * A camera that says how long it waits between two frames, as the listing
 * publishes it: `min_interval_ms`, a number of milliseconds.
 */
const declaring = (camera: Camera, intervalMs: number): Camera => ({
  ...camera,
  min_interval_ms: intervalMs,
});

/**
 * The same camera with the driver settings key instead. An older core and a
 * module answering with its own rows both spell it this way, so the store
 * still has to read it.
 */
const declaringSettings = (camera: Camera, interval: string | number): Camera =>
  ({ ...camera, min_interval: interval }) as unknown as Camera;

interface Calls {
  list: number;
  devices: (string | undefined)[];
}

const stubClient = (
  rows: readonly Camera[],
  refuse?: StubApiError,
): { client: CamerasClient; calls: Calls } => {
  const calls: Calls = { list: 0, devices: [] };
  const client: CamerasClient = {
    watchCamera: () => Promise.resolve('v=0'),
    listCameras: (request) => {
      if (refuse) throw refuse;
      calls.list += 1;
      const device = request?.query?.device;
      calls.devices.push(device);
      const kept =
        device === undefined
          ? rows
          : rows.filter((row) => row.device_id === device);

      return Promise.resolve({ items: [...kept] });
    },
    transport: stubUrlBuilder(),
  };

  return { client, calls };
};

describe('Cameras', () => {
  it('starts unread, which is not the same as a printer with no camera', () => {
    const { client } = stubClient([]);
    const store = createCameras(client, 'printer-1');

    expect(store.items).toEqual([]);
    expect(store.loaded).toBe(false);
    expect(store.first).toBeUndefined();
  });

  it('reads one printer cameras and says which are serving', async () => {
    const { client, calls } = stubClient([
      cameraOf('front', 'printer-1'),
      cameraOf('back', 'printer-1', false),
      cameraOf('other', 'printer-2'),
    ]);
    const store = createCameras(client, 'printer-1');

    await store.load();

    expect(calls.devices).toEqual(['printer-1']);
    expect(store.items).toHaveLength(2);
    expect(store.serving.map((row) => row.name)).toEqual(['front']);
    expect(store.first?.name).toBe('front');
  });

  // svelte-data.md: two readers of the same list cost one request.
  it('costs one request however many readers ask at once', async () => {
    const { client, calls } = stubClient([cameraOf('front', 'printer-1')]);
    const store = createCameras(client, 'printer-1');

    await Promise.all([store.load(), store.load()]);

    expect(calls.list).toBe(1);
  });

  // The server starts a camera after the write that declares it answered.
  it('reads again until the listing carries a camera just declared', async () => {
    const rows: Camera[] = [];
    const { client, calls } = stubClient(rows);
    const clock = fakeClock();
    const store = createCameras(client, 'printer-1', { timers: clock });

    const settled = store.loadUntil((items) => items.length > 0);
    await vi.waitFor(() => {
      expect(clock.pending).toBe(1);
    });
    rows.push(cameraOf('front', 'printer-1'));
    clock.advance(300);
    await settled;

    expect(calls.list).toBe(2);
    expect(store.items.map((row) => row.name)).toEqual(['front']);
  });

  it('gives up after its tries when the camera never comes', async () => {
    const { client, calls } = stubClient([]);
    const clock = fakeClock();
    const store = createCameras(client, 'printer-1', { timers: clock });

    const settled = store.loadUntil(() => false, 3);
    for (let tick = 0; tick < 2; tick += 1) {
      await vi.waitFor(() => {
        expect(clock.pending).toBe(1);
      });
      clock.advance(300);
    }
    await settled;

    expect(calls.list).toBe(3);
    expect(clock.pending).toBe(0);
  });

  it('builds the snapshot and the stream address through the SDK', () => {
    const { client } = stubClient([]);
    const store = createCameras(client, 'printer-1');

    expect(store.snapshotUrl('front')).toBe(
      'https://printer.example/api/v1/cameras/front/snapshot?tick=0',
    );
    expect(store.streamUrl('front')).toBe(
      'https://printer.example/api/v1/cameras/front/stream',
    );
  });

  it('offers no still address for a camera that keeps no still', () => {
    const { client } = stubClient([]);
    const store = createCameras(client, 'printer-1');
    const still = cameraOf('front', 'printer-1');

    expect(store.stillUrl(still)).toBe(store.snapshotUrl('front'));
    expect(store.stillUrl({ ...still, snapshot: false })).toBeUndefined();
  });

  // A camera that announces its frames loads its still on each one and not on
  // the clock; a camera that announces none still follows the clock.
  it('loads a still when its camera announces a frame', () => {
    const clock = fakeClock();
    const { client } = stubClient([]);
    const store = createCameras(client, 'printer-1', { timers: clock });
    let listener: ((event: StreamEvent) => void) | undefined;
    const stop = store.follow({
      subscribe: (next) => {
        listener = next;

        return () => {
          listener = undefined;
        };
      },
    });
    const frame = (camera: string): StreamEvent =>
      // The cast stands in for the narrowed union the SDK yields for one type.
      ({
        type: 'com.prusa.connect.camera.frame.received',
        id: '1',
        payload: {},
        envelope: { subject: `device:${camera}` },
      }) as StreamEvent;

    const before = store.snapshotUrl('front');
    listener?.(frame('front'));
    const announced = store.snapshotUrl('front');
    expect(announced).not.toBe(before);

    const ticking = store.snapshotUrl('side');
    const listening = store.listen();
    clock.advance(2000);
    expect(store.snapshotUrl('front')).toBe(announced);
    expect(store.snapshotUrl('side')).not.toBe(ticking);

    // A camera silent past the hold time goes back on the clock, so a picture
    // it no longer has is asked for again.
    const now = vi.spyOn(Date, 'now').mockReturnValue(Date.now() + 6 * 60_000);
    clock.advance(2000);
    expect(store.snapshotUrl('front')).not.toBe(announced);
    now.mockRestore();

    listening();
    stop();
    expect(listener).toBeUndefined();
  });

  it('gives the browser a new address on every tick', () => {
    const clock = fakeClock();
    const { client } = stubClient([]);
    const store = createCameras(client, 'printer-1', {
      timers: clock,
      refreshMs: 2000,
    });
    const before = store.snapshotUrl('front');
    const stop = store.listen();

    clock.advance(2000);
    expect(store.tick).toBe(1);
    expect(store.snapshotUrl('front')).not.toBe(before);

    clock.advance(2000);
    expect(store.tick).toBe(2);

    stop();
    expect(clock.pending).toBe(0);
  });

  it('keeps ticking until the last pane that listens lets go', () => {
    const clock = fakeClock();
    const { client } = stubClient([]);
    const store = createCameras(client, undefined, {
      timers: clock,
      refreshMs: 2000,
    });
    const card = store.listen();
    const screen = store.listen();

    card();
    card();
    clock.advance(2000);
    expect(store.tick).toBe(1);
    expect(clock.pending).toBe(1);

    screen();
    expect(clock.pending).toBe(0);
    clock.advance(2000);
    expect(store.tick).toBe(1);
  });

  it('stops the tick on a hard stop, whoever listens, until the next listen', () => {
    const clock = fakeClock();
    const { client } = stubClient([]);
    const store = createCameras(client, undefined, {
      timers: clock,
      refreshMs: 2000,
    });
    const card = store.listen();

    store.stop();
    expect(clock.pending).toBe(0);

    const pane = store.listen();
    clock.advance(2000);
    expect(store.tick).toBe(1);

    pane();
    expect(clock.pending).toBe(1);
    card();
    expect(clock.pending).toBe(0);
  });

  it('asks for a new frame when a person asks', () => {
    const { client } = stubClient([]);
    const store = createCameras(client, 'printer-1');

    store.refresh();

    expect(store.tick).toBe(1);
  });

  it('reports a refusal by its code and never as a sentence', async () => {
    const { client } = stubClient(
      [],
      new StubApiError('core.auth/ACCESS_DENIED', 403),
    );
    const store = createCameras(client, 'printer-1');

    await store.load();

    expect(store.error?.code).toBe('core.auth/ACCESS_DENIED');
    expect(store.error?.status).toBe(403);
    expect(store.items).toEqual([]);
  });

  it('waits the fallback when no camera declares an interval', async () => {
    const { client } = stubClient([cameraOf('front', 'printer-1')]);
    const store = createCameras(client, 'printer-1', { refreshMs: 3000 });

    await store.load();

    expect(store.declaredMs).toBeUndefined();
    expect(store.refreshMs).toBe(3000);
  });

  it('follows the interval a camera declares', async () => {
    const { client } = stubClient([
      declaring(cameraOf('front', 'printer-1'), 4000),
    ]);
    const store = createCameras(client, 'printer-1', { refreshMs: 2000 });
    store.floorMs = DEVICE_REFRESH_FLOOR_MS;

    await store.load();

    // Slower than the fallback and slower than the floor, so it is the camera
    // that decides: asking four times a second for a frame that arrives every
    // four seconds is three wasted requests.
    expect(store.declaredMs).toBe(4000);
    expect(store.refreshMs).toBe(4000);
  });

  it('reads a Go duration on the driver settings key', async () => {
    const { client } = stubClient([
      declaringSettings(cameraOf('front', 'printer-1'), '2s500ms'),
    ]);
    const store = createCameras(client, 'printer-1');

    await store.load();

    expect(store.declaredMs).toBe(2500);
  });

  it('reads a number of milliseconds on the driver settings key', async () => {
    const { client } = stubClient([
      declaringSettings(cameraOf('front', 'printer-1'), 2500),
    ]);
    const store = createCameras(client, 'printer-1');

    await store.load();

    expect(store.declaredMs).toBe(2500);
  });

  it('takes the published member over the driver settings key', async () => {
    const row = declaringSettings(
      declaring(cameraOf('front', 'printer-1'), 4000),
      '30s',
    );
    const { client } = stubClient([row]);
    const store = createCameras(client, 'printer-1', { refreshMs: 2000 });
    store.floorMs = DEVICE_REFRESH_FLOOR_MS;

    await store.load();

    // The core publishes min_interval_ms and it is the camera's own cadence.
    // The settings key is read only when the listing carries no such member.
    expect(store.declaredMs).toBe(4000);
  });

  it('never asks faster than the floor the page set', async () => {
    const { client } = stubClient([
      declaring(cameraOf('front', 'printer-1'), 200),
    ]);
    const store = createCameras(client, 'printer-1');

    await store.load();

    // The camera can do five frames a second and neither page will ask for
    // them: a fleet of four hundred cards is four hundred requests a tick.
    expect(store.declaredMs).toBe(200);
    expect(store.refreshMs).toBe(FLEET_REFRESH_FLOOR_MS);

    store.floorMs = DEVICE_REFRESH_FLOOR_MS;
    expect(store.refreshMs).toBe(DEVICE_REFRESH_FLOOR_MS);
  });

  it('takes the fastest camera when several declare', async () => {
    const { client } = stubClient([
      declaring(cameraOf('front', 'printer-1'), 5000),
      declaring(cameraOf('back', 'printer-1'), 1500),
      cameraOf('quiet', 'printer-1'),
    ]);
    const store = createCameras(client, 'printer-1');

    await store.load();

    // One tick serves every pane on the screen, so the fastest camera sets it.
    expect(store.declaredMs).toBe(1500);
    expect(store.refreshMs).toBe(1500);
  });

  it('keeps the floor when a page asks for one that is not a wait', () => {
    const { client } = stubClient([]);
    const store = createCameras(client, 'printer-1');

    store.floorMs = 0;
    store.floorMs = -1;
    store.floorMs = Number.NaN;

    expect(store.floorMs).toBe(FLEET_REFRESH_FLOOR_MS);
  });

  it('holds the floor against a fallback that is faster than it', () => {
    const { client } = stubClient([]);
    const store = createCameras(client, 'printer-1', { refreshMs: 50 });

    expect(store.refreshMs).toBe(FLEET_REFRESH_FLOOR_MS);
  });

  it('ticks at the floor a page named, not at the fallback', () => {
    const clock = fakeClock();
    const { client } = stubClient([]);
    const store = createCameras(client, 'printer-1', {
      timers: clock,
      refreshMs: 4000,
    });
    const stop = store.listen(DEVICE_REFRESH_FLOOR_MS);

    // The floor is a floor and never a rate: a fallback of four seconds is
    // slower than it, so four seconds is the wait.
    clock.advance(500);
    expect(store.tick).toBe(0);
    clock.advance(3500);
    expect(store.tick).toBe(1);
    expect(store.floorMs).toBe(DEVICE_REFRESH_FLOOR_MS);

    stop();
    expect(clock.pending).toBe(0);
  });

  it('reads every camera when no printer was named', async () => {
    const { client, calls } = stubClient([
      cameraOf('front', 'printer-1'),
      cameraOf('other', 'printer-2'),
    ]);
    const store = createCameras(client);

    await store.load();

    expect(calls.devices).toEqual([undefined]);
    expect(store.items).toHaveLength(2);
  });
});

// A browser can sign out and sign in as somebody else without the page
// reloading. The store then holds nothing of the first person.
describe('Cameras reset', () => {
  it('forgets every camera of the last person', async () => {
    const { client } = stubClient([cameraOf('cam-1', 'printer-1')]);
    const store = createCameras(client);
    await store.load();

    store.reset();

    expect(store.items).toEqual([]);
    expect(store.serving).toEqual([]);
    expect(store.loaded).toBe(false);
    expect(store.loading).toBe(false);
  });

  it('drops an answer that was asked for before the reset', async () => {
    let answer: (() => void) | undefined;
    const client: CamerasClient = {
      ...stubClient([]).client,
      listCameras: () =>
        new Promise((resolve) => {
          answer = () => {
            resolve({ items: [cameraOf('cam-1', 'printer-1')] });
          };
        }),
    };
    const store = createCameras(client);
    const read = store.load();

    store.reset();
    answer?.();
    await read;

    expect(store.items).toEqual([]);
    expect(store.loaded).toBe(false);
  });
});
