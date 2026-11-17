import { describe, expect, it } from 'vitest';

import type { FilesService } from './files';
import type { PartDetail } from './parts';
import { segmentOf, VersionDraft } from './version-draft.svelte';

const PART: PartDetail = {
  v: 1,
  id: 5,
  folder: 0,
  name: 'Hook',
  tags: [],
  created: '',
  created_by: 'user:demo',
  changed: '',
  changed_by: 'user:demo',
  revision: 1,
  versions: [
    {
      v: 1,
      part: 5,
      number: 1,
      files: [
        {
          name: 'hook.3mf',
          storage: 'local',
          path: 'parts/5/a1/hook.3mf',
          size: 10,
        },
        {
          name: 'hook.bgcode',
          storage: 'local',
          path: 'parts/5/a1/hook.bgcode',
          size: 20,
        },
      ],
      created: '',
      created_by: 'user:demo',
      changed: '',
      changed_by: 'user:demo',
    },
  ],
  comments: [],
  dir: 'parts/5',
  storage: 'local',
};

/** A storage that holds each upload until the test lets it land. */
const storage = () => {
  const uploaded: string[] = [];
  const removed: string[] = [];
  const waiting: {
    path: string;
    land: () => void;
    refuse: (error: unknown) => void;
  }[] = [];

  const files: Pick<FilesService, 'upload' | 'remove'> = {
    upload: (_storage, path) =>
      new Promise<number>((resolve, reject) => {
        uploaded.push(path);
        waiting.push({
          path,
          land: () => {
            resolve(1);
          },
          refuse: reject,
        });
      }),
    remove: (_storage, path) => {
      removed.push(path);

      return Promise.resolve();
    },
  };

  return { files: files as FilesService, uploaded, removed, waiting };
};

const settle = (): Promise<void> =>
  new Promise((resolve) => setTimeout(resolve, 0));

describe('segmentOf', () => {
  it('keeps a name and turns what a path cannot carry into a hyphen', () => {
    expect(segmentOf('bracket v2.3mf')).toBe('bracket v2.3mf');
    expect(segmentOf('a/b\\c.stl')).toBe('a-b-c.stl');
    expect(segmentOf('tab\there.gcode')).toBe('tab-here.gcode');
    expect(segmentOf('..')).toBe('file');
  });
});

describe('segmentOf, against the file name rules of the module', () => {
  const bytes = (text: string) => new TextEncoder().encode(text).length;

  it('clips a long name to 200 bytes and keeps its extension', () => {
    const segment = segmentOf(`${'a'.repeat(300)}.bgcode`);

    expect(bytes(segment)).toBe(200);
    expect(segment.endsWith('.bgcode')).toBe(true);
  });

  it('never cuts a character in half', () => {
    const segment = segmentOf(`${'😀'.repeat(80)}.stl`);

    expect(bytes(segment)).toBeLessThanOrEqual(200);
    expect(segment).toMatch(/^(😀)+\.stl$/u);
  });

  it('turns a C1 control character into a hyphen, as it does a C0 one', () => {
    expect(segmentOf('a\u0085b\u0009c.stl')).toBe('a-b-c.stl');
  });

  it('clips a long name with no extension, or with an extension too long to keep', () => {
    expect(bytes(segmentOf('x'.repeat(400)))).toBe(200);
    expect(bytes(segmentOf(`a.${'y'.repeat(300)}`))).toBe(200);
  });
});

describe('VersionDraft', () => {
  it('starts as the newest version, with every file kept', () => {
    const { files } = storage();
    const draft = new VersionDraft(files, PART);

    expect(draft.kept.map((row) => row.kept)).toEqual([true, true]);
    expect(draft.files).toEqual([
      { storage: 'local', path: 'parts/5/a1/hook.3mf' },
      { storage: 'local', path: 'parts/5/a1/hook.bgcode' },
    ]);
    expect(draft.ready).toBe(true);
  });

  it('uploads into the part folder under one segment of its own', async () => {
    const box = storage();
    const draft = new VersionDraft(box.files, PART);

    draft.add([new File(['x'], 'drawing.pdf'), new File(['y'], 'photo.jpg')]);

    expect(box.uploaded).toHaveLength(2);
    const [first, second] = box.uploaded;
    expect(first).toMatch(/^parts\/5\/[a-z0-9]{1,10}\/drawing\.pdf$/);
    expect(second?.split('/')[2]).toBe(first?.split('/')[2]);
    expect(draft.sending).toBe(true);
    expect(draft.ready).toBe(false);

    for (const row of box.waiting) row.land();
    await settle();

    expect(draft.uploads.map((row) => row.state)).toEqual(['done', 'done']);
    expect(draft.files.slice(2)).toEqual([{ path: first }, { path: second }]);
  });

  it('marks a refused upload and leaves it out of the version', async () => {
    const box = storage();
    const draft = new VersionDraft(box.files, PART);
    const refusal = new Error('the storage takes no .exe');

    draft.add([new File(['x'], 'tool.exe')]);
    box.waiting[0]?.refuse(refusal);
    await settle();

    expect(draft.uploads[0]?.state).toBe('failed');
    expect(draft.uploads[0]?.fault).toBe(refusal);
    expect(draft.files).toHaveLength(2);
  });

  it('is not ready with every file left out', () => {
    const { files } = storage();
    const draft = new VersionDraft(files, PART);

    for (const row of draft.kept) draft.keep(row.file, false);

    expect(draft.files).toEqual([]);
    expect(draft.ready).toBe(false);
  });

  it('deletes what it uploaded when it is thrown away, also a file still on its way', async () => {
    const box = storage();
    const draft = new VersionDraft(box.files, PART);

    draft.add([new File(['x'], 'one.stl'), new File(['y'], 'two.stl')]);
    box.waiting[0]?.land();
    await settle();

    await draft.discard();
    box.waiting[1]?.land();
    await settle();

    expect(box.removed.sort()).toEqual([...box.uploaded].sort());
    expect(draft.uploads).toEqual([]);
  });

  it('deletes a file taken out while it was on its way, once it lands, and never marks it', async () => {
    const box = storage();
    const draft = new VersionDraft(box.files, PART);

    draft.add([new File(['x'], 'one.stl')]);
    const [key] = draft.uploads.map((row) => row.key);
    await draft.drop(key ?? '');

    expect(draft.uploads).toEqual([]);
    expect(box.removed).toEqual([]);

    box.waiting[0]?.land();
    await settle();

    expect(box.removed).toEqual(box.uploaded);
    expect(draft.uploads).toEqual([]);
    expect(draft.files).toHaveLength(2);
  });

  it('sends a name dropped again to a path of its own, so an older upload landing last is deleted and not recorded', async () => {
    const box = storage();
    const draft = new VersionDraft(box.files, PART);

    draft.add([new File(['old'], 'hook.stl')]);
    draft.add([new File(['new'], 'hook.stl')]);

    const [older, newer] = box.uploaded;
    expect(older).toMatch(/^parts\/5\/[a-z0-9]{10}\/hook\.stl$/);
    expect(newer).toMatch(/^parts\/5\/[a-z0-9]{10}-[0-9]+\/hook\.stl$/);
    expect(draft.uploads.map((row) => row.path)).toEqual([newer]);

    // The newer bytes land first, the older ones after.
    box.waiting[1]?.land();
    await settle();
    box.waiting[0]?.land();
    await settle();

    expect(draft.uploads.map((row) => [row.path, row.state])).toEqual([
      [newer, 'done'],
    ]);
    expect(draft.files.slice(2)).toEqual([{ path: newer }]);
    expect(box.removed).toEqual([older]);
  });

  it('deletes the file a second one of the same name replaced, once it had landed', async () => {
    const box = storage();
    const draft = new VersionDraft(box.files, PART);

    draft.add([new File(['old'], 'hook.stl')]);
    box.waiting[0]?.land();
    await settle();
    draft.add([new File(['new'], 'hook.stl')]);
    await settle();

    expect(box.removed).toEqual([box.uploaded[0]]);
    expect(box.uploaded[1]).not.toBe(box.uploaded[0]);
  });

  it('keeps every file once saved, also when it is thrown away after', async () => {
    const box = storage();
    const draft = new VersionDraft(box.files, PART);
    draft.add([new File(['x'], 'one.stl')]);
    box.waiting[0]?.land();
    await settle();

    expect(await draft.save(() => Promise.resolve(true))).toBe(true);
    await draft.discard();

    expect(box.removed).toEqual([]);
  });

  it('waits for the answer when it is thrown away while being saved', async () => {
    const box = storage();
    const draft = new VersionDraft(box.files, PART);
    draft.add([new File(['x'], 'one.stl')]);
    box.waiting[0]?.land();
    await settle();

    let answer: (saved: boolean) => void = () => undefined;
    const saving = draft.save(
      () =>
        new Promise<boolean>((resolve) => {
          answer = resolve;
        }),
    );
    expect(draft.saving).toBe(true);

    // Leaving while the version is recorded deletes nothing yet.
    await draft.discard();
    expect(box.removed).toEqual([]);

    answer(true);
    expect(await saving).toBe(true);
    expect(box.removed).toEqual([]);
  });

  it('deletes its files when a save it was thrown away during is refused', async () => {
    const box = storage();
    const draft = new VersionDraft(box.files, PART);
    draft.add([new File(['x'], 'one.stl')]);
    box.waiting[0]?.land();
    await settle();

    let answer: (saved: boolean) => void = () => undefined;
    const saving = draft.save(
      () =>
        new Promise<boolean>((resolve) => {
          answer = resolve;
        }),
    );
    await draft.discard();
    answer(false);

    expect(await saving).toBe(false);
    expect(box.removed).toEqual(box.uploaded);
  });

  it('takes no file in and lets none out while it is being saved', async () => {
    const box = storage();
    const draft = new VersionDraft(box.files, PART);
    draft.add([new File(['x'], 'one.stl')]);
    box.waiting[0]?.land();
    await settle();

    let answer: (saved: boolean) => void = () => undefined;
    const saving = draft.save(
      () =>
        new Promise<boolean>((resolve) => {
          answer = resolve;
        }),
    );
    draft.add([new File(['y'], 'two.stl')]);
    await draft.drop(draft.uploads[0]?.key ?? '');

    expect(box.uploaded).toHaveLength(1);
    expect(draft.uploads).toHaveLength(1);
    expect(box.removed).toEqual([]);
    expect(draft.ready).toBe(false);

    answer(true);
    await saving;
  });
});
