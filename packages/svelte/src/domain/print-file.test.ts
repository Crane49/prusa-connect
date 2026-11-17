import { describe, expect, it } from 'vitest';

import { newestFirst } from './print-file';

interface Row {
  folder: boolean;
  modified?: string;
  name: string;
}

const row = (name: string, modified?: string, folder = false): Row =>
  modified === undefined ? { folder, name } : { folder, modified, name };

describe('newestFirst', () => {
  const order = (rows: Row[]): string[] =>
    rows.sort(newestFirst((r) => r.name)).map((r) => r.name);

  it('puts folders first, then the newest file, then undated ones', () => {
    expect(
      order([
        row('old.bgcode', '2026-01-01T00:00:00Z'),
        row('fw.bbf'),
        row('new.bgcode', '2026-09-01T00:00:00Z'),
        row('older-dir', '2025-01-01T00:00:00Z', true),
        row('newer-dir', '2026-02-01T00:00:00Z', true),
        row('junk.gcode', 'not a date'),
      ]),
    ).toEqual([
      'newer-dir',
      'older-dir',
      'new.bgcode',
      'old.bgcode',
      'fw.bbf',
      'junk.gcode',
    ]);
  });

  it('breaks ties by name', () => {
    const at = '2026-09-01T00:00:00Z';

    expect(order([row('b', at), row('a', at), row('d'), row('c')])).toEqual([
      'a',
      'b',
      'c',
      'd',
    ]);
  });
});
