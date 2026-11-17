import { describe, expect, it } from 'vitest';

import { Fleet } from '../../data';
import { fakeDevice, fakeFleetClient } from '../../test/fakes';
import { byTag, tagRowsOf } from './presentation';
import type { TagFleetSource } from './types';

const tagged = (id: string, tags: string[]) => ({
  ...fakeDevice(id, id),
  tags,
});

describe('the fleet port', () => {
  it('is satisfied by the store this product hands in', () => {
    const { client } = fakeFleetClient([]);
    // Structural typing makes the match silent in both directions, so it is
    // asserted rather than assumed.
    const source: TagFleetSource = new Fleet(client);

    expect(source.loaded).toBe(false);
  });
});

describe('the tags the fleet carries', () => {
  it('gathers one row per tag, with every printer carrying it', () => {
    const rows = tagRowsOf([
      tagged('p0', ['prototyping', 'brno']),
      tagged('p1', ['brno']),
      tagged('p2', []),
    ]).sort(byTag);

    expect(rows.map((row) => row.tag)).toEqual(['brno', 'prototyping']);
    expect(rows[0]?.printers.map((printer) => printer.id)).toEqual([
      'p0',
      'p1',
    ]);
    expect(rows[0]?.printers[0]?.detailHref).toBe('/printers/p0');
  });

  // The server takes two tags that differ only in case as one tag, and the
  // first spelling is the one shown.
  it('folds two spellings that differ only in case into one row', () => {
    const rows = tagRowsOf([
      tagged('p0', ['Night shift']),
      tagged('p1', ['night shift']),
    ]);

    expect(rows).toHaveLength(1);
    expect(rows[0]?.tag).toBe('Night shift');
    expect(rows[0]?.printers).toHaveLength(2);
  });

  // A family emoji with its joiners and the same people written side by side
  // are one tag.
  it('folds a tag written with and without joiners into one row', () => {
    const rows = tagRowsOf([
      tagged('p0', ['\u{1F468}\u200D\u{1F469}\u200D\u{1F467}']),
      tagged('p1', ['\u{1F468}\u{1F469}\u{1F467}']),
    ]);

    expect(rows).toHaveLength(1);
    expect(rows[0]?.printers).toHaveLength(2);
  });

  it('lists a printer once under a tag it carries in two spellings', () => {
    const rows = tagRowsOf([tagged('p0', ['PETG', 'petg'])]);

    expect(rows[0]?.printers).toHaveLength(1);
  });
});
