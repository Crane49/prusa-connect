import { describe, expect, it } from 'vitest';

import { Selection } from './selection.svelte';

const listing = (initial: readonly string[]) => {
  let rows = $state.raw(initial);
  const selection = new Selection<string>({ rows: () => rows });

  return {
    selection,
    setRows: (next: readonly string[]) => {
      rows = next;
    },
  };
};

describe('Selection', () => {
  it('starts with nothing ticked', () => {
    const { selection } = listing(['a', 'b']);

    expect(selection.keys).toEqual([]);
    expect(selection.count).toBe(0);
    expect(selection.total).toBe(2);
  });

  it('ticks and unticks one row, keeping the order of the ticks', () => {
    const { selection } = listing(['a', 'b', 'c']);

    selection.toggle('c');
    selection.toggle('a');
    expect(selection.keys).toEqual(['c', 'a']);

    selection.toggle('c');
    expect(selection.keys).toEqual(['a']);
  });

  // The rows between take the new state of the row pressed, as in a mail
  // client: a shift press on a ticked row unticks the range.
  it('ticks a range in the order of the listing, either way', () => {
    const { selection } = listing(['a', 'b', 'c', 'd', 'e']);

    selection.toggle('d');
    selection.toggle('b', true);
    expect(selection.stateOf(['b', 'c', 'd'])).toBe('all');
    expect(selection.has('a')).toBe(false);
    expect(selection.has('e')).toBe(false);

    selection.toggle('e', true);
    expect(selection.stateOf(['b', 'c', 'd', 'e'])).toBe('all');
  });

  it('unticks a range when the row pressed was ticked', () => {
    const { selection } = listing(['a', 'b', 'c', 'd']);

    selection.selectAll();
    selection.toggle('a');
    selection.toggle('c', true);
    expect(selection.keys).toEqual(['d']);
  });

  it('ticks one row on a range press with nothing pressed before', () => {
    const { selection } = listing(['a', 'b', 'c']);

    selection.toggle('c', true);
    expect(selection.keys).toEqual(['c']);
  });

  // The row a range starts from was deleted, so there is no range to take.
  it('ticks one row when the start of the range left the listing', () => {
    const { selection, setRows } = listing(['a', 'b', 'c']);

    selection.toggle('a');
    setRows(['b', 'c']);
    selection.toggle('c', true);
    expect(selection.keys).toEqual(['c']);
  });

  it('never counts a row that left the listing, and keeps it for its return', () => {
    const { selection, setRows } = listing(['a', 'b', 'c']);

    selection.set(['a', 'b'], true);
    setRows(['b', 'c']);
    expect(selection.keys).toEqual(['b']);
    expect(selection.has('a')).toBe(false);

    setRows(['a', 'b', 'c']);
    expect(selection.keys).toEqual(['a', 'b']);
  });

  it('ticks every row on every page, and clears them', () => {
    const { selection } = listing(['a', 'b', 'c']);

    selection.toggle('b');
    selection.selectAll();
    expect(selection.keys).toEqual(['b', 'a', 'c']);

    selection.clear();
    expect(selection.keys).toEqual([]);
    selection.toggle('c', true);
    expect(selection.keys).toEqual(['c']);
  });

  it('says whether none, some or all of a group are ticked', () => {
    const { selection } = listing(['a', 'b', 'c']);

    expect(selection.stateOf(['a', 'b'])).toBe('none');
    selection.toggle('a');
    expect(selection.stateOf(['a', 'b'])).toBe('some');
    selection.set(['a', 'b'], true);
    expect(selection.stateOf(['a', 'b'])).toBe('all');
    selection.set(['a', 'b'], false);
    expect(selection.keys).toEqual([]);
  });

  // A page that keeps its own copy hears of each change the person makes,
  // and never of a row that only left the listing.
  it('tells the page the ticked rows after each change', () => {
    let rows = $state.raw<readonly string[]>(['a', 'b', 'c']);
    const told: (readonly string[])[] = [];
    const selection = new Selection<string>({
      rows: () => rows,
      onchange: (keys) => told.push(keys),
    });

    selection.toggle('a');
    selection.toggle('c', true);
    rows = ['b', 'c'];
    selection.clear();
    expect(told).toEqual([['a'], ['a', 'b', 'c'], []]);
  });
});
