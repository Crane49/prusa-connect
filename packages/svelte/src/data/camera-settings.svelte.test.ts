import { describe, expect, it } from 'vitest';

import { CameraSettings, printerOf } from './camera-settings.svelte';
import type {
  CameraDeclaration,
  CameraSettingsClient,
} from './services/camera-settings';
import { StubApiError } from './stubs';

const declaration = (
  name: string,
  config: Record<string, unknown>,
  extra: Partial<CameraDeclaration> = {},
): CameraDeclaration => ({
  name,
  type: 'push',
  config,
  version: '1',
  ...extra,
});

type Put = Parameters<CameraSettingsClient['putCamera']>[0];
type Delete = Parameters<CameraSettingsClient['deleteCamera']>[0];

/**
 * The camera API over one list. Each camera has its own version, which a
 * write moves, so a write quoting a version an earlier write used up is
 * refused.
 */
const fakeClient = (
  start: readonly CameraDeclaration[],
  options: { refuse?: StubApiError; allowed?: boolean | 'throws' } = {},
) => {
  let rows = [...start];
  const puts: Put[] = [];
  const deletes: Delete[] = [];
  let reads = 0;

  const versionOf = (name: string): string =>
    rows.find((row) => row.name === name)?.version ?? '0';

  const fence = (
    name: string,
    header: { 'If-Match'?: string } | undefined,
  ): void => {
    const quoted = header?.['If-Match'];
    if (quoted !== undefined && quoted !== `"${versionOf(name)}"`) {
      throw new StubApiError('core.resource/VERSION_CONFLICT', 412);
    }
  };

  const client: CameraSettingsClient = {
    listCameraSettings: () => {
      reads += 1;

      return Promise.resolve({ items: rows.map((row) => ({ ...row })) });
    },
    putCamera: (request: Put) => {
      puts.push(request);
      if (options.refuse) return Promise.reject(options.refuse);
      const name = request.path.camera;
      fence(name, request.header);
      const next = declaration(name, request.body.config, {
        type: request.body.type,
        version: String(Number(versionOf(name)) + 1),
      });
      rows = [...rows.filter((row) => row.name !== name), next];

      return Promise.resolve(next);
    },
    deleteCamera: (request: Delete) => {
      deletes.push(request);
      if (options.refuse) return Promise.reject(options.refuse);
      fence(request.path.camera, request.header);
      rows = rows.filter((row) => row.name !== request.path.camera);

      return Promise.resolve(undefined);
    },
    checkAccess: () =>
      options.allowed === 'throws'
        ? Promise.reject(new StubApiError('core.auth/FORBIDDEN'))
        : Promise.resolve({ allowed: options.allowed ?? true }),
  } as unknown as CameraSettingsClient;

  return {
    client,
    puts,
    deletes,
    reads: () => reads,
  };
};

describe('CameraSettings', () => {
  it('starts unread, which a reader tells apart from no cameras', () => {
    const store = new CameraSettings(fakeClient([]).client);

    expect(store.loaded).toBe(false);
    expect(store.items).toEqual([]);
    expect(store.allowed).toBeUndefined();
    expect(store.pending).toBeUndefined();
  });

  it('reads every camera and keeps the ones one printer declares', async () => {
    const fake = fakeClient([
      declaration('bed', { printer: 'p1' }),
      declaration('door', { printer: 'p2' }),
      declaration('room', {}),
    ]);
    const store = new CameraSettings(fake.client);

    await store.load();

    expect(store.loaded).toBe(true);
    expect(store.allowed).toBe(true);
    expect(store.of('p1').map((row) => row.name)).toEqual(['bed']);
    expect(store.of('p3')).toEqual([]);
  });

  it('gives a second load the read already in flight', async () => {
    const fake = fakeClient([]);
    const store = new CameraSettings(fake.client);

    await Promise.all([store.load(), store.load()]);

    expect(fake.reads()).toBe(1);
  });

  it('leaves the answer unknown when the access check fails', async () => {
    const store = new CameraSettings(
      fakeClient([], { allowed: 'throws' }).client,
    );

    await store.load();

    expect(store.loaded).toBe(true);
    expect(store.allowed).toBeUndefined();
  });

  it('adds a camera only as a new name, then reads the list and the host again', async () => {
    const fake = fakeClient([]);
    let reread = 0;
    const store = new CameraSettings(fake.client, {
      afterWrite: () => {
        reread += 1;

        return Promise.resolve();
      },
    });
    await store.load();

    const ok = await store.save({
      name: 'bed',
      type: 'generic',
      printer: 'p1',
      snapshotUrl: ' http://cam/snap.jpg ',
      streamUrl: '',
    });

    expect(ok).toBe(true);
    expect(fake.puts[0]?.header).toEqual({ 'If-None-Match': '*' });
    expect(fake.puts[0]?.body).toEqual({
      type: 'generic',
      config: { printer: 'p1', snapshot_url: 'http://cam/snap.jpg' },
    });
    expect(store.of('p1').map((row) => row.name)).toEqual(['bed']);
    expect(reread).toBe(1);
    expect(store.pending).toBeUndefined();
  });

  it('moves a camera and keeps its type and the settings the draft does not name', async () => {
    const fake = fakeClient([
      declaration(
        'bed',
        {
          printer: 'p1',
          snapshot_every: '1m',
          snapshot_url: 'http://cam/snap.jpg',
          stream_url: 'http://cam/stream',
        },
        { type: 'core/mjpeg' },
      ),
    ]);
    const store = new CameraSettings(fake.client);
    await store.load();

    // The draft asks for generic. A change keeps the type, which only a
    // removal changes, and the stream endpoint it cleared is gone.
    const ok = await store.save({
      name: 'bed',
      type: 'generic',
      printer: 'p2',
      snapshotUrl: 'http://cam/snap.jpg',
    });

    expect(ok).toBe(true);
    expect(fake.puts[0]?.header).toEqual({ 'If-Match': '"1"' });
    expect(fake.puts[0]?.body.type).toBe('core/mjpeg');
    expect(fake.puts[0]?.body.config).toEqual({
      printer: 'p2',
      snapshot_every: '1m',
      snapshot_url: 'http://cam/snap.jpg',
    });
    expect(store.of('p1')).toEqual([]);
    expect(store.of('p2').map((row) => row.name)).toEqual(['bed']);
  });

  it('quotes the version the last write left, never the one it used up', async () => {
    const fake = fakeClient([declaration('bed', { printer: 'p1' })]);
    const store = new CameraSettings(fake.client);
    await store.load();

    const draft = { name: 'bed', type: 'push', printer: 'p2' };
    expect(await store.save(draft)).toBe(true);
    expect(await store.save({ ...draft, printer: 'p3' })).toBe(true);
    expect(await store.remove(store.items[0]!)).toBe(true);

    expect(
      [...fake.puts, ...fake.deletes].map((request) => request.header),
    ).toEqual([
      { 'If-Match': '"1"' },
      { 'If-Match': '"2"' },
      { 'If-Match': '"3"' },
    ]);
    expect(store.items).toEqual([]);
  });

  it('keeps a refused write by its code and does not read the host again', async () => {
    const fake = fakeClient([declaration('bed', { printer: 'p1' })], {
      refuse: new StubApiError('core.auth/ACCESS_DENIED', 403),
    });
    let reread = 0;
    const store = new CameraSettings(fake.client, {
      afterWrite: () => {
        reread += 1;

        return Promise.resolve();
      },
    });
    await store.load();

    const ok = await store.remove(store.items[0]!);

    expect(ok).toBe(false);
    expect(store.failed).toBe('remove');
    expect(store.error?.code).toBe('core.auth/ACCESS_DENIED');
    expect(reread).toBe(0);
    expect(store.items.map((row) => row.name)).toEqual(['bed']);
  });

  it('refuses a second act while one is on its way, and sends nothing for it', async () => {
    const fake = fakeClient([declaration('bed', { printer: 'p1' })]);
    const store = new CameraSettings(fake.client);
    await store.load();

    const first = store.remove(store.items[0]!);
    const second = await store.save({
      name: 'door',
      type: 'generic',
      printer: 'p1',
    });

    expect(second).toBe(false);
    expect(fake.puts).toEqual([]);
    expect(await first).toBe(true);
  });
});

describe('printerOf', () => {
  it('reads the printer key only when it is a string', () => {
    expect(printerOf(declaration('a', { printer: 'p1' }))).toBe('p1');
    expect(printerOf(declaration('a', { printer: 7 }))).toBeUndefined();
    expect(printerOf(declaration('a', {}))).toBeUndefined();
  });
});
