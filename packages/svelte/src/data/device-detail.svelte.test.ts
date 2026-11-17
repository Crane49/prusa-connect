import type { StreamEvent } from '@prusa/connect';
import { describe, expect, it, vi } from 'vitest';

import {
  DeviceDetail,
  type DeviceDetailFleet,
  RECENT_PRINTS,
} from './device-detail.svelte';
import type { Device } from '../domain/command';
import type {
  DeviceDetailClient,
  DeviceEntry,
  DeviceJobs,
  DeviceListing,
} from './services/device';

const entryOf = (name: string, path: string, folder = false): DeviceEntry => ({
  folder,
  modified: '2026-09-12T00:00:00Z',
  name,
  path,
  read_only: false,
  size: folder ? 0 : 1024,
});

// A printer with nothing on the bed and nothing behind it. The jobs read is
// not what these cases are about.
const noJobs: DeviceJobs = {};

// A client whose listings answer when the test says so, keyed by the path each
// one asked for. It is how a slow folder is made to answer after a fast one.
const heldClient = (): {
  client: DeviceDetailClient;
  answer: Map<string, (rows: readonly DeviceEntry[]) => void>;
} => {
  const answer = new Map<string, (rows: readonly DeviceEntry[]) => void>();
  const client: DeviceDetailClient = {
    url: '',
    getDevice: () => Promise.reject(new Error('no record here')),
    listDeviceJobs: () => Promise.resolve({ items: [], cursor: '', limit: 0 }),
    getDeviceJobs: () => Promise.resolve(noJobs),
    listDeviceStorages: () => Promise.resolve({ items: [], reported: false }),
    listDeviceFiles: (request) =>
      new Promise<DeviceListing>((resolve) => {
        const path = request.query?.path ?? '';
        answer.set(path, (rows) => {
          resolve({ directory: entryOf(path, path, true), items: [...rows] });
        });
      }),
  };

  return { client, answer };
};

const eventOf = (type: string, deviceId: string): StreamEvent =>
  // The cast stands in for the narrowed union the SDK yields for one type.
  ({
    type,
    id: '1',
    payload: {},
    envelope: { subject: `device:${deviceId}` },
  }) as StreamEvent;

const fakeFleet = () => {
  let listener: ((event: StreamEvent) => void) | undefined;
  const fleet: DeviceDetailFleet = {
    subscribe: (next) => {
      listener = next;

      return () => {
        listener = undefined;
      };
    },
  };

  return { fleet, emit: (event: StreamEvent) => listener?.(event) };
};

describe('DeviceDetail', () => {
  // A listed file's address carries the version it was listed at, so the
  // core can answer from what it holds and the address changes with the file.
  it('addresses the picture inside a print file by its listed version', async () => {
    const { client, answer } = heldClient();
    const store = new DeviceDetail(client, 'mk4-01');

    const opened = store.openFolder('usb');
    answer.get('usb')?.([entryOf('part.bgcode', 'usb/part.bgcode')]);
    await opened;

    expect(store.thumbnailUrl('usb/part.bgcode')).toBe(
      '/api/v1/devices/mk4-01/files/thumbnail?path=usb%2Fpart.bgcode&size=1024&modified=1789171200',
    );
    expect(store.thumbnailUrl('usb/gone.gcode')).toBe(
      '/api/v1/devices/mk4-01/files/thumbnail?path=usb%2Fgone.gcode',
    );
    expect(store.thumbnailUrl('usb/model.stl')).toBeUndefined();
  });

  // A person clicking down a tree leaves a request behind on
  // every click, and an answer nobody dropped overwrites the rows of the
  // folder they are actually looking at.
  it('keeps the open folder when the folder before it answers late', async () => {
    const { client, answer } = heldClient();
    const store = new DeviceDetail(client, 'mk4-01');

    const first = store.openFolder('usb/models');
    const second = store.openFolder('usb/plates');

    answer.get('usb/plates')?.([
      entryOf('plate.bgcode', 'usb/plates/plate.bgcode'),
    ]);
    await second;
    answer.get('usb/models')?.([
      entryOf('old.bgcode', 'usb/models/old.bgcode'),
    ]);
    await first;

    expect(store.directory).toBe('usb/plates');
    expect(store.entries.map((row) => row.name)).toEqual(['plate.bgcode']);
    expect(store.filesError).toBeUndefined();
    expect(store.filesLoading).toBe(false);
  });

  // The store keeps no copy of a folder it left. A copy would need rules for
  // when it goes stale, and the printer is the only one who knows.
  it('reads a folder it has read before again, with no old rows drawn', async () => {
    const { client, answer } = heldClient();
    const store = new DeviceDetail(client, 'mk4-01');

    const first = store.openFolder('usb/models');
    answer.get('usb/models')?.([
      entryOf('old.bgcode', 'usb/models/old.bgcode'),
    ]);
    await first;

    void store.openFolder('usb/plates');
    const again = store.openFolder('usb/models');

    expect(store.filesLoading).toBe(true);

    answer.get('usb/models')?.([
      entryOf('new.bgcode', 'usb/models/new.bgcode'),
    ]);
    await again;

    expect(store.entries.map((row) => row.name)).toEqual(['new.bgcode']);
  });

  // Coming back to the Files tab lists the root again while a drive is open.
  // Until the root answers, the drive's folders must not read as the drives.
  it('holds no rows of the folder it left while the next one reads', async () => {
    const { client, answer } = heldClient();
    const store = new DeviceDetail(client, 'mk4-01');

    const drive = store.openFolder('usb');
    answer.get('usb')?.([entryOf('examples', 'usb/examples', true)]);
    await drive;

    void store.openFolder('');

    expect(store.directory).toBe('');
    expect(store.filesLoaded).toBe(false);
    expect(store.entries).toEqual([]);
  });

  // A write changes the folder on screen, and the rows a person is looking
  // at stay while the printer is asked again.
  it('keeps the rows of the open folder while it reads it again', async () => {
    const { client, answer } = heldClient();
    const store = new DeviceDetail(client, 'mk4-01');

    const first = store.openFolder('usb/models');
    answer.get('usb/models')?.([
      entryOf('old.bgcode', 'usb/models/old.bgcode'),
    ]);
    await first;

    const again = store.changedAt('usb/models/made.bgcode');
    expect(store.filesLoading).toBe(false);
    expect(store.filesRefreshing).toBe(true);
    expect(store.entries.map((row) => row.name)).toEqual(['old.bgcode']);

    answer.get('usb/models')?.([
      entryOf('old.bgcode', 'usb/models/old.bgcode'),
      entryOf('made.bgcode', 'usb/models/made.bgcode'),
    ]);
    await again;

    expect(store.filesRefreshing).toBe(false);
    expect(store.entries.map((row) => row.name)).toEqual([
      'old.bgcode',
      'made.bgcode',
    ]);
  });

  it('reads nothing again for a change in a folder that is not on screen', async () => {
    const { client, answer } = heldClient();
    const store = new DeviceDetail(client, 'mk4-01');

    const first = store.openFolder('usb/models');
    answer.get('usb/models')?.([]);
    await first;
    answer.clear();

    await store.changedAt('/usb/elsewhere/cube.bgcode');

    expect(answer.size).toBe(0);
  });

  it('expands a folder under its row and folds it away', async () => {
    const { client, answer } = heldClient();
    const store = new DeviceDetail(client, 'mk4-01');

    const open = store.openFolder('usb');
    answer.get('usb')?.([entryOf('models', 'usb/models', true)]);
    await open;

    const expanding = store.expand('usb/models');
    expect(Object.hasOwn(store.expanded, 'usb/models')).toBe(true);
    answer.get('usb/models')?.([entryOf('a.bgcode', 'usb/models/a.bgcode')]);
    await expanding;
    expect(store.expanded['usb/models']?.map((row) => row.name)).toEqual([
      'a.bgcode',
    ]);

    store.collapse('usb/models');
    expect(Object.hasOwn(store.expanded, 'usb/models')).toBe(false);
  });

  // A folder expanded in the tree shows its own rows, so a file landing in
  // it or deleted from it has to show there too.
  it('reads an expanded folder again when something changes inside it', async () => {
    const { client, answer } = heldClient();
    const store = new DeviceDetail(client, 'mk4-01');

    const open = store.openFolder('usb');
    answer.get('usb')?.([entryOf('models', 'usb/models', true)]);
    await open;
    const expanding = store.expand('usb/models');
    answer.get('usb/models')?.([entryOf('a.bgcode', 'usb/models/a.bgcode')]);
    await expanding;

    const change = store.changedAt('/usb/models/b.bgcode');
    expect(store.expanded['usb/models']?.map((row) => row.name)).toEqual([
      'a.bgcode',
    ]);
    answer.get('usb/models')?.([
      entryOf('a.bgcode', 'usb/models/a.bgcode'),
      entryOf('b.bgcode', 'usb/models/b.bgcode'),
    ]);
    await change;

    expect(store.expanded['usb/models']?.map((row) => row.name)).toEqual([
      'a.bgcode',
      'b.bgcode',
    ]);
  });

  it('folds every expanded folder away when another folder opens', async () => {
    const { client, answer } = heldClient();
    const store = new DeviceDetail(client, 'mk4-01');

    const open = store.openFolder('usb');
    answer.get('usb')?.([entryOf('models', 'usb/models', true)]);
    await open;
    const expanding = store.expand('usb/models');
    answer.get('usb/models')?.([]);
    await expanding;

    void store.openFolder('sd');

    expect(store.expanded).toEqual({});
  });
});

// A client that keeps every listing still waiting, per path, so a test can
// answer an older request after a newer one.
const queuedClient = (): {
  client: DeviceDetailClient;
  waiting: (path: string) => number;
  answer: (
    path: string,
    rows: readonly DeviceEntry[],
    which?: 'oldest' | 'newest',
  ) => void;
} => {
  const held = new Map<string, ((rows: readonly DeviceEntry[]) => void)[]>();
  const client: DeviceDetailClient = {
    url: '',
    getDevice: () => Promise.reject(new Error('no record here')),
    listDeviceJobs: () => Promise.resolve({ items: [], cursor: '', limit: 0 }),
    getDeviceJobs: () => Promise.resolve(noJobs),
    listDeviceStorages: () => Promise.resolve({ items: [], reported: false }),
    listDeviceFiles: (request) =>
      new Promise<DeviceListing>((resolve) => {
        const path = request.query?.path ?? '';
        const queue = held.get(path) ?? [];
        queue.push((rows) => {
          resolve({ directory: entryOf(path, path, true), items: [...rows] });
        });
        held.set(path, queue);
      }),
  };

  return {
    client,
    waiting: (path) => held.get(path)?.length ?? 0,
    answer: (path, rows, which = 'oldest') => {
      const queue = held.get(path) ?? [];
      const next = which === 'oldest' ? queue.shift() : queue.pop();
      next?.(rows);
    },
  };
};

// Opens `usb` with a `models` folder in it and expands that folder.
const withExpanded = async (
  held: ReturnType<typeof queuedClient>,
  store: DeviceDetail,
  inside: readonly DeviceEntry[],
): Promise<void> => {
  const open = store.openFolder('usb');
  held.answer('usb', [entryOf('models', 'usb/models', true)]);
  await open;
  const expanding = store.expand('usb/models');
  held.answer('usb/models', inside);
  await expanding;
};

const namesIn = (store: DeviceDetail, path: string): string[] | undefined =>
  store.expanded[path]?.map((row) => row.name);

describe('DeviceDetail expanded folders', () => {
  // The printer answers in the order its storage walked. The tree puts the
  // folders first and the rest by name, as the open folder does.
  it('puts the folders of an expanded folder first, then the names in order', async () => {
    const held = queuedClient();
    const store = new DeviceDetail(held.client, 'mk4-01');

    await withExpanded(held, store, [
      entryOf('b.bgcode', 'usb/models/b.bgcode'),
      entryOf('parts', 'usb/models/parts', true),
      entryOf('a.bgcode', 'usb/models/a.bgcode'),
    ]);

    expect(namesIn(store, 'usb/models')).toEqual([
      'parts',
      'a.bgcode',
      'b.bgcode',
    ]);
  });

  // Two rereads of one folder: the older answer arriving last must not
  // replace the newer one.
  it('keeps the newer answer when two reads of one folder cross', async () => {
    const held = queuedClient();
    const store = new DeviceDetail(held.client, 'mk4-01');
    await withExpanded(held, store, []);

    const first = store.changedAt('usb/models/x.bgcode');
    const second = store.changedAt('usb/models/y.bgcode');
    held.answer(
      'usb/models',
      [entryOf('new.bgcode', 'usb/models/new.bgcode')],
      'newest',
    );
    held.answer('usb/models', [entryOf('old.bgcode', 'usb/models/old.bgcode')]);
    await Promise.all([first, second]);

    expect(namesIn(store, 'usb/models')).toEqual(['new.bgcode']);
  });

  // A change that arrives while the folder is still being expanded would be
  // missing from that first answer, so the folder is read again.
  it('reads a folder again for a change made while it was expanding', async () => {
    const held = queuedClient();
    const store = new DeviceDetail(held.client, 'mk4-01');
    const open = store.openFolder('usb');
    held.answer('usb', [entryOf('models', 'usb/models', true)]);
    await open;

    const expanding = store.expand('usb/models');
    const change = store.changedAt('usb/models/new.bgcode');
    expect(held.waiting('usb/models')).toBe(2);
    held.answer(
      'usb/models',
      [entryOf('new.bgcode', 'usb/models/new.bgcode')],
      'newest',
    );
    held.answer('usb/models', []);
    await Promise.all([expanding, change]);

    expect(namesIn(store, 'usb/models')).toEqual(['new.bgcode']);
  });

  // A folder the printer no longer has cannot stay open in the tree.
  it('folds away an expanded folder the reread no longer lists', async () => {
    const held = queuedClient();
    const store = new DeviceDetail(held.client, 'mk4-01');
    await withExpanded(held, store, [
      entryOf('parts', 'usb/models/parts', true),
    ]);
    const inner = store.expand('usb/models/parts');
    held.answer('usb/models/parts', []);
    await inner;

    const reread = store.changedAt('usb/models');
    held.answer('usb', []);
    await reread;

    expect(Object.keys(store.expanded)).toEqual([]);
  });
});

// A client whose usb folder holds one file, answered from memory first and
// then by the printer, as the core answers a folder it listed before.
const usbClient = (
  asked: (string | undefined)[],
  failing = false,
): DeviceDetailClient => ({
  url: '',
  getDevice: () => Promise.reject(new Error('no record here')),
  listDeviceJobs: () => Promise.resolve({ items: [], cursor: '', limit: 0 }),
  getDeviceJobs: () => Promise.resolve(noJobs),
  listDeviceStorages: () => Promise.resolve({ items: [], reported: false }),
  listDeviceFiles: (request) => {
    const refresh = request.query?.refresh === true;
    asked.push(`${request.query?.path ?? ''}${refresh ? ' refresh' : ''}`);
    if (failing) return Promise.reject(new Error('the printer is busy'));

    // The answer from memory still holds the file a stopped send removed.
    const remembered = refresh
      ? []
      : [entryOf('SHEEP_~2.BGC', 'usb/sheep_partial.bgcode')];

    return Promise.resolve({
      directory: entryOf('usb', 'usb', true),
      items: [entryOf('SHEEP_~1.BGC', 'usb/sheep.bgcode'), ...remembered],
      stale: !refresh,
    });
  },
});

describe('DeviceDetail held files', () => {
  // The machine names a print's file by its short name, with a leading
  // slash. A stopped send leaves it naming a file it removed (CL-275).
  it('holds a file a print names by its short name or its path', async () => {
    const asked: (string | undefined)[] = [];
    const store = new DeviceDetail(usbClient(asked), 'mk4-01');

    await store.checkHeld(
      ['/usb/SHEEP_~1.BGC', '/usb/SHEEP_~2.BGC', 'usb/sheep.bgcode'],
      ['/usb/SHEEP_~2.BGC'],
    );

    expect(store.holds('/usb/SHEEP_~1.BGC')).toBe(true);
    expect(store.holds('/usb/SHEEP_~2.BGC')).toBe(false);
    expect(store.holds('usb/sheep.bgcode')).toBe(true);
    expect(store.holds('/usb/OTHER.BGC')).toBeUndefined();
  });

  // What the server remembers can predate a stopped send, so a stopped
  // print's file it still holds has the printer read the folder, once for a
  // path named twice.
  it('asks the printer to read the folder for a stopped print it still finds', async () => {
    const asked: (string | undefined)[] = [];
    const store = new DeviceDetail(usbClient(asked), 'mk4-01');
    const stopped = ['/usb/SHEEP_~2.BGC'];

    await store.checkHeld(['/usb/SHEEP_~2.BGC', '/usb/SHEEP_~2.BGC'], stopped);
    await store.checkHeld(['/usb/SHEEP_~2.BGC'], stopped);

    expect(asked).toEqual(['usb', 'usb refresh']);
    expect(store.holds('/usb/SHEEP_~2.BGC')).toBe(false);
  });

  it('takes what the server holds for a print that ended otherwise', async () => {
    const asked: (string | undefined)[] = [];
    const store = new DeviceDetail(usbClient(asked), 'mk4-01');

    await store.checkHeld(['/usb/SHEEP_~2.BGC']);

    expect(asked).toEqual(['usb']);
    expect(store.holds('/usb/SHEEP_~2.BGC')).toBe(true);
  });

  it('asks no more for a stopped print whose file is already gone', async () => {
    const asked: (string | undefined)[] = [];
    const store = new DeviceDetail(usbClient(asked), 'mk4-01');

    await store.checkHeld(['/usb/OTHER.BGC'], ['/usb/OTHER.BGC']);

    expect(asked).toEqual(['usb']);
    expect(store.holds('/usb/OTHER.BGC')).toBe(false);
  });

  // A folder that will not list says nothing, so Print again stays offered.
  it('leaves a file unknown when its folder will not list', async () => {
    const asked: (string | undefined)[] = [];
    const store = new DeviceDetail(usbClient(asked, true), 'mk4-01');

    await store.checkHeld(['/usb/SHEEP_~1.BGC']);

    expect(store.holds('/usb/SHEEP_~1.BGC')).toBeUndefined();
  });

  // A file that lands may be one a print names, so its folder is checked
  // again against what the server holds, which the send already changed. The
  // printer is not asked to read the folder again.
  it('checks a folder again when a file lands in it', async () => {
    const asked: (string | undefined)[] = [];
    const store = new DeviceDetail(usbClient(asked), 'mk4-01');

    await store.checkHeld(['/usb/SHEEP_~2.BGC'], ['/usb/SHEEP_~2.BGC']);
    await store.changedAt('usb/sheep_partial.bgcode');

    expect(asked).toEqual(['usb', 'usb refresh', 'usb']);
    expect(store.holds('/usb/SHEEP_~2.BGC')).toBe(true);
  });
});

describe('DeviceDetail withdrawn printer', () => {
  it('reads as missing when its printer is withdrawn', async () => {
    const record = { id: 'mk4-01', name: 'MK4' } as Device;
    const { client } = heldClient();
    const store = new DeviceDetail(
      { ...client, getDevice: () => Promise.resolve(record) },
      'mk4-01',
    );
    let emit: (event: StreamEvent) => void = () => undefined;
    store.watch({
      subscribe: (next) => {
        emit = next;

        return () => undefined;
      },
    });
    await store.loadRecord();
    expect(store.record).toBe(record);

    const withdrawn = (origin: string): StreamEvent => ({
      type: 'com.prusa.connect.stream.origin.withdrawn',
      id: '2',
      payload: { origin },
      envelope: undefined,
    });
    emit(withdrawn('device:xl-02'));
    expect(store.record).toBe(record);

    emit(withdrawn('device:mk4-01'));
    expect(store.record).toBeUndefined();
    expect(store.recordLoaded).toBe(true);
  });
});

describe('DeviceDetail record read', () => {
  it('keeps the fault a failed record read answered with', async () => {
    const { client } = heldClient();
    const refusal = Object.assign(new Error('boom'), { status: 500 });
    const store = new DeviceDetail(
      { ...client, getDevice: () => Promise.reject(refusal) },
      'mk4-01',
    );
    await store.loadRecord();

    expect(store.record).toBeUndefined();
    expect(store.recordLoaded).toBe(true);
    expect(store.recordError).toBe(refusal);
  });
});

describe('DeviceDetail jobs', () => {
  // The page read the jobs while nothing had printed yet. The print that
  // settles since then is the one the block must show without a reload.
  it('reads the jobs again when a print of this printer starts or ends', async () => {
    const answers: DeviceJobs[] = [
      {},
      {
        last: {
          display_name: 'benchy.bgcode',
          id: 'c1de23eb-4472-483a-8f70-222e89e7f711',
          device_id: 'mk4-01',
          device_job_id: '412',
          path: '/usb/benchy.bgcode',
          progress: 100,
          started: '2026-10-07T09:00:00Z',
          ended: '2026-10-07T09:05:00Z',
          state: 'FINISHED',
          time_remaining_ms: 0,
        },
      },
    ];
    let reads = 0;
    const client: DeviceDetailClient = {
      url: '',
      getDevice: () => Promise.reject(new Error('no record here')),
      listDeviceJobs: () =>
        Promise.resolve({ items: [], cursor: '', limit: 0 }),
      getDeviceJobs: () => Promise.resolve(answers[Math.min(reads++, 1)] ?? {}),
      listDeviceStorages: () => Promise.resolve({ items: [], reported: false }),
      listDeviceFiles: () =>
        Promise.resolve({ directory: entryOf('', '', true), items: [] }),
    };
    const store = new DeviceDetail(client, 'mk4-01');
    const { fleet, emit } = fakeFleet();
    await store.load();
    const stop = store.watch(fleet);

    emit(eventOf('com.prusa.connect.device.job.settled', 'xl-02'));
    emit(eventOf('com.prusa.connect.device.record.changed', 'mk4-01'));
    expect(reads).toBe(1);

    emit(eventOf('com.prusa.connect.device.job.settled', 'mk4-01'));
    await vi.waitFor(() => {
      expect(store.jobs?.last?.display_name).toBe('benchy.bgcode');
    });

    emit(eventOf('com.prusa.connect.device.job.started', 'mk4-01'));
    expect(reads).toBe(3);

    stop();
    emit(eventOf('com.prusa.connect.device.job.settled', 'mk4-01'));
    expect(reads).toBe(3);
  });

  // The recent prints are a read of their own beside the jobs read, of this
  // printer's ended prints, and a refusal of it leaves the jobs read whole.
  it('reads the recent prints beside the jobs, and survives their refusal', async () => {
    const ended = {
      display_name: 'benchy.bgcode',
      id: 'c1de23eb-4472-483a-8f70-222e89e7f711',
      device_id: 'mk4-01',
      device_job_id: '412',
      path: '/usb/benchy.bgcode',
      progress: 100,
      started: '2026-10-07T09:00:00Z',
      ended: '2026-10-07T09:05:00Z',
      state: 'FINISHED',
      time_remaining_ms: 0,
    };
    const asked: unknown[] = [];
    let refuse = false;
    const client: DeviceDetailClient = {
      url: '',
      getDevice: () => Promise.reject(new Error('no record here')),
      listDeviceJobs: (request) => {
        asked.push({ ...request.path, ...request.query });

        return refuse
          ? Promise.reject(new Error('refused'))
          : Promise.resolve({
              items: [ended],
              cursor: '',
              limit: RECENT_PRINTS,
            });
      },
      getDeviceJobs: () => Promise.resolve({ last: ended }),
      listDeviceStorages: () => Promise.resolve({ items: [], reported: false }),
      listDeviceFiles: () =>
        Promise.resolve({ directory: entryOf('', '', true), items: [] }),
    };
    const store = new DeviceDetail(client, 'mk4-01');

    expect(store.recent).toBeUndefined();
    await store.load();

    expect(asked).toEqual([{ device_id: 'mk4-01', limit: RECENT_PRINTS }]);
    expect(store.recent?.map((job) => job.id)).toEqual([ended.id]);

    refuse = true;
    const { fleet, emit } = fakeFleet();
    const stop = store.watch(fleet);
    emit(eventOf('com.prusa.connect.device.job.settled', 'mk4-01'));
    await vi.waitFor(() => {
      expect(store.recent).toBeUndefined();
    });
    expect(store.jobsError).toBeUndefined();
    expect(store.jobs?.last?.id).toBe(ended.id);
    stop();
  });

  // A drive put in or pulled out reaches the page as an event. The storages
  // it carries replace the ones read, and the listing starts again from the
  // storages, because the open folder may be on the drive that went.
  it('takes the storages a USB drive event carries and lists the storages again', async () => {
    const listed: string[] = [];
    const client: DeviceDetailClient = {
      url: '',
      getDevice: () => Promise.reject(new Error('no record here')),
      listDeviceJobs: () =>
        Promise.resolve({ items: [], cursor: '', limit: 0 }),
      getDeviceJobs: () => Promise.resolve(noJobs),
      listDeviceStorages: () => Promise.resolve({ items: [], reported: true }),
      listDeviceFiles: (request) => {
        const path = request.query?.path ?? '';
        listed.push(path);

        return Promise.resolve({
          directory: entryOf(path, path, true),
          items: [],
        });
      },
    };
    const store = new DeviceDetail(client, 'mk4-01');
    const { fleet, emit } = fakeFleet();
    await store.openFolder('usb/models');
    await store.readStorages();
    const stop = store.watch(fleet);
    const usb = {
      free_space: 1024,
      mountpoint: '/usb',
      name: 'usb',
      read_only: false,
      type: 'USB',
    };
    const storagesEvent = (deviceId: string, items: unknown[]): StreamEvent =>
      ({
        ...eventOf('com.prusa.connect.device.storages.changed', deviceId),
        payload: { device_id: deviceId, items, reported: true },
      }) as StreamEvent;

    emit(storagesEvent('xl-02', [usb]));
    expect(store.storages?.items).toEqual([]);

    // The drive the open folder is on is still there, so the folder stays.
    emit(storagesEvent('mk4-01', [usb]));
    expect(store.storages?.items).toEqual([usb]);
    expect(store.directory).toBe('usb/models');

    // The drive went, and the folder went with it.
    emit(storagesEvent('mk4-01', []));
    expect(store.storages).toEqual({ items: [], reported: true });
    await vi.waitFor(() => {
      expect(store.directory).toBe('');
    });
    expect(listed.at(-1)).toBe('');

    stop();
  });
});

describe('DeviceDetail listing events', () => {
  const READ_BEGAN = '2026-11-12T10:00:00Z';

  const listingOf = (
    path: string,
    items: readonly DeviceEntry[],
    extra: Partial<DeviceListing> = {},
  ): DeviceListing => ({
    directory: entryOf(path, path, true),
    items: [...items],
    ...extra,
  });

  const filesChanged = (
    deviceId: string,
    path: string,
    reading: boolean,
    failed = false,
  ): StreamEvent =>
    ({
      ...eventOf('com.prusa.connect.device.files.changed', deviceId),
      payload: {
        device_id: deviceId,
        driver: 'prusalink',
        path,
        reading,
        failed,
      },
    }) as StreamEvent;

  type Listing = DeviceDetailClient['listDeviceFiles'];

  // A watched store over a client whose listings the test answers.
  const watched = (listDeviceFiles: Listing) => {
    const files = vi.fn(listDeviceFiles);
    const store = new DeviceDetail(
      { ...heldClient().client, listDeviceFiles: files },
      'mk4-01',
    );
    const { fleet, emit } = fakeFleet();
    store.watch(fleet);

    return { store, files, emit };
  };

  const a = entryOf('a.bgcode', 'usb/a.bgcode');
  const b = entryOf('b.bgcode', 'usb/b.bgcode');

  // The server reads a folder it holds nothing of: the first ask of a path
  // answers reading, and the asks after it what the read left.
  const readOnce = () => {
    const seen = new Set<string>();

    return (path: string): boolean => {
      const first = !seen.has(path);
      seen.add(path);

      return first;
    };
  };
  // A read under way of a folder the server holds nothing of, and of one it holds.
  const reading = { reading: true, reading_since: READ_BEGAN };
  const held = { ...reading, stale: true };

  // The route answers at once with what it holds. Asking again before the
  // read lands would get the same answer, so the page waits for the event.
  it('shows what the server holds and waits for the event instead of asking again', async () => {
    const { store, files } = watched(() =>
      Promise.resolve(listingOf('usb', [a], held)),
    );

    await store.openFolder('usb');

    expect(files).toHaveBeenCalledTimes(1);
    expect(files.mock.calls[0]?.[0].query?.refresh).toBeUndefined();
    expect(store.entries).toHaveLength(1);
    expect(store.filesReadingSince).toBeDefined();
    expect(store.filesRefreshing).toBe(false);
  });

  // An empty folder the server holds is empty while it is read again; only a
  // folder it holds nothing of is still waiting for its rows.
  // The server's clock need not agree with the browser's, so the read is
  // counted from when this page first heard of it, and from then on.
  it('counts a read from when the page heard of it, by the browser clock', async () => {
    vi.useFakeTimers({ toFake: ['Date'] });
    try {
      let since = READ_BEGAN;
      const { store } = watched(() =>
        Promise.resolve(
          listingOf('usb', [a], { ...held, reading_since: since }),
        ),
      );

      vi.setSystemTime(Date.UTC(2026, 10, 12, 9, 0, 0));
      await store.openFolder('usb');
      expect(store.filesReadingSince).toBe('2026-11-12T09:00:00.000Z');

      vi.setSystemTime(Date.UTC(2026, 10, 12, 9, 0, 5));
      await store.openFolder('usb', true);
      expect(store.filesReadingSince).toBe('2026-11-12T09:00:00.000Z');

      since = '2026-11-12T10:05:00Z';
      await store.openFolder('usb', true);
      expect(store.filesReadingSince).toBe('2026-11-12T09:00:05.000Z');
    } finally {
      vi.useRealTimers();
    }
  });

  it('tells a held empty folder from one it holds nothing of', async () => {
    const models = entryOf('models', 'usb/models', true);
    const { store } = watched((request) => {
      const path = request.query?.path ?? '';
      if (path === 'sd') return Promise.resolve(listingOf('sd', [], reading));
      if (path === 'usb') {
        return Promise.resolve(listingOf('usb', [models], held));
      }

      return Promise.resolve(listingOf(path, [], held));
    });

    await store.openFolder('usb');
    await store.expand('usb/models');
    expect(store.filesUnread).toBe(false);
    expect(store.expanded['usb/models']).toEqual([]);

    await store.openFolder('usb/models');
    expect(store.filesUnread).toBe(false);
    expect(store.filesReadingSince).toBeDefined();

    await store.openFolder('sd');
    expect(store.filesUnread).toBe(true);
  });

  it('re-reads with refresh off after files.changed and asks nothing more', async () => {
    const { store, files, emit } = watched((request) =>
      Promise.resolve(
        request.query?.refresh === true
          ? listingOf('usb', [a], held)
          : listingOf('usb', [a, b], { stale: true }),
      ),
    );
    await store.openFolder('usb');

    emit(filesChanged('mk4-01', 'usb', false));

    await vi.waitFor(() => {
      expect(store.entries).toHaveLength(2);
    });
    expect(files).toHaveBeenCalledTimes(2);
    expect(files.mock.calls[1]?.[0].query?.refresh).toBeUndefined();
    // A loop would ask again within any window; this one spans timers too.
    await new Promise((resolve) => setTimeout(resolve, 50));
    expect(files).toHaveBeenCalledTimes(2);
    expect(store.filesReadingSince).toBeUndefined();
  });

  it('ignores files.changed for another folder or another printer', async () => {
    const { store, files, emit } = watched(() =>
      Promise.resolve(listingOf('usb', [a], held)),
    );
    await store.openFolder('usb');

    emit(filesChanged('mk4-01', 'usb/models', false));
    emit(filesChanged('xl-02', 'usb', false));
    emit(filesChanged('mk4-01', 'usb', true));

    expect(files).toHaveBeenCalledTimes(1);
  });

  // The page can see the landed event before the answer to the ask that
  // started that read, and that answer still says reading.
  it('re-reads when the read lands before the answer to its ask', async () => {
    let answerFirst: (listing: DeviceListing) => void = () => undefined;
    const { store, files, emit } = watched((request) =>
      request.query?.refresh === true
        ? new Promise<DeviceListing>((resolve) => {
            answerFirst = resolve;
          })
        : Promise.resolve(listingOf('usb', [a, b], { stale: true })),
    );
    const opened = store.openFolder('usb');

    emit(filesChanged('mk4-01', 'usb', false));
    answerFirst(listingOf('usb', [a], held));
    await opened;

    await vi.waitFor(() => {
      expect(store.entries).toHaveLength(2);
    });
    expect(files.mock.calls.at(-1)?.[0].query?.refresh).toBeUndefined();
    expect(store.filesReadingSince).toBeUndefined();
    expect(store.readingDrive).toBe(false);
  });

  it('shows the fault a failed read left and retries with refresh', async () => {
    const { store, files } = watched(() =>
      Promise.resolve(
        listingOf('usb', [], {
          stale: true,
          fault: {
            code: 'core.printer/LISTING_REFUSED',
            title: 'The printer refused to list the folder',
            params: {},
            retryable: true,
          },
        }),
      ),
    );

    await store.openFolder('usb');
    expect(store.filesError).toMatchObject({
      code: 'core.printer/LISTING_REFUSED',
    });

    await store.openFolder('usb', true);
    expect(files).toHaveBeenCalledTimes(2);
    expect(files.mock.calls[1]?.[0].query?.refresh).toBe(true);
  });

  it('marks the drive as being read until the read lands', async () => {
    const { store, emit } = watched(() =>
      Promise.resolve(listingOf('usb', [a], { stale: true })),
    );
    await store.openFolder('usb');
    expect(store.readingDrive).toBe(false);

    emit(filesChanged('mk4-01', 'usb/models', true));
    expect(store.readingDrive).toBe(true);

    emit(filesChanged('mk4-01', 'usb/models', false));
    expect(store.readingDrive).toBe(false);
  });

  it('re-reads an expanded folder when its read lands', async () => {
    const firstRead = readOnce();
    const models = entryOf('models', 'usb/models', true);
    const inside = entryOf('c.bgcode', 'usb/models/c.bgcode');
    const { store, files, emit } = watched((request) => {
      const path = request.query?.path ?? '';
      if (path === 'usb') return Promise.resolve(listingOf('usb', [models]));

      return Promise.resolve(
        firstRead(path) || request.query?.refresh === true
          ? listingOf(path, [], reading)
          : listingOf(path, [inside], { stale: true }),
      );
    });
    await store.openFolder('usb');
    await store.expand('usb/models');
    expect(store.expanded['usb/models']).toBeUndefined();

    emit(filesChanged('mk4-01', 'usb/models', false));

    await vi.waitFor(() => {
      expect(namesIn(store, 'usb/models')).toEqual(['c.bgcode']);
    });
    expect(files.mock.calls.at(-1)?.[0].query).toEqual({ path: 'usb/models' });
  });

  // A gap may have swallowed the landed event of any folder the page shows
  // or waits on, so each is asked once more and nothing stays reading.
  it('reads every folder it waits on once after a gap in the stream', async () => {
    const firstRead = readOnce();
    const models = entryOf('models', 'usb/models', true);
    const { store, files, emit } = watched((request) => {
      const path = request.query?.path ?? '';
      if (path === 'usb') return Promise.resolve(listingOf('usb', [models]));

      return Promise.resolve(
        firstRead(path) || request.query?.refresh === true
          ? listingOf(path, [], reading)
          : listingOf(path, [entryOf('x.bgcode', `${path}/x.bgcode`)], {
              stale: true,
            }),
      );
    });
    await store.openFolder('usb');
    await store.expand('usb/models');
    await store.checkHeld(['/sd/x.bgcode', '/sd/y.bgcode']);
    emit(filesChanged('mk4-01', 'usb/other', true));
    expect(store.readingDrive).toBe(true);
    const before = files.mock.calls.length;

    emit(eventOf('com.prusa.connect.stream.continuity.lost', 'mk4-01'));

    await vi.waitFor(() => {
      expect(store.holds('/sd/x.bgcode')).toBe(true);
      expect(namesIn(store, 'usb/models')).toEqual(['x.bgcode']);
    });
    expect(store.readingDrive).toBe(false);
    const asked = files.mock.calls
      .slice(before)
      .map(([request]) => request.query);
    expect(asked).toEqual([
      { path: 'usb' },
      { path: 'usb/models' },
      { path: 'sd' },
    ]);
  });

  it('reads no folder after a gap when the Files tab never opened', async () => {
    const { files, emit } = watched(() => Promise.resolve(listingOf('', [])));

    emit(eventOf('com.prusa.connect.stream.continuity.lost', 'mk4-01'));
    await new Promise((resolve) => setTimeout(resolve, 50));

    expect(files).not.toHaveBeenCalled();
  });

  it('checks a print file again when its folder lands', async () => {
    const { store, emit } = watched((request) =>
      Promise.resolve(
        request.query?.refresh === true
          ? listingOf('usb', [], reading)
          : listingOf('usb', [a], { stale: true }),
      ),
    );

    await store.checkHeld(['/usb/a.bgcode'], ['/usb/a.bgcode']);
    expect(store.holds('/usb/a.bgcode')).toBeUndefined();

    emit(filesChanged('mk4-01', 'usb', false));

    await vi.waitFor(() => {
      expect(store.holds('/usb/a.bgcode')).toBe(true);
    });
  });
});
