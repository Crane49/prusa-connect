import { describe, expect, it } from 'vitest';

import {
  DESCRIBE_AT_ONCE,
  describeInTurn,
  isPrintFile,
  siblingPath,
  splitName,
  type DescribeSource,
} from './presentation';

/** A store that answers on the next turn of the loop and counts what is in flight. */
const counted = () => {
  const asked: string[] = [];
  let open = 0;
  let peak = 0;

  const source: DescribeSource = {
    describe: async (_storage: string, path: string) => {
      asked.push(path);
      open += 1;
      peak = Math.max(peak, open);
      await new Promise((resolve) => setTimeout(resolve, 1));
      open -= 1;
    },
  };

  return { source, asked, peak: () => peak };
};

describe('isPrintFile', () => {
  // The server picks its parser from the first bytes and not from the name
  // (internal/printfile), so every spelling a slicer writes text G-code under
  // reads the same.
  it('names every file a slicer writes', () => {
    expect(isPrintFile('bracket.bgcode')).toBe(true);
    expect(isPrintFile('bracket.gcode')).toBe(true);
    expect(isPrintFile('bracket.gco')).toBe(true);
    expect(isPrintFile('bracket.g')).toBe(true);
    expect(isPrintFile('Bracket.3MF')).toBe(true);
  });

  // Asking about these can only ever answer "no picture", and every ask opens
  // the file on the server.
  it('leaves out a file that is not a print', () => {
    expect(isPrintFile('bracket.stl')).toBe(false);
    expect(isPrintFile('notes.txt')).toBe(false);
    expect(isPrintFile('bracket.gcode.bak')).toBe(false);
    expect(isPrintFile('gcode')).toBe(false);
  });
});

describe('describeInTurn', () => {
  it('keeps at most four files in flight at once', async () => {
    const store = counted();
    const paths = Array.from(
      { length: 20 },
      (_, at) => `file-${String(at)}.bgcode`,
    );

    await describeInTurn(
      store.source,
      'local',
      paths,
      new AbortController().signal,
    );

    expect(store.asked).toHaveLength(20);
    expect(store.peak()).toBe(DESCRIBE_AT_ONCE);
  });

  // A reader who turns the page leaves the rest of the batch behind. Without
  // this, a flip through ten pages stacks ten batches on the server.
  it('stops asking once the batch is abandoned', async () => {
    const store = counted();
    const batch = new AbortController();
    const paths = Array.from(
      { length: 20 },
      (_, at) => `file-${String(at)}.bgcode`,
    );
    const done = describeInTurn(store.source, 'local', paths, batch.signal);

    batch.abort();
    await done;

    // The four that were already in flight when the reader turned the page,
    // and nothing after them.
    expect(store.asked).toHaveLength(DESCRIBE_AT_ONCE);
  });

  it('hands the signal to the store, so a read in flight is stopped too', async () => {
    const seen: (AbortSignal | undefined)[] = [];
    const source: DescribeSource = {
      describe: (_storage, _path, signal) => {
        seen.push(signal);

        return Promise.resolve();
      },
    };
    const batch = new AbortController();
    await describeInTurn(source, 'local', ['one.bgcode'], batch.signal);

    expect(seen).toEqual([batch.signal]);
  });

  // One file the server could not open must not take a worker out of the pool
  // for the rest of the page, and must not reject the batch.
  it('keeps going past a read that failed', async () => {
    const asked: string[] = [];
    let open = 0;
    let peak = 0;
    const source: DescribeSource = {
      describe: async (_storage, path) => {
        asked.push(path);
        open += 1;
        peak = Math.max(peak, open);
        await new Promise((resolve) => setTimeout(resolve, 1));
        open -= 1;
        if (path.startsWith('bad')) throw new Error('the server refused');
      },
    };
    const paths = Array.from({ length: 12 }, (_, at) =>
      at % 3 === 0 ? `bad-${String(at)}` : `good-${String(at)}`,
    );

    await expect(
      describeInTurn(source, 'local', paths, new AbortController().signal),
    ).resolves.toBeUndefined();
    expect(asked).toHaveLength(12);
    expect(peak).toBe(DESCRIBE_AT_ONCE);
  });

  it('asks nothing for an empty page', async () => {
    const store = counted();
    await describeInTurn(
      store.source,
      'local',
      [],
      new AbortController().signal,
    );

    expect(store.asked).toEqual([]);
  });
});

describe('renaming', () => {
  it('keeps the extension of a file and the whole name of a folder', () => {
    expect(splitName('benchy.v2.3mf', false)).toEqual({
      stem: 'benchy.v2',
      extension: '.3mf',
    });
    expect(splitName('v1.2', true)).toEqual({ stem: 'v1.2', extension: '' });
    expect(splitName('.hidden', false)).toEqual({
      stem: '.hidden',
      extension: '',
    });
    expect(splitName('README', false)).toEqual({
      stem: 'README',
      extension: '',
    });
  });

  it('puts the new name in the same folder', () => {
    expect(siblingPath('models/a/benchy.3mf', 'boat.3mf')).toBe(
      'models/a/boat.3mf',
    );
    expect(siblingPath('benchy.3mf', 'boat.3mf')).toBe('boat.3mf');
  });
});
