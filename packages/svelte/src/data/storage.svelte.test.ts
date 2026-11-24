import { describe, expect, it } from 'vitest';

import {
  type ServerStorage,
  type StorageClient,
  type StorageEntry,
  UPLOAD_LIMIT_BYTES,
  UPLOAD_TOO_LARGE,
} from './services/storage';
import { createStorage, Storage } from './storage.svelte';
import { StubApiError } from './stubs';

const storageOf = (name: string, write = true): ServerStorage => ({
  allowed_extensions: [],
  browse: true,
  display_name: name,
  download_url: true,
  move: write,
  name,
  ranges: true,
  write,
});

const fileOf = (name: string, path: string, size = 1024): StorageEntry => ({
  folder: false,
  modified: '2025-09-04T15:33:20Z',
  name,
  path,
  size,
});

/**
 * A file of a stated length. Allocating four gigabytes to test the limit
 * would be four gigabytes, and the store reads `size` and hands the rest to
 * the client, which the stub below never looks at.
 */
// The cast stands in for a file this test cannot allocate.
const sized = (size: number): Blob => ({ size }) as unknown as Blob;

const folderOf = (name: string, path: string): StorageEntry => ({
  folder: true,
  modified: '2025-09-04T15:33:20Z',
  name,
  path,
  size: 0,
});

interface Calls {
  storages: number;
  listed: string[];
  uploaded: string[];
  deleted: string[];
  moved: string[];
  stated: string[];
}

/** An upload a test holds open, to report progress or to wait for a cancel. */
type Hold = (
  request: { onProgress?: (sent: number, total: number) => void },
  signal: AbortSignal | undefined,
) => Promise<{ bytes: number; path: string }>;

const stubClient = (
  storages: readonly ServerStorage[],
  tree: Readonly<Record<string, readonly StorageEntry[]>>,
  refuse?: StubApiError,
  hold?: Hold,
): { client: StorageClient; calls: Calls } => {
  const calls: Calls = {
    storages: 0,
    listed: [],
    uploaded: [],
    deleted: [],
    moved: [],
    stated: [],
  };
  const held: Record<string, StorageEntry[]> = {};
  for (const [key, rows] of Object.entries(tree)) held[key] = [...rows];

  const client: StorageClient = {
    url: 'https://printer.example',
    statStorageEntry: (request) => {
      const path = request.query.path;
      calls.stated.push(`${request.path.storage}:${path}`);

      if (path.endsWith('.bgcode')) {
        return Promise.resolve({
          folder: false,
          modified: '2026-09-22T00:00:00Z',
          name: path,
          path,
          size: 12_526,
          print: {
            estimated_seconds: 136,
            filament_g: 0.4,
            material: 'PLA',
            printer_model: 'MK4IS',
            thumbnail_height: 173,
            thumbnail_width: 313,
          },
        });
      }

      return Promise.resolve({
        folder: false,
        modified: '2026-09-22T00:00:00Z',
        name: path,
        path,
        size: 12,
      });
    },
    listStorages: () => {
      calls.storages += 1;

      return Promise.resolve({ items: [...storages] });
    },
    listStorageFiles: (request) => {
      const at = `${request.path.storage}:${request.query?.path ?? ''}`;
      calls.listed.push(at);

      return Promise.resolve({ items: [...(held[at] ?? [])] });
    },
    uploadStorageObject: (request, signal) => {
      if (refuse) throw refuse;
      calls.uploaded.push(request.query.path);
      if (hold !== undefined) return hold(request, signal);
      const at = `${request.path.storage}:`;
      const rows = held[at] ?? [];
      rows.push(fileOf(request.query.path, request.query.path, 7));
      held[at] = rows;

      return Promise.resolve({ bytes: 7, path: request.query.path });
    },
    createStorageFolder: () => Promise.resolve({ path: '' }),
    moveStorageEntry: (request) => {
      const at = `${request.path.storage}:`;
      calls.moved.push(`${request.body.from}->${request.body.to}`);
      held[at] = (held[at] ?? []).filter(
        (row) => row.path !== request.body.from,
      );

      return Promise.resolve({ path: request.body.to });
    },
    deleteStorageEntry: (request) => {
      const at = `${request.path.storage}:`;
      calls.deleted.push(request.query.path);
      held[at] = (held[at] ?? []).filter(
        (row) => row.path !== request.query.path,
      );

      return Promise.resolve();
    },
  };

  return { client, calls };
};

describe('Storage', () => {
  it('starts unread, which is not the same as a server with no storage', () => {
    const { client } = stubClient([], {});
    const store = createStorage(client);

    expect(store.storages).toEqual([]);
    expect(store.loaded).toBe(false);
    expect(store.filesLoaded).toBe(false);
  });

  it('reads the storages and says which of them a person may write to', async () => {
    const { client } = stubClient(
      [storageOf('local'), storageOf('usb', false)],
      {},
    );
    const store = createStorage(client);

    await store.load();

    expect(store.loaded).toBe(true);
    expect(store.writable.map((row) => row.name)).toEqual(['local']);
  });

  // svelte-data.md: two readers of the same list cost one request.
  it('costs one request however many readers ask at once', async () => {
    const { client, calls } = stubClient([storageOf('local')], {});
    const store = createStorage(client);

    await Promise.all([store.load(), store.load()]);

    expect(calls.storages).toBe(1);
  });

  it('lists one directory and separates the folders from the files', async () => {
    const { client } = stubClient([storageOf('local')], {
      'local:': [
        folderOf('models', 'models'),
        fileOf('cube.bgcode', 'cube.bgcode'),
      ],
    });
    const store = createStorage(client);

    await store.open('local');

    expect(store.storage).toBe('local');
    expect(store.directory).toBe('');
    expect(store.folders.map((row) => row.name)).toEqual(['models']);
    expect(store.files.map((row) => row.name)).toEqual(['cube.bgcode']);
  });

  it('reports a refused listing by its code and never as a sentence', async () => {
    const { client } = stubClient([], {});
    const store = createStorage(client);
    const refusing: StorageClient = {
      ...client,
      listStorageFiles: () => {
        throw new StubApiError('core.auth/ACCESS_DENIED', 403);
      },
    };

    const refused = createStorage(refusing);
    await refused.open('local');

    expect(refused.filesError?.code).toBe('core.auth/ACCESS_DENIED');
    expect(refused.filesError?.status).toBe(403);
    expect(store.filesError).toBeUndefined();
  });

  it('writes a file, keeps the upload row and lists the directory again', async () => {
    const { client, calls } = stubClient([storageOf('local')], {
      'local:': [],
    });
    const store = createStorage(client);
    await store.open('local');

    const saved = await store.upload(
      'local',
      'cube.bgcode',
      new Blob(['1234567']),
    );

    expect(saved?.bytes).toBe(7);
    expect(calls.uploaded).toEqual(['cube.bgcode']);
    expect(store.uploads).toHaveLength(1);
    expect(store.uploads[0]?.state).toBe('done');
    expect(store.files.map((row) => row.name)).toEqual(['cube.bgcode']);
  });

  // A refusal arrives with nothing sent, because the stub throws before the
  // request goes out.
  it('keeps a failed upload with its fault and no bytes sent', async () => {
    const { client } = stubClient(
      [storageOf('local')],
      { 'local:': [] },
      new StubApiError('core.storage/WRITE_REFUSED', 507),
    );
    const store = createStorage(client);

    await store.upload('local', 'cube.bgcode', new Blob(['1234567']));

    expect(store.uploads[0]?.state).toBe('failed');
    expect(store.uploads[0]?.sent).toBe(0);
    expect(store.uploads[0]?.total).toBe(7);
    expect(store.error?.code).toBe('core.storage/WRITE_REFUSED');
  });

  it('counts the bytes sent while an upload goes up', async () => {
    let report: ((sent: number, total: number) => void) | undefined;
    let finish: (() => void) | undefined;
    const { client } = stubClient(
      [storageOf('local')],
      { 'local:': [] },
      undefined,
      (request) => {
        report = request.onProgress;
        return new Promise((resolve) => {
          finish = () => {
            resolve({ bytes: 7, path: 'cube.bgcode' });
          };
        });
      },
    );
    const store = createStorage(client);

    const saved = store.upload('local', 'cube.bgcode', new Blob(['1234567']));
    report?.(3, 7);

    expect(store.uploads[0]?.state).toBe('sending');
    expect(store.uploads[0]?.sent).toBe(3);

    finish?.();
    await saved;
    expect(store.uploads[0]?.sent).toBe(7);
  });

  it('cancels an upload on its way, and records it as cancelled rather than failed', async () => {
    const { client } = stubClient(
      [storageOf('local')],
      { 'local:': [] },
      undefined,
      (_request, signal) =>
        new Promise((_resolve, reject) => {
          signal?.addEventListener('abort', () => {
            reject(new DOMException('aborted', 'AbortError'));
          });
        }),
    );
    const store = createStorage(client);

    const saved = store.upload('local', 'cube.bgcode', new Blob(['1234567']));
    store.cancelUpload('local', 'cube.bgcode');

    expect(await saved).toBeUndefined();
    expect(store.uploads[0]?.state).toBe('cancelled');
    expect(store.uploads[0]?.fault).toBeUndefined();
    expect(store.error).toBeUndefined();
  });

  // A second upload of the same file aborts the first, which then rejects on
  // a later microtask, after the second has already started. The first's
  // rejection must not overwrite the row the second is still writing.
  it('keeps the row of the newer upload when the older one for the same file settles late', async () => {
    let call = 0;
    let resolveSecond: (() => void) | undefined;
    const { client } = stubClient(
      [storageOf('local')],
      { 'local:': [] },
      undefined,
      (_request, signal) => {
        call += 1;
        if (call === 1) {
          return new Promise((_resolve, reject) => {
            signal?.addEventListener('abort', () => {
              void Promise.resolve().then(() => {
                reject(new DOMException('aborted', 'AbortError'));
              });
            });
          });
        }

        return new Promise((resolve) => {
          resolveSecond = () => {
            resolve({ bytes: 7, path: 'cube.bgcode' });
          };
        });
      },
    );
    const store = createStorage(client);

    const first = store.upload('local', 'cube.bgcode', new Blob(['1234567']));
    const second = store.upload('local', 'cube.bgcode', new Blob(['12']));

    expect(await first).toBeUndefined();
    expect(store.uploads[0]?.state).toBe('sending');
    expect(store.uploads[0]?.total).toBe(2);

    resolveSecond?.();
    await second;

    expect(store.uploads[0]?.state).toBe('done');
    expect(store.uploads[0]?.total).toBe(2);
  });

  it('aborts an upload on its way when the person changes', async () => {
    let aborted = false;
    const { client } = stubClient(
      [storageOf('local')],
      { 'local:': [] },
      undefined,
      (_request, signal) =>
        new Promise((_resolve, reject) => {
          signal?.addEventListener('abort', () => {
            aborted = true;
            reject(new DOMException('aborted', 'AbortError'));
          });
        }),
    );
    const store = createStorage(client);

    const saved = store.upload('local', 'cube.bgcode', new Blob(['1234567']));
    store.reset();

    expect(await saved).toBeUndefined();
    expect(aborted).toBe(true);
    expect(store.uploads).toEqual([]);
  });

  // A file over the route's bound is refused before the request. Sending it
  // would push four gigabytes over the network and be refused at the end of
  // it, and the person would wait out the whole upload to read that.
  it('refuses a file over the upload limit without sending it', async () => {
    const { client, calls } = stubClient([storageOf('local')], {
      'local:': [],
    });
    const store = createStorage(client, () => UPLOAD_LIMIT_BYTES);

    const saved = await store.upload(
      'local',
      'huge.bgcode',
      sized(UPLOAD_LIMIT_BYTES + 1),
    );

    expect(saved).toBeUndefined();
    expect(calls.uploaded).toEqual([]);
    expect(store.uploads[0]?.state).toBe('failed');
    expect(store.uploads[0]?.fault?.code).toBe(UPLOAD_TOO_LARGE);
    expect(store.uploads[0]?.fault?.params.limit_bytes).toBe(
      UPLOAD_LIMIT_BYTES,
    );
    expect(store.error?.code).toBe(UPLOAD_TOO_LARGE);
  });

  // Before the server's settings are read the limit is not known, and a
  // server raised past the default would take the file, so it is sent and
  // the server decides.
  it('sends a file of any size while the limit is not known', async () => {
    const { client, calls } = stubClient([storageOf('local')], {
      'local:': [],
    });
    const store = new Storage(client, () => undefined);

    await store.upload('local', 'big.bgcode', sized(UPLOAD_LIMIT_BYTES + 1));

    expect(calls.uploaded).toEqual(['big.bgcode']);
  });

  // The server's own limit wins over the default, so a server that raised
  // core.api.max_upload_size takes a file the default would refuse.
  it('holds a file to the limit the server names', async () => {
    const { client, calls } = stubClient([storageOf('local')], {
      'local:': [],
    });
    const raised = UPLOAD_LIMIT_BYTES * 2;
    const store = new Storage(client, () => raised);

    await store.upload('local', 'big.bgcode', sized(UPLOAD_LIMIT_BYTES + 1));
    expect(calls.uploaded).toHaveLength(1);

    await store.upload('local', 'huge.bgcode', sized(raised + 1));
    expect(store.uploads.at(-1)?.fault?.params.limit_bytes).toBe(raised);
    expect(calls.uploaded).toHaveLength(1);
  });

  it('sends a file of exactly the upload limit', async () => {
    const { client, calls } = stubClient([storageOf('local')], {
      'local:': [],
    });
    const store = new Storage(client, () => UPLOAD_LIMIT_BYTES);

    await store.upload('local', 'wide.bgcode', sized(UPLOAD_LIMIT_BYTES));

    expect(calls.uploaded).toEqual(['wide.bgcode']);
  });

  it('writes a temporary copy, answers where the server put it, and leaves the uploads and the listing alone', async () => {
    let asked: { query?: unknown } | undefined;
    let report: ((sent: number, total: number) => void) | undefined;
    const { client, calls } = stubClient(
      [storageOf('local')],
      { 'local:': [] },
      undefined,
      (request) => {
        asked = request as { query?: unknown };
        report = request.onProgress;

        return Promise.resolve({
          bytes: 7,
          path: '.pcl-incoming/1761300000-a/cube.bgcode',
        });
      },
    );
    const store = createStorage(client);
    await store.open('local');
    const listed = calls.listed.length;
    const seen: number[] = [];

    const answer = await store.uploadTemporary(
      'local',
      'cube.bgcode',
      new Blob(['1234567']),
      (sent) => seen.push(sent),
    );
    report?.(7, 7);

    expect(asked?.query).toEqual({ path: 'cube.bgcode', temporary: true });
    expect(answer).toEqual({
      saved: { bytes: 7, path: '.pcl-incoming/1761300000-a/cube.bgcode' },
    });
    expect(seen).toEqual([7]);
    expect(store.uploads).toEqual([]);
    expect(calls.listed.length).toBe(listed);
  });

  it('answers a refused temporary copy with its fault and leaves the page error alone', async () => {
    const { client } = stubClient(
      [storageOf('local')],
      { 'local:': [] },
      new StubApiError('core.storage/WRITE_REFUSED', 507),
    );
    const store = createStorage(client);

    const answer = await store.uploadTemporary(
      'local',
      'cube.bgcode',
      new Blob(['1234567']),
    );

    expect('fault' in answer && answer.fault.code).toBe(
      'core.storage/WRITE_REFUSED',
    );
    expect(store.error).toBeUndefined();
    expect(store.uploads).toEqual([]);
  });

  it('refuses a temporary copy over the upload limit without sending it', async () => {
    const { client, calls } = stubClient([storageOf('local')], {
      'local:': [],
    });
    const store = new Storage(client, () => UPLOAD_LIMIT_BYTES);

    const answer = await store.uploadTemporary(
      'local',
      'huge.bgcode',
      sized(UPLOAD_LIMIT_BYTES + 1),
    );

    expect('fault' in answer && answer.fault.code).toBe(UPLOAD_TOO_LARGE);
    expect(calls.uploaded).toEqual([]);
  });

  it('removes a file and lists the directory again', async () => {
    const { client, calls } = stubClient([storageOf('local')], {
      'local:': [fileOf('cube.bgcode', 'cube.bgcode')],
    });
    const store = createStorage(client);
    await store.open('local');

    await store.remove('local', 'cube.bgcode');

    expect(calls.deleted).toEqual(['cube.bgcode']);
    expect(store.files).toEqual([]);
  });

  it('moves each entry and lists the directory once at the end', async () => {
    const { client, calls } = stubClient([storageOf('local')], {
      'local:': [
        fileOf('cube.bgcode', 'cube.bgcode'),
        fileOf('gear.bgcode', 'gear.bgcode'),
      ],
    });
    const store = createStorage(client);
    await store.open('local');

    const results = await store.moveAll('local', [
      { from: 'cube.bgcode', to: 'models/cube.bgcode' },
      { from: 'gear.bgcode', to: 'models/gear.bgcode' },
    ]);

    expect(calls.moved).toEqual([
      'cube.bgcode->models/cube.bgcode',
      'gear.bgcode->models/gear.bgcode',
    ]);
    expect(results.map((row) => row.fault)).toEqual([undefined, undefined]);
    expect(calls.listed).toEqual(['local:', 'local:']);
    expect(store.files).toEqual([]);
  });

  // A refused move followed by one that went through is still a refusal, and
  // the reader is told which entry stayed where it was.
  it('reports each entry and keeps the refusal a later move would hide', async () => {
    const { client } = stubClient([storageOf('local')], {
      'local:': [
        fileOf('cube.bgcode', 'cube.bgcode'),
        fileOf('gear.bgcode', 'gear.bgcode'),
      ],
    });
    const refusing: StorageClient = {
      ...client,
      createStorageFolder: () => Promise.resolve({ path: '' }),
      moveStorageEntry: (request) =>
        request.body.from === 'cube.bgcode'
          ? Promise.reject(new StubApiError('core.storage/EXISTS', 409))
          : client.moveStorageEntry(request),
    };
    const store = createStorage(refusing);
    await store.open('local');

    const results = await store.moveAll('local', [
      { from: 'cube.bgcode', to: 'models/cube.bgcode' },
      { from: 'gear.bgcode', to: 'models/gear.bgcode' },
    ]);

    expect(results.map((row) => [row.from, row.fault?.code])).toEqual([
      ['cube.bgcode', 'core.storage/EXISTS'],
      ['gear.bgcode', undefined],
    ]);
    expect(store.error?.code).toBe('core.storage/EXISTS');
  });

  // What a destination picker walks: another directory, read while the listing
  // behind it goes on showing the one a person opened.
  it('walks the folders of another directory and keeps the open one', async () => {
    const { client } = stubClient([storageOf('local')], {
      'local:': [fileOf('cube.bgcode', 'cube.bgcode')],
      'local:models': [
        folderOf('spares', 'models/spares'),
        fileOf('hinge.bgcode', 'models/hinge.bgcode'),
      ],
    });
    const store = createStorage(client);
    await store.open('local');

    await store.browseFolders('local', 'models');

    expect(store.pickerPath).toBe('models');
    expect(store.pickerFolders.map((row) => row.path)).toEqual([
      'models/spares',
    ]);
    expect(store.directory).toBe('');
    expect(store.files.map((row) => row.path)).toEqual(['cube.bgcode']);
  });

  // A reader who steps on before an answer arrives is shown the folder they
  // are in, and not the one behind it.
  it('drops the folders of a step the picker has already left', async () => {
    const answer = new Map<string, (rows: readonly StorageEntry[]) => void>();
    const { client } = stubClient([storageOf('local')], {});
    const slow: StorageClient = {
      ...client,
      listStorageFiles: (request) =>
        new Promise((resolve) => {
          answer.set(request.query?.path ?? '', (rows) => {
            resolve({ items: [...rows] });
          });
        }),
    };
    const store = createStorage(slow);

    const first = store.browseFolders('local', 'models');
    const second = store.browseFolders('local', 'plates');

    answer.get('plates')?.([folderOf('small', 'plates/small')]);
    await second;
    answer.get('models')?.([folderOf('old', 'models/old')]);
    await first;

    expect(store.pickerPath).toBe('plates');
    expect(store.pickerFolders.map((row) => row.path)).toEqual([
      'plates/small',
    ]);
  });

  // A person clicking down a tree leaves a request behind on every click. An
  // answer nobody dropped would overwrite the rows of the folder they are in.
  it('keeps the open folder when the folder before it answers late', async () => {
    const answer = new Map<string, (rows: readonly StorageEntry[]) => void>();
    const { client } = stubClient([storageOf('local')], {});
    const slow: StorageClient = {
      ...client,
      listStorageFiles: (request) =>
        new Promise((resolve) => {
          answer.set(request.query?.path ?? '', (rows) => {
            resolve({ items: [...rows] });
          });
        }),
    };
    const store = createStorage(slow);

    const first = store.open('local', 'models');
    const second = store.open('local', 'plates');

    answer.get('plates')?.([fileOf('plate.bgcode', 'plates/plate.bgcode')]);
    await second;
    answer.get('models')?.([fileOf('old.bgcode', 'models/old.bgcode')]);
    await first;

    expect(store.directory).toBe('plates');
    expect(store.files.map((row) => row.name)).toEqual(['plate.bgcode']);
    expect(store.filesError).toBeUndefined();
    expect(store.filesLoading).toBe(false);
  });

  it('drops the finished uploads when a sheet reopens', async () => {
    const { client } = stubClient([storageOf('local')], { 'local:': [] });
    const store = createStorage(client);
    await store.upload('local', 'cube.bgcode', new Blob(['1234567']));

    store.clearUploads();

    expect(store.uploads).toEqual([]);
  });

  it('describes a file with what the slicer said and where its picture is', async () => {
    const { client, calls } = stubClient([storageOf('local')], {
      'local:': [],
    });
    const store = createStorage(client);

    await store.describe('local', 'models/cube.bgcode');

    const described = store.describedFile('local', 'models/cube.bgcode');
    expect(described?.details?.estimated_seconds).toBe(136);
    expect(described?.details?.material).toBe('PLA');
    expect(described?.thumbnailUrl).toBe(
      'https://printer.example/api/v1/storages/local/thumbnail' +
        '?path=models%2Fcube.bgcode',
    );

    // A file already described costs no second request, so a list that
    // rerenders does not ask the server to open every file again.
    await store.describe('local', 'models/cube.bgcode');
    expect(calls.stated).toHaveLength(1);
  });

  it('offers no picture for a file that carries none', async () => {
    const { client } = stubClient([storageOf('local')], { 'local:': [] });
    const store = createStorage(client);

    await store.describe('local', 'notes.txt');

    const described = store.describedFile('local', 'notes.txt');
    expect(described?.details).toBeUndefined();
    expect(described?.thumbnailUrl).toBeUndefined();
  });

  // The file page asks for the folder the file sits in, so the listing and the
  // page agree on which storage holds it.
  it('opens the folder that holds a file and describes the file', async () => {
    const { client, calls } = stubClient(
      [storageOf('local'), storageOf('usb')],
      {
        'usb:models': [fileOf('cube.bgcode', 'models/cube.bgcode')],
      },
    );
    const store = createStorage(client);

    await store.openFile('usb', 'models/cube.bgcode');

    expect(calls.listed).toEqual(['usb:models']);
    expect(calls.stated).toEqual(['usb:models/cube.bgcode']);
    expect(store.storage).toBe('usb');
    expect(store.directory).toBe('models');
    expect(store.describedFile('usb', 'models/cube.bgcode')).toBeDefined();
  });

  // The storage page and the file page may read the same file at once. When
  // the page that started the read stops it, the other page reads again.
  describe('a file both pages asked for', () => {
    /** A client whose file reads wait until the test answers them. */
    const waiting = () => {
      const reads: { signal: AbortSignal | undefined; answer: () => void }[] =
        [];
      const { client } = stubClient([storageOf('local')], { 'local:': [] });
      const slow: StorageClient = {
        ...client,
        statStorageEntry: (request, signal) =>
          new Promise((resolve, reject) => {
            signal?.addEventListener('abort', () => {
              reject(new Error('the read was stopped'));
            });
            reads.push({
              signal,
              answer: () => {
                void client.statStorageEntry(request).then(resolve);
              },
            });
          }),
      };
      const answerLive = async () => {
        // A retry starts after the stopped read settles, so this waits for it.
        await expect
          .poll(() => reads.some((read) => read.signal?.aborted !== true))
          .toBe(true);
        for (const read of reads) {
          if (read.signal?.aborted !== true) read.answer();
        }
      };

      return { store: createStorage(slow), reads, answerLive };
    };

    it('is read again when the storage page stops its read', async () => {
      const { store, answerLive } = waiting();
      const page = new AbortController();

      const listing = store.describe('local', 'cube.bgcode', page.signal);
      const opening = store.openFile('local', 'cube.bgcode');
      page.abort();
      await answerLive();
      await Promise.allSettled([listing, opening]);

      expect(store.describedFile('local', 'cube.bgcode')).toBeDefined();
    });

    it('is read again when the file page stops its read', async () => {
      const { store, answerLive } = waiting();

      const opening = store.openFile('local', 'cube.bgcode');
      const listing = store.describe(
        'local',
        'cube.bgcode',
        new AbortController().signal,
      );
      store.leaveFile();
      await answerLive();
      await Promise.allSettled([listing, opening]);

      expect(store.describedFile('local', 'cube.bgcode')).toBeDefined();
    });

    // A stopped read clears its own mark and not the mark of the read that
    // replaced it, so a third reader joins the second read.
    it('clears only the mark of the read that ended', async () => {
      const { store, reads, answerLive } = waiting();
      const page = new AbortController();

      const listing = store.describe('local', 'cube.bgcode', page.signal);
      const opening = store.openFile('local', 'cube.bgcode');
      page.abort();
      await expect
        .poll(() => reads.filter((read) => read.signal?.aborted !== true))
        .toHaveLength(1);
      const third = store.describe('local', 'cube.bgcode');
      await answerLive();
      await Promise.allSettled([listing, opening, third]);

      expect(reads).toHaveLength(2);
      expect(store.describedFile('local', 'cube.bgcode')).toBeDefined();
    });
  });

  // A reader who opens another file, or leaves, stops the read the page left
  // behind, and a stopped read records nothing.
  it('stops the file read a reader turned away from', () => {
    const signals: (AbortSignal | undefined)[] = [];
    const { client } = stubClient([storageOf('local')], {});
    const slow: StorageClient = {
      ...client,
      statStorageEntry: (_request, signal) => {
        signals.push(signal);

        return new Promise(() => undefined);
      },
    };
    const store = createStorage(slow);

    void store.openFile('local', 'a.bgcode');
    void store.openFile('local', 'b.bgcode');
    expect(signals[0]?.aborted).toBe(true);
    expect(signals[1]?.aborted).toBe(false);

    store.leaveFile();
    expect(signals[1]?.aborted).toBe(true);
  });
});

// A browser can sign out and sign in as somebody else without the page
// reloading. The store then holds nothing of the first person.
describe('Storage reset', () => {
  it('forgets the storages, the open folder, the files and the uploads', async () => {
    const { client } = stubClient([storageOf('local')], {
      'local:': [
        fileOf('cube.bgcode', 'cube.bgcode'),
        folderOf('parts', 'parts'),
      ],
      'local:parts': [folderOf('left', 'parts/left')],
    });
    const store = createStorage(client);
    await store.load();
    await store.open('local');
    await store.upload('local', 'notes.txt', sized(7));
    await store.describe('local', 'cube.bgcode');
    await store.browseFolders('local', 'parts');

    store.reset();

    expect(store.storages).toEqual([]);
    expect(store.loaded).toBe(false);
    expect(store.storage).toBe('');
    expect(store.directory).toBe('');
    expect(store.entries).toEqual([]);
    expect(store.files).toEqual([]);
    expect(store.filesLoaded).toBe(false);
    expect(store.uploads).toEqual([]);
    expect(store.describedFile('local', 'cube.bgcode')).toBeUndefined();
    expect(store.pickerPath).toBe('');
    expect(store.pickerFolders).toEqual([]);
    expect(store.error).toBeUndefined();
  });

  it('drops the answers that were asked for before the reset', async () => {
    const answers: (() => void)[] = [];
    const base = stubClient([storageOf('local')], {
      'local:': [fileOf('cube.bgcode', 'cube.bgcode')],
    }).client;
    // Every answer waits until the test lets it through.
    const held =
      <A extends unknown[], R>(call: (...args: A) => Promise<R>) =>
      (...args: A): Promise<R> =>
        new Promise((resolve, reject) => {
          answers.push(() => {
            call(...args).then(resolve, reject);
          });
        });
    const client: StorageClient = {
      ...base,
      listStorages: held(base.listStorages),
      listStorageFiles: held(base.listStorageFiles),
      statStorageEntry: held(base.statStorageEntry),
      uploadStorageObject: held(base.uploadStorageObject),
    };
    const store = createStorage(client);
    const reads = [
      store.load(),
      store.open('local'),
      store.describe('local', 'cube.bgcode'),
      store.upload('local', 'notes.txt', sized(7)),
    ];

    store.reset();
    for (const answer of answers) answer();
    await Promise.all(reads);

    expect(store.storages).toEqual([]);
    expect(store.loaded).toBe(false);
    expect(store.entries).toEqual([]);
    expect(store.describedFile('local', 'cube.bgcode')).toBeUndefined();
    expect(store.uploads).toEqual([]);
  });
});

describe('Storage reset during a move', () => {
  it('sends none of the moves left after a reset', async () => {
    const { client, calls } = stubClient([storageOf('local')], {
      'local:': [
        fileOf('cube.bgcode', 'cube.bgcode'),
        fileOf('gear.bgcode', 'gear.bgcode'),
      ],
    });
    let answer: (() => void) | undefined;
    const moving: StorageClient = {
      ...client,
      createStorageFolder: () => Promise.resolve({ path: '' }),
      moveStorageEntry: (...args) =>
        new Promise((resolve, reject) => {
          answer = () => {
            client.moveStorageEntry(...args).then(resolve, reject);
          };
        }),
    };
    const store = createStorage(moving);
    const move = store.moveAll('local', [
      { from: 'cube.bgcode', to: 'models/cube.bgcode' },
      { from: 'gear.bgcode', to: 'models/gear.bgcode' },
    ]);

    store.reset();
    answer?.();
    await move;

    expect(calls.moved).toEqual(['cube.bgcode->models/cube.bgcode']);
    expect(calls.listed).toEqual([]);
  });
});
