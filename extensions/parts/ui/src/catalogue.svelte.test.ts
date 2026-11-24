import { describe, expect, it } from 'vitest';

import { Catalogue } from './catalogue.svelte';
import type { FilesService } from './files';
import { TOP } from './navigation-query';
import type {
  Listing,
  Overview,
  PartDetail,
  PartQuery,
  PartsService,
} from './parts';

const OVERVIEW: Overview = {
  folders: [
    {
      v: 1,
      id: 1,
      parent: 0,
      name: 'Garage',
      parts: 1,
      folders: 1,
      created: '',
      changed: '',
      changed_by: 'user:demo',
    },
    {
      v: 1,
      id: 2,
      parent: 1,
      name: 'Shelves',
      parts: 0,
      folders: 0,
      created: '',
      changed: '',
      changed_by: 'user:demo',
    },
  ],
  root_parts: 0,
  tags: [{ tag: 'bike', parts: 1 }],
  usage: { bytes: 400, limit: 8388608 },
  storage: 'local',
  root: 'parts',
  principal: 'user:demo',
};

const PART: PartDetail = {
  v: 1,
  id: 5,
  folder: 1,
  name: 'Hook',
  tags: ['bike'],
  created: '2026-10-05T09:00:00Z',
  created_by: 'user:demo',
  changed: '2026-10-05T09:00:00Z',
  changed_by: 'user:demo',
  revision: 3,
  versions: [],
  comments: [],
  dir: 'parts/5',
  storage: 'local',
};

const LISTING: Listing = {
  parts: [
    {
      id: 5,
      folder: 1,
      name: 'Hook',
      tags: ['bike'],
      versions: 0,
      comments: 0,
      changed: '',
      changed_by: 'user:demo',
    },
  ],
  matched: 1,
  held: 1,
  principal: 'user:demo',
};

/** The services, recording every call, answering from the fixtures above. */
const fakes = (override: Partial<PartsService> = {}) => {
  const asked: { what: string; args: unknown[] }[] = [];
  const record =
    <T>(what: string, value: T) =>
    (...args: unknown[]): Promise<T> => {
      asked.push({ what, args });

      return Promise.resolve(value);
    };

  const parts: PartsService = {
    overview: record('overview', OVERVIEW),
    list: record('list', LISTING),
    part: record('part', PART),
    addFolder: record('addFolder', OVERVIEW.folders[0]!),
    changeFolder: record('changeFolder', OVERVIEW.folders[0]!),
    deleteFolder: record('deleteFolder', undefined),
    addPart: record('addPart', PART),
    changePart: record('changePart', { ...PART, revision: 4 }),
    deletePart: record('deletePart', {
      storage: 'local',
      dir: 'parts/5',
      dirs: [{ storage: 'local', dir: 'parts/5' }],
      files: [],
    }),
    addVersion: record('addVersion', PART),
    changeVersion: record('changeVersion', PART),
    deleteVersion: record('deleteVersion', {
      removed: { storage: 'local', dirs: [], files: [] },
      part: PART,
    }),
    addComment: record('addComment', {
      v: 1,
      id: 9,
      part: 5,
      body: 'nice',
      by: 'user:demo',
      created: '',
    }),
    changeComment: record('changeComment', {
      v: 1,
      id: 9,
      part: 5,
      body: 'nicer',
      by: 'user:demo',
      created: '',
    }),
    deleteComment: record('deleteComment', undefined),
    ...override,
  };

  const removed: string[] = [];
  const files: FilesService = {
    storages: record('storages', []),
    allowed: record('allowed', {
      read: true,
      write: true,
      upload: true,
      files: true,
    }),
    checkSize: record('checkSize', undefined),
    upload: record('upload', 0),
    remove: (storage, path) => {
      removed.push(`${storage}:${path}`);

      return Promise.resolve();
    },
    describe: record('describe', undefined),
    print: record('print', {
      path: '',
      ok: true,
      transferId: undefined,
      faultCode: undefined,
      machineReason: undefined,
      commandId: undefined,
    }),
    thumbnailUrl: () => '',
    contentUrl: () => '',
  };

  return {
    parts,
    files,
    asked,
    removed,
    count: (what: string) => asked.filter((row) => row.what === what).length,
  };
};

describe('Catalogue', () => {
  it('tells not read from empty', () => {
    const { parts, files } = fakes();
    const catalogue = new Catalogue(parts, files, 'user:demo');

    expect(catalogue.listed).toBe(false);
    expect(catalogue.rows).toEqual([]);
    expect(catalogue.allowed.write).toBe(false);
    expect(catalogue.overview).toBeUndefined();
  });

  it('reads the frame once and the listing once for one place', async () => {
    const fake = fakes();
    const catalogue = new Catalogue(fake.parts, fake.files, 'user:demo');

    await Promise.all([catalogue.follow(TOP), catalogue.follow(TOP)]);
    await catalogue.follow(TOP);

    expect(fake.count('overview')).toBe(1);
    expect(fake.count('allowed')).toBe(1);
    expect(fake.count('list')).toBe(1);
    expect(catalogue.listed).toBe(true);
    expect(catalogue.rows).toHaveLength(1);
    expect(catalogue.allowed.write).toBe(true);
  });

  it('lists one folder, and a search over a folder and everything under it', async () => {
    const fake = fakes();
    const catalogue = new Catalogue(fake.parts, fake.files, 'user:demo');

    await catalogue.follow({ ...TOP, folder: 1 });
    await catalogue.follow({ ...TOP, folder: 1, q: 'hook' });
    await catalogue.follow({ ...TOP, tag: 'bike' });

    const queries = fake.asked
      .filter((row) => row.what === 'list')
      .map((row) => row.args[0] as PartQuery);

    expect(queries[0]).toMatchObject({ folder: 1 });
    expect(queries[0]?.deep).toBeUndefined();
    expect(queries[1]).toMatchObject({ folder: 1, deep: true, q: 'hook' });
    expect(queries[2]?.folder).toBeUndefined();
    expect(queries[2]).toMatchObject({ tag: 'bike' });
  });

  it('opens a part and drops the listing read while one is open', async () => {
    const fake = fakes();
    const catalogue = new Catalogue(fake.parts, fake.files, 'user:demo');

    await catalogue.follow({ ...TOP, folder: 1, part: 5 });

    expect(catalogue.detail?.name).toBe('Hook');
    expect(fake.count('list')).toBe(0);
  });

  it('keeps a part it could not read as a fault, and no part', async () => {
    const thrown = new Error('gone');
    const fake = fakes({ part: () => Promise.reject(thrown) });
    const catalogue = new Catalogue(fake.parts, fake.files, 'user:demo');

    await catalogue.follow({ ...TOP, part: 99 });

    expect(catalogue.detail).toBeUndefined();
    expect(catalogue.detailFault).toBe(thrown);
    expect(catalogue.detailLoading).toBe(false);
  });

  it('sends a change with the revision it read', async () => {
    const fake = fakes();
    const catalogue = new Catalogue(fake.parts, fake.files, 'user:demo');
    await catalogue.follow({ ...TOP, part: 5 });

    expect(await catalogue.changePart(5, { description: 'longer' })).toBe(true);

    const sent = fake.asked.find((row) => row.what === 'changePart')?.args[1];
    expect(sent).toEqual({ description: 'longer', revision: 3 });
    expect(catalogue.detail?.revision).toBe(4);
  });

  it('keeps the refusal of a write and answers that it did not land', async () => {
    const thrown = new Error('changed');
    const fake = fakes({ changePart: () => Promise.reject(thrown) });
    const catalogue = new Catalogue(fake.parts, fake.files, 'user:demo');
    await catalogue.follow({ ...TOP, part: 5 });

    expect(await catalogue.changePart(5, { name: 'x' })).toBe(false);
    expect(catalogue.fault).toBe(thrown);
    expect(catalogue.busy).toBe(false);
    expect(catalogue.detail?.revision).toBe(3);
  });

  it('deletes the part, then its folder in the storage', async () => {
    const fake = fakes();
    const catalogue = new Catalogue(fake.parts, fake.files, 'user:demo');
    await catalogue.follow({ ...TOP, part: 5 });

    expect(await catalogue.deletePart(5)).toBe(true);
    expect(fake.removed).toEqual(['local:parts/5']);
    expect(catalogue.detail).toBeUndefined();
  });

  it('reports a file it could not delete after the record went', async () => {
    const fake = fakes();
    const thrown = new Error('denied');
    fake.files.remove = () => Promise.reject(thrown);
    const catalogue = new Catalogue(fake.parts, fake.files, 'user:demo');

    expect(await catalogue.deletePart(5)).toBe(true);
    expect(catalogue.fault).toBe(thrown);
  });

  it('refuses a page size the pager does not offer and keeps its own', async () => {
    const fake = fakes();
    const catalogue = new Catalogue(fake.parts, fake.files, 'user:demo');
    await catalogue.follow(TOP);

    catalogue.limit = 7 as never;
    expect(catalogue.limit).toBe(50);
    expect(fake.count('list')).toBe(1);

    catalogue.limit = 25;
    expect(catalogue.limit).toBe(25);
    expect(catalogue.offset).toBe(0);
  });

  it('refuses an offset that is not a page start', async () => {
    const fake = fakes();
    const catalogue = new Catalogue(fake.parts, fake.files, 'user:demo');
    await catalogue.follow(TOP);

    catalogue.offset = -1;
    catalogue.offset = 1.5;

    expect(catalogue.offset).toBe(0);
    expect(fake.count('list')).toBe(1);
  });

  it('walks the tree from the top down to a folder', async () => {
    const fake = fakes();
    const catalogue = new Catalogue(fake.parts, fake.files, 'user:demo');
    await catalogue.follow(TOP);

    expect(catalogue.trailOf(2).map((folder) => folder.name)).toEqual([
      'Garage',
      'Shelves',
    ]);
    expect(catalogue.childrenOf(0).map((folder) => folder.id)).toEqual([1]);
    expect(catalogue.trailOf(0)).toEqual([]);
  });

  it('adds a comment to the part on screen without reading it again', async () => {
    const fake = fakes();
    const catalogue = new Catalogue(fake.parts, fake.files, 'user:demo');
    await catalogue.follow({ ...TOP, part: 5 });

    await catalogue.addComment(5, 'nice');

    expect(catalogue.detail?.comments.map((row) => row.body)).toEqual(['nice']);
    expect(fake.count('part')).toBe(1);
  });

  it('replaces the rows once per read, whatever else moves', async () => {
    const fake = fakes();
    const catalogue = new Catalogue(fake.parts, fake.files, 'user:demo');
    await catalogue.follow(TOP);

    const rows = catalogue.rows;
    await catalogue.changeFolder(1, { name: 'Workshop' });

    expect(catalogue.rows).toBe(rows);
  });

  it('puts the newest overview on screen when an older read answers last', async () => {
    const answers: ((overview: Overview) => void)[] = [];
    const signals: (AbortSignal | undefined)[] = [];
    const fake = fakes({
      overview: (signal) => {
        signals.push(signal);

        return new Promise<Overview>((resolve) => {
          answers.push(resolve);
        });
      },
    });
    const catalogue = new Catalogue(fake.parts, fake.files, 'user:demo');

    // The frame reads the overview, and a write reads it again before the
    // first read answered.
    const framing = catalogue.follow(TOP);
    const writing = catalogue.addFolder({ name: 'Workshop' });
    await Promise.resolve();
    await Promise.resolve();
    expect(answers).toHaveLength(2);

    const newer: Overview = { ...OVERVIEW, root_parts: 7 };
    answers[1]?.(newer);
    await writing;
    answers[0]?.({ ...OVERVIEW, root_parts: 1 });
    await framing;

    expect(catalogue.overview).toBe(newer);
    expect(signals[0]?.aborted).toBe(true);
    expect(signals[1]?.aborted).toBe(false);
  });

  it('indexes the folders once per overview, and keeps the index across reads that do not touch it', async () => {
    const fake = fakes();
    const catalogue = new Catalogue(fake.parts, fake.files, 'user:demo');
    await catalogue.follow(TOP);

    const children = catalogue.childrenOf(1);
    const branches = catalogue.branches;
    expect(children.map((folder) => folder.id)).toEqual([2]);
    expect(branches).toEqual([1]);
    expect(catalogue.childrenOf(2)).toEqual([]);
    expect(catalogue.folder(2)?.name).toBe('Shelves');

    // A listing read and a comment are not an overview read.
    catalogue.offset = 50;
    await catalogue.addComment(5, 'nice');

    expect(catalogue.childrenOf(1)).toBe(children);
    expect(catalogue.branches).toBe(branches);
  });

  it('walks a tree of thousands of folders by lookups', async () => {
    // Ten thousand folders, a hundred under each of a hundred at the top.
    const folders = Array.from({ length: 10100 }, (_, at) => ({
      ...OVERVIEW.folders[0]!,
      id: at + 1,
      parent: at < 100 ? 0 : Math.floor((at - 100) / 100) + 1,
      name: `folder ${String(at + 1)}`,
    }));
    const fake = fakes({
      overview: () => Promise.resolve({ ...OVERVIEW, folders }),
    });
    const catalogue = new Catalogue(fake.parts, fake.files, 'user:demo');
    await catalogue.follow(TOP);

    expect(catalogue.childrenOf(0)).toHaveLength(100);
    expect(catalogue.childrenOf(100)).toHaveLength(100);
    expect(catalogue.trailOf(10100).map((folder) => folder.id)).toEqual([
      100, 10100,
    ]);
    expect(catalogue.branches).toHaveLength(100);
  });

  it('keeps a dialog busy while a write made in the background ends first', async () => {
    let answer: (part: PartDetail) => void = () => undefined;
    const fake = fakes({
      addPart: (draft) =>
        draft.name === 'from a dialog'
          ? new Promise<PartDetail>((resolve) => {
              answer = resolve;
            })
          : Promise.resolve(PART),
    });
    const catalogue = new Catalogue(fake.parts, fake.files, 'user:demo');
    await catalogue.follow(TOP);

    const dialog = catalogue.addPart({ name: 'from a dialog', folder: 0 });
    expect(catalogue.busy).toBe(true);

    await catalogue.addPartQuietly({ name: 'dropped', folder: 0 });
    expect(catalogue.busy).toBe(true);

    answer(PART);
    await dialog;
    expect(catalogue.busy).toBe(false);
  });

  it('leaves the fault a dialog kept to the dialog when a background write is refused or lands', async () => {
    const refused = new Error('refused in a dialog');
    const thrown = new Error('refused in the background');
    const fake = fakes({
      changePart: () => Promise.reject(refused),
      addPart: (draft) =>
        draft.name === 'bad' ? Promise.reject(thrown) : Promise.resolve(PART),
    });
    const catalogue = new Catalogue(fake.parts, fake.files, 'user:demo');
    await catalogue.follow(TOP);

    await catalogue.changePart(5, { name: 'x' });
    expect(catalogue.fault).toBe(refused);

    await catalogue.addPartQuietly({ name: 'good', folder: 0 });
    await expect(
      catalogue.addPartQuietly({ name: 'bad', folder: 0 }),
    ).rejects.toBe(thrown);

    expect(catalogue.fault).toBe(refused);
    expect(catalogue.busy).toBe(false);
  });

  it('answers what a background delete left in the storage, and throws when the part stayed', async () => {
    const fake = fakes();
    const denied = new Error('denied');
    fake.files.remove = () => Promise.reject(denied);
    const catalogue = new Catalogue(fake.parts, fake.files, 'user:demo');

    expect(await catalogue.deletePartQuietly(5)).toBe(denied);
    expect(catalogue.fault).toBeUndefined();

    const gone = new Error('store failed');
    const failing = new Catalogue(
      fakes({ deletePart: () => Promise.reject(gone) }).parts,
      fake.files,
      'user:demo',
    );
    await expect(failing.deletePartQuietly(5)).rejects.toBe(gone);
  });

  it('deletes every folder a deleted part kept files in, each in its own storage', async () => {
    const fake = fakes({
      deletePart: () =>
        Promise.resolve({
          storage: 'local',
          dirs: [
            { storage: 'local', dir: 'parts/5' },
            { storage: 'nas', dir: 'old-root/5' },
          ],
          files: [
            {
              name: 'a.stl',
              storage: 'local',
              path: 'parts/5/a1/a.stl',
              size: 1,
            },
            {
              name: 'b.stl',
              storage: 'nas',
              path: 'old-root/5/b2/b.stl',
              size: 1,
            },
          ],
        }),
    });
    const catalogue = new Catalogue(fake.parts, fake.files, 'user:demo');

    expect(await catalogue.deletePart(5)).toBe(true);
    // The folders go whole; the files inside them are not asked again.
    expect(fake.removed).toEqual(['local:parts/5', 'nas:old-root/5']);
    expect(catalogue.fault).toBeUndefined();
  });

  it('deletes the files a deleted version left, one by one, and keeps the revision it answered', async () => {
    const answered: PartDetail = { ...PART, revision: 9, last_version: 2 };
    const fake = fakes({
      deleteVersion: () =>
        Promise.resolve({
          removed: {
            storage: 'local',
            dirs: [],
            files: [
              {
                name: 'a.stl',
                storage: 'local',
                path: 'parts/5/a1/a.stl',
                size: 1,
              },
            ],
          },
          part: answered,
        }),
    });
    const catalogue = new Catalogue(fake.parts, fake.files, 'user:demo');
    await catalogue.follow({ ...TOP, part: 5 });

    expect(await catalogue.deleteVersion(5, 2)).toBe(true);
    expect(fake.removed).toEqual(['local:parts/5/a1/a.stl']);

    // Deleting the newest version moves the revision, so the next change is
    // sent against the one the delete answered.
    await catalogue.changePart(5, { name: 'Hook 2' });
    const sent = fake.asked.find((row) => row.what === 'changePart')?.args[1];
    expect(sent).toEqual({ name: 'Hook 2', revision: 9 });
  });
});
