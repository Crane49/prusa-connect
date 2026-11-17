import { describe, expect, it, vi } from 'vitest';

import {
  DeviceAdministration,
  type DeviceAdministrationClient,
} from './device-administration.svelte';
import { fakeDevice } from '../test/fakes';
import { StubApiError } from './stubs';

type Tags = readonly string[];

interface Held {
  client: DeviceAdministrationClient;
  names: string[];
  tagWrites: Tags[];
  retired: number;
  /** The tag list the server holds now. A test changes it to stand for another person. */
  server: Tags;
  /** Answers the oldest tag write once it has been sent, or refuses it. */
  answerTags(refused?: boolean): Promise<void>;
}

// A client whose tag writes wait until the test answers them. That is how
// a test makes a second edit while the first is still on its way. A taken
// write becomes the list the server holds.
const heldClient = (refuse?: StubApiError, server: Tags = []): Held => {
  const waiting: ((refused: boolean) => void)[] = [];
  const held: Held = {
    names: [],
    tagWrites: [],
    retired: 0,
    server,
    answerTags: async (refused = false) => {
      await vi.waitFor(() => {
        expect(waiting.length).toBeGreaterThan(0);
      });
      waiting.shift()?.(refused);
    },
    client: {
      getDevice: (request) =>
        Promise.resolve({
          ...fakeDevice(request.path.device_id),
          tags: [...held.server],
        }),
      setDeviceName: (request) => {
        if (refuse) return Promise.reject(refuse);
        held.names.push(request.body.name);

        return Promise.resolve({ name: request.body.name || 'MK4S serial' });
      },
      setDeviceTags: (request) => {
        if (refuse) return Promise.reject(refuse);
        const tags = [...(request.body.tags ?? [])];
        held.tagWrites.push(tags);

        return new Promise((resolve, reject) => {
          waiting.push((refused) => {
            if (refused) {
              reject(new StubApiError('core.validation/TAG_TOO_LONG', 422));
            } else {
              held.server = tags;
              resolve({ tags });
            }
          });
        });
      },
      retireDevice: () => {
        if (refuse) return Promise.reject(refuse);
        held.retired += 1;

        return Promise.resolve();
      },
    },
  };

  return held;
};

describe('DeviceAdministration', () => {
  it('starts with nothing pending and no fault', () => {
    const { client } = heldClient();
    const store = new DeviceAdministration(client, 'printer-1');

    expect(store.pending('rename')).toBe(false);
    expect(store.pending('tags')).toBe(false);
    expect(store.pending('retire')).toBe(false);
    expect(store.error).toBeUndefined();
    expect(store.tags(['workshop'])).toEqual(['workshop']);
  });

  it('renames and reports the answer only once the server has it', async () => {
    const held = heldClient();
    let reread = 0;
    const store = new DeviceAdministration(held.client, 'printer-1', {
      afterWrite: () => {
        reread += 1;

        return Promise.resolve();
      },
    });

    const flight = store.rename('Workshop bench');
    expect(store.pending('rename')).toBe(true);

    await expect(flight).resolves.toBe(true);
    expect(held.names).toEqual(['Workshop bench']);
    expect(reread).toBe(1);
    expect(store.pending('rename')).toBe(false);
  });

  // The server takes an empty name off and the machine reads under its
  // model and serial again.
  it('sends an empty name as it is', async () => {
    const held = heldClient();
    const store = new DeviceAdministration(held.client, 'printer-1');

    await store.rename('');

    expect(held.names).toEqual(['']);
  });

  it('keeps an error by its code and says which act it stopped', async () => {
    const held = heldClient(new StubApiError('core.authz/FORBIDDEN', 403));
    const store = new DeviceAdministration(held.client, 'printer-1');

    await expect(store.retire()).resolves.toBe(false);

    expect(store.error?.code).toBe('core.authz/FORBIDDEN');
    expect(store.failed).toBe('retire');
    expect(store.pending('retire')).toBe(false);
  });

  // Two quick edits: the second is built on the first, not on the record
  // the page drew before either was answered.
  it('builds a second tag edit on the first while the first is on its way', async () => {
    const held = heldClient(undefined, ['assembly', 'workshop 2']);
    const store = new DeviceAdministration(held.client, 'printer-1');
    const record = ['assembly', 'workshop 2'];

    const first = store.editTags((current) =>
      current.filter((tag) => tag !== 'assembly'),
    );
    const second = store.editTags((current) => [...current, 'shelf B']);

    // The edit shows at once, before the server has answered.
    expect(store.tags(record)).toEqual(['workshop 2', 'shelf B']);

    await held.answerTags();
    await first;
    await held.answerTags();
    await second;

    expect(held.tagWrites).toEqual([['workshop 2'], ['workshop 2', 'shelf B']]);
  });

  it('sends one tag write at a time', async () => {
    const held = heldClient();
    const store = new DeviceAdministration(held.client, 'printer-1');

    void store.editTags(() => ['a']);
    void store.editTags((current) => [...current, 'b']);
    await vi.waitFor(() => {
      expect(held.tagWrites).toHaveLength(1);
    });
    // The second write waits for the first answer, however long that takes.
    await new Promise((resolve) => setTimeout(resolve, 20));

    expect(held.tagWrites).toHaveLength(1);
  });

  it('keeps the tag writes pending until the last one is answered', async () => {
    const held = heldClient();
    const store = new DeviceAdministration(held.client, 'printer-1');

    const first = store.editTags(() => ['a']);
    const second = store.editTags((current) => [...current, 'b']);
    expect(store.pending('tags')).toBe(true);

    await held.answerTags();
    await first;
    expect(store.pending('tags')).toBe(true);

    await held.answerTags();
    await second;
    expect(store.pending('tags')).toBe(false);
  });

  it('goes back to the record once every tag write is answered', async () => {
    const held = heldClient();
    const store = new DeviceAdministration(held.client, 'printer-1');

    const write = store.editTags(() => ['a']);
    await held.answerTags();
    await write;

    expect(store.tags(['a', 'from the record'])).toEqual([
      'a',
      'from the record',
    ]);
  });

  // An edit made while the record is read again belongs to a new batch, and
  // the end of the old batch must not wipe it off the screen.
  it('keeps an edit made while the record is read again after a write', async () => {
    const held = heldClient();
    // Held in an object, so the compiler does not narrow it after a reset.
    const reread: { finish?: (() => void) | undefined } = {};
    const finish = (): void => {
      reread.finish?.();
    };
    const store = new DeviceAdministration(held.client, 'printer-1', {
      afterWrite: () =>
        new Promise<void>((resolve) => {
          reread.finish = resolve;
        }),
    });

    const first = store.editTags(() => ['a']);
    await held.answerTags();
    await vi.waitFor(() => {
      expect(reread.finish).toBeDefined();
    });

    const second = store.editTags((current) => [...current, 'b']);
    finish();
    await first;

    expect(store.tags(['a'])).toEqual(['a', 'b']);
    reread.finish = undefined;
    await held.answerTags();
    await vi.waitFor(() => {
      expect(reread.finish).toBeDefined();
    });
    finish();
    await second;
  });

  // A write taken in the middle is on the server even when the writes around
  // it were refused, so the record is read again.
  it('reads the record again when only a write in the middle is taken', async () => {
    const held = heldClient(undefined, ['a']);
    let rereads = 0;
    const store = new DeviceAdministration(held.client, 'printer-1', {
      afterWrite: () => {
        rereads += 1;

        return Promise.resolve();
      },
    });

    const writes = [
      store.editTags(() => []),
      store.editTags((current) => [...current, 'b']),
      store.editTags((current) => [...current, 'c']),
    ];
    // The first and last writes are refused and only the middle one is
    // taken, so the reread depends on the batch and not on its last answer.
    await held.answerTags(true);
    await held.answerTags();
    await held.answerTags(true);
    await Promise.all(writes);

    expect(rereads).toBe(1);
    expect(store.failed).toBe('tags');
    expect(store.error?.code).toBe('core.validation/TAG_TOO_LONG');
  });

  // Remove a tag, then add one the server refuses. The removal happened, so
  // the record is read again and the old list is not drawn.
  it('reads the record again when only the last write is refused', async () => {
    const held = heldClient(undefined, ['a']);
    let rereads = 0;
    const store = new DeviceAdministration(held.client, 'printer-1', {
      afterWrite: () => {
        rereads += 1;

        return Promise.resolve();
      },
    });

    const writes = [
      store.editTags(() => []),
      store.editTags((current) => [...current, 'b,c']),
    ];
    await held.answerTags();
    await held.answerTags(true);
    await Promise.all(writes);

    expect(rereads).toBe(1);
  });

  it('reads nothing again when every write is refused', async () => {
    const held = heldClient(undefined, ['a']);
    let rereads = 0;
    const store = new DeviceAdministration(held.client, 'printer-1', {
      afterWrite: () => {
        rereads += 1;

        return Promise.resolve();
      },
    });

    const write = store.editTags(() => []);
    await held.answerTags(true);
    await write;

    expect(rereads).toBe(0);
    expect(store.tags(['a'])).toEqual(['a']);
  });

  // Another person added a tag after this page read the record. The write
  // is built on the list the server holds now, so their tag stays.
  it('keeps a tag another person added after the page read the record', async () => {
    const held = heldClient(undefined, ['a']);
    const store = new DeviceAdministration(held.client, 'printer-1');

    // The page drew ['a']. Another person adds a tag before this edit.
    held.server = ['a', 'from elsewhere'];
    const write = store.editTags((current) => [...current, 'b']);
    await held.answerTags();
    await write;

    expect(held.tagWrites).toEqual([['a', 'from elsewhere', 'b']]);
  });

  it('removes a tag from the list the server holds now', async () => {
    const held = heldClient(undefined, ['a', 'b']);
    const store = new DeviceAdministration(held.client, 'printer-1');

    held.server = ['a', 'b', 'c'];
    const write = store.editTags((current) =>
      current.filter((tag) => tag !== 'a'),
    );
    await held.answerTags();
    await write;

    expect(held.tagWrites).toEqual([['b', 'c']]);
  });

  // The server refused the first add. The edit queued behind it is built on
  // the last list the server took, so it does not carry the refused tag.
  it('builds an edit queued behind a refused add on the last taken list', async () => {
    const held = heldClient(undefined, ['x']);
    const store = new DeviceAdministration(held.client, 'printer-1');

    const refused = store.editTags((current) => [...current, 'bad,tag']);
    const taken = store.editTags((current) => [...current, 'b']);
    // The list shown carries both edits while they wait.
    expect(store.tags(['x'])).toEqual(['x', 'bad,tag', 'b']);

    await held.answerTags(true);
    await expect(refused).resolves.toBe(false);
    // Once the add is refused, the list shown drops it.
    expect(store.tags(['x'])).toEqual(['x', 'b']);

    await held.answerTags();
    await expect(taken).resolves.toBe(true);

    expect(held.tagWrites).toEqual([
      ['x', 'bad,tag'],
      ['x', 'b'],
    ]);
  });

  // A tag write that ends must not report a rename that is still waiting
  // as finished.
  it('keeps a rename pending while a tag write ends beside it', async () => {
    const held = heldClient();
    let answerRename: () => void = () => undefined;
    const client = {
      ...held.client,
      setDeviceName: () =>
        new Promise<{ name: string }>((resolve) => {
          answerRename = () => {
            resolve({ name: 'x' });
          };
        }),
    };
    const store = new DeviceAdministration(client, 'printer-1');

    const rename = store.rename('x');
    const tag = store.editTags(() => ['a']);
    await held.answerTags();
    await tag;

    expect(store.pending('rename')).toBe(true);
    expect(store.pending('tags')).toBe(false);
    answerRename();
    await rename;
    expect(store.pending('rename')).toBe(false);
  });
});
