import type { ServerStorage } from '@prusa/connect-ui/data';
import { describe, expect, it } from 'vitest';

import { Catalogue } from './catalogue.svelte';
import { groupByStem, stemOf } from './drop-groups';
import type { Allowed, FilesService } from './files';
import { FolderDrop } from './folder-drop.svelte';
import { TOP } from './navigation-query';
import type { FileDraft, PartDetail, PartDraft, PartsService } from './parts';

const EVERYTHING: Allowed = {
  read: true,
  write: true,
  upload: true,
  files: true,
};

/** A part as the module answers a new one: the id it counted, and its folder in the storage. */
const made = (id: number, draft: PartDraft): PartDetail => ({
  v: 1,
  id,
  folder: draft.folder ?? 0,
  name: draft.name ?? '',
  tags: [],
  created: '',
  created_by: 'user:demo',
  changed: '',
  changed_by: 'user:demo',
  revision: 1,
  versions: [],
  comments: [],
  dir: `parts/${String(id)}`,
  storage: 'local',
});

const storage = (allowed: string[]): ServerStorage => ({
  name: 'local',
  display_name: 'Storage',
  allowed_extensions: allowed,
  browse: true,
  write: true,
  move: true,
  ranges: true,
  download_url: false,
});

interface Options {
  allowed?: Allowed;
  extensions?: string[];
  /** A file name the storage refuses. */
  refuse?: string;
  /** A folder the module no longer holds, so a part made in it is refused. */
  gone?: number;
  /** What deleting a part throws, for a cleanup that fails. */
  undeletable?: Error;
  /** What deleting a file throws, for files the storage keeps. */
  unremovable?: Error;
}

/** A catalogue over services in memory, recording what the drop asked of them. */
const build = async (options: Options = {}) => {
  const log: string[] = [];
  let next = 10;
  const unused = (): Promise<never> =>
    Promise.reject(new Error('not asked in these cases'));

  const parts: PartsService = {
    overview: () =>
      Promise.resolve({
        folders: [],
        root_parts: 0,
        tags: [],
        usage: { bytes: 0, limit: 8388608 },
        storage: 'local',
        root: 'parts',
        principal: 'user:demo',
      }),
    list: () => {
      log.push('list');

      return Promise.resolve({
        parts: [],
        matched: 0,
        held: 0,
        principal: 'user:demo',
      });
    },
    part: unused,
    addFolder: unused,
    changeFolder: unused,
    deleteFolder: unused,
    addPart: (draft) => {
      if (options.gone !== undefined && draft.folder === options.gone) {
        log.push(`addPart ${draft.name ?? ''} refused`);

        return Promise.reject(new Error('parts/NOT_FOUND: no such folder'));
      }
      next += 1;
      log.push(`addPart ${draft.name ?? ''} in ${String(draft.folder)}`);

      return Promise.resolve(made(next, draft));
    },
    changePart: unused,
    deletePart: (id) => {
      log.push(`deletePart ${String(id)}`);
      if (options.undeletable !== undefined) {
        return Promise.reject(options.undeletable);
      }

      return Promise.resolve({
        storage: 'local',
        dir: `parts/${String(id)}`,
        dirs: [{ storage: 'local', dir: `parts/${String(id)}` }],
        files: [],
      });
    },
    addVersion: (id: number, _note: string, files: FileDraft[]) => {
      log.push(
        `addVersion ${String(id)} ${files.map((file) => file.path.split('/').at(-1)).join(',')}`,
      );

      return Promise.resolve(made(id, {}));
    },
    changeVersion: unused,
    deleteVersion: unused,
    addComment: unused,
    changeComment: unused,
    deleteComment: unused,
  };

  const uploaded: string[] = [];
  const files: FilesService = {
    storages: () => Promise.resolve([storage(options.extensions ?? [])]),
    allowed: () => Promise.resolve(options.allowed ?? EVERYTHING),
    upload: (_storage, path) => {
      if (options.refuse !== undefined && path.endsWith(`/${options.refuse}`)) {
        return Promise.reject(new Error('the storage refused it'));
      }
      uploaded.push(path);

      return Promise.resolve(1);
    },
    remove: (_storage, path) => {
      log.push(`remove ${path}`);
      if (options.unremovable !== undefined) {
        return Promise.reject(options.unremovable);
      }

      return Promise.resolve();
    },
    describe: () => Promise.resolve(undefined),
    print: unused,
    thumbnailUrl: () => '',
    contentUrl: () => '',
  };

  const catalogue = new Catalogue(parts, files, 'user:demo');
  await catalogue.follow(TOP);
  log.length = 0;

  return { catalogue, drop: new FolderDrop(catalogue, files), log, uploaded };
};

const file = (name: string): File => new File(['G28'], name);

describe('grouping dropped files', () => {
  it('names a part after its file without the last extension', () => {
    expect(stemOf('hook.bgcode')).toBe('hook');
    expect(stemOf('hook.v2.stl')).toBe('hook.v2');
    expect(stemOf('README')).toBe('README');
    expect(stemOf('.gitkeep')).toBe('.gitkeep');
  });

  it('puts files that differ only by their extension into one part, in the order they came', () => {
    const groups = groupByStem([
      file('hook.stl'),
      file('shelf.3mf'),
      file('Hook.bgcode'),
    ]);

    expect(
      groups.map((group) => [group.name, group.files.map((row) => row.name)]),
    ).toEqual([
      ['hook', ['hook.stl', 'Hook.bgcode']],
      ['shelf', ['shelf.3mf']],
    ]);
  });
});

describe('FolderDrop', () => {
  it('starts with nothing dropped', async () => {
    const { drop } = await build();

    expect(drop.jobs).toEqual([]);
    expect(drop.sending).toBe(false);
    expect(drop.allowed).toBe(true);
  });

  it('makes one part of each group, uploads its files into the part, and records them as its first version', async () => {
    const { drop, log, uploaded } = await build();

    await drop.drop(4, [
      file('hook.stl'),
      file('hook.bgcode'),
      file('shelf.3mf'),
    ]);

    expect(log).toEqual([
      'addPart hook in 4',
      'addVersion 11 hook.stl,hook.bgcode',
      'addPart shelf in 4',
      'addVersion 12 shelf.3mf',
      'list',
    ]);
    // Every file of a part went into that part's own folder, under one
    // upload segment.
    expect(uploaded).toHaveLength(3);
    expect(uploaded[0]).toMatch(/^parts\/11\/[a-z0-9]+\/hook\.stl$/);
    expect(uploaded[1]?.split('/').slice(0, 3)).toEqual(
      uploaded[0]?.split('/').slice(0, 3),
    );
    expect(uploaded[2]).toMatch(/^parts\/12\/[a-z0-9]+\/shelf\.3mf$/);

    expect(
      drop.jobs.map((job) => [job.name, job.state, job.part, job.files.length]),
    ).toEqual([
      ['hook', 'done', 11, 2],
      ['shelf', 'done', 12, 1],
    ]);
    expect(drop.sending).toBe(false);
  });

  it('makes no part of files the storage does not take', async () => {
    const { drop, log } = await build({ extensions: ['.gcode', '.bgcode'] });

    await drop.drop(0, [file('hook.bgcode'), file('photo.jpg')]);

    expect(log).toEqual([
      'addPart hook in 0',
      'addVersion 11 hook.bgcode',
      'list',
    ]);
    expect(drop.jobs.map((job) => [job.name, job.state])).toEqual([
      ['hook', 'done'],
      ['photo', 'refused'],
    ]);
  });

  it('takes a part back out when the storage refuses one of its files', async () => {
    const { catalogue, drop, log } = await build({ refuse: 'hook.bgcode' });

    await drop.drop(0, [file('hook.stl'), file('hook.bgcode')]);

    expect(log).toEqual([
      'addPart hook in 0',
      'deletePart 11',
      'remove parts/11',
      'list',
    ]);
    const [job] = drop.jobs;
    expect(job?.state).toBe('failed');
    expect(job?.fault).toBeInstanceOf(Error);
    // The fault is the row's, so the page does not draw it a second time.
    expect(catalogue.fault).toBeUndefined();
  });

  it('does nothing for a person who may not write files', async () => {
    const { drop, log } = await build({
      allowed: { ...EVERYTHING, upload: false },
    });

    await drop.drop(0, [file('hook.stl')]);

    expect(drop.allowed).toBe(false);
    expect(drop.jobs).toEqual([]);
    expect(log).toEqual([]);
  });

  it('refuses a folder no folder can be, and makes nothing', async () => {
    const { drop, log } = await build();

    await drop.drop(-1, [file('hook.stl')]);
    await drop.drop(0, []);

    expect(drop.jobs).toEqual([]);
    expect(log).toEqual([]);
  });

  it('clears the finished rows and keeps the list when nothing finished', async () => {
    const { drop } = await build({ extensions: ['.gcode'] });

    drop.clear();
    expect(drop.jobs).toEqual([]);

    await drop.drop(0, [file('photo.jpg')]);
    const before = drop.jobs;
    drop.clear();

    expect(before).toHaveLength(1);
    expect(drop.jobs).toEqual([]);
  });

  it('keeps one file of a name dropped twice, so the version names it once', async () => {
    const { drop, log, uploaded } = await build();

    await drop.drop(0, [
      file('hook.stl'),
      file('hook.bgcode'),
      file('Hook.STL'),
    ]);

    // Hook.STL replaced hook.stl: one part, two files, the later kept.
    expect(log).toEqual([
      'addPart hook in 0',
      'addVersion 11 Hook.STL,hook.bgcode',
      'list',
    ]);
    expect(uploaded).toHaveLength(2);
  });

  it('makes a part of the files the storage takes and a row of the ones it does not', async () => {
    const { drop, log } = await build({ extensions: ['.stl'] });

    await drop.drop(0, [file('hook.stl'), file('hook.exe')]);

    expect(log).toEqual([
      'addPart hook in 0',
      'addVersion 11 hook.stl',
      'list',
    ]);
    expect(drop.jobs.map((job) => [job.name, job.state, job.files])).toEqual([
      ['hook', 'done', ['hook.stl']],
      ['hook', 'refused', ['hook.exe']],
    ]);
  });

  it('finishes the parts before a failure and the parts after it', async () => {
    const { drop, log } = await build({ refuse: 'b.stl' });

    await drop.drop(0, [file('a.stl'), file('b.stl'), file('c.stl')]);

    expect(drop.jobs.map((job) => [job.name, job.state])).toEqual([
      ['a', 'done'],
      ['b', 'failed'],
      ['c', 'done'],
    ]);
    // b's part was taken back out, and c still went on to be made.
    expect(log).toContain('deletePart 12');
    expect(log).toContain('addVersion 13 c.stl');
  });

  it('runs a second drop after the first, in the order they came', async () => {
    const { drop, log } = await build();

    await Promise.all([
      drop.drop(0, [file('first.stl')]),
      drop.drop(0, [file('second.stl')]),
    ]);

    expect(log.filter((line) => line.startsWith('addPart'))).toEqual([
      'addPart first in 0',
      'addPart second in 0',
    ]);
  });

  it('marks a drop onto a folder deleted meanwhile as failed, and uploads nothing', async () => {
    const { catalogue, drop, uploaded } = await build({ gone: 9 });

    await drop.drop(9, [file('hook.stl')]);

    const [job] = drop.jobs;
    expect(job?.state).toBe('failed');
    expect(job?.fault).toBeInstanceOf(Error);
    expect(uploaded).toEqual([]);
    expect(catalogue.fault).toBeUndefined();
  });

  it('gives a long name in accents a part name the module takes, and a file name too', async () => {
    const { drop, log, uploaded } = await build();
    const long = 'ž'.repeat(100);

    await drop.drop(0, [file(`${long}.bgcode`)]);

    const name = log[0]?.replace(/^addPart (.*) in 0$/u, '$1') ?? '';
    // 100 two-byte letters are 200 bytes; the module holds a name to 120.
    expect(new TextEncoder().encode(name).length).toBeLessThanOrEqual(120);
    expect(name).toBe('ž'.repeat(60));
    const segment = uploaded[0]?.split('/').at(-1) ?? '';
    expect(new TextEncoder().encode(segment).length).toBeLessThanOrEqual(200);
    expect(segment.endsWith('.bgcode')).toBe(true);
  });

  it('names a part after a file with no extension, and after a hidden file', async () => {
    const { drop, log } = await build();

    await drop.drop(0, [file('README'), file('.gitkeep')]);

    expect(log.filter((line) => line.startsWith('addPart'))).toEqual([
      'addPart README in 0',
      'addPart .gitkeep in 0',
    ]);
  });

  it('says the part is left when taking a failed one back out is refused too, and links to it', async () => {
    const undeletable = new Error('the catalogue could not be written');
    const { catalogue, drop } = await build({
      refuse: 'hook.stl',
      undeletable,
    });

    await drop.drop(0, [file('hook.stl')]);

    const [job] = drop.jobs;
    expect(job?.state).toBe('failed');
    expect(job?.left).toBe('part');
    expect(job?.part).toBe(11);
    expect(job?.leftFault).toBe(undeletable);
    expect(catalogue.fault).toBeUndefined();
  });

  it('says files are left in the storage when the part went and its files did not', async () => {
    const unremovable = new Error('the storage refused the delete');
    const { drop } = await build({ refuse: 'hook.stl', unremovable });

    await drop.drop(0, [file('hook.stl')]);

    const [job] = drop.jobs;
    expect(job?.state).toBe('failed');
    expect(job?.left).toBe('files');
    expect(job?.leftIn).toBe('parts/11');
    expect(job?.leftFault).toBe(unremovable);
    expect(job?.part).toBeUndefined();
  });

  it('says nothing is left when a failed part was taken back out whole', async () => {
    const { drop } = await build({ refuse: 'hook.stl' });

    await drop.drop(0, [file('hook.stl')]);

    expect(drop.jobs[0]?.left).toBeUndefined();
  });

  it('leaves the fault a dialog kept, and its busy flag, alone while a drop runs', async () => {
    const { catalogue, drop } = await build();
    // A refused dialog write the person has not read yet.
    expect(await catalogue.changePart(5, { name: 'x' })).toBe(false);
    const unread = catalogue.fault;
    expect(unread).toBeInstanceOf(Error);

    await drop.drop(0, [file('hook.stl')]);

    expect(drop.jobs[0]?.state).toBe('done');
    expect(catalogue.fault).toBe(unread);
    expect(catalogue.busy).toBe(false);
  });
});
