import type { ScopedStore } from '@prusa/connect-ui/slots';
import { describe, expect, it } from 'vitest';

import { TreeState } from './tree-state.svelte';

/** A store in memory, counting the writes it was given. */
const memory = (start: Record<string, string> = {}) => {
  const held = new Map(Object.entries(start));
  let writes = 0;

  const store: ScopedStore = {
    get: (key) => held.get(key),
    set: (key, value) => {
      writes += 1;
      held.set(key, value);
    },
    delete: (key) => {
      writes += 1;
      held.delete(key);
    },
    keys: () => [...held.keys()].sort(),
  };

  return { store, held, writes: () => writes };
};

describe('TreeState', () => {
  it('opens every folder until the person closes one', () => {
    const tree = new TreeState(memory().store);

    expect(tree.allOpen).toBe(true);
    expect(tree.isOpen(0, 0, false)).toBe(true);
    expect(tree.isOpen(7, 0, false)).toBe(true);
  });

  it('reads the folders closed in an earlier visit, and passes over what it cannot read', () => {
    expect(
      new TreeState(memory({ 'tree.closed': '[3, -1, 1.5, "4"]' }).store)
        .allOpen,
    ).toBe(false);
    expect(
      new TreeState(memory({ 'tree.closed': '[3]' }).store).isOpen(3, 0, false),
    ).toBe(false);
    expect(
      new TreeState(memory({ 'tree.closed': 'not json' }).store).allOpen,
    ).toBe(true);
    expect(new TreeState(memory({ 'tree.closed': '{}' }).store).allOpen).toBe(
      true,
    );
  });

  it('keeps a closed folder for the person, and forgets it once it is open again', () => {
    const kept = memory();
    const tree = new TreeState(kept.store);

    tree.toggle(3, 0, false);

    expect(tree.isOpen(3, 0, false)).toBe(false);
    expect(kept.held.get('tree.closed')).toBe('[3]');

    tree.toggle(3, 0, false);

    expect(tree.isOpen(3, 0, false)).toBe(true);
    expect(kept.held.has('tree.closed')).toBe(false);
  });

  it('opens a closed folder on the way to the folder on screen, unless it was closed from there', () => {
    // Folder 3 holds folder 8. The person closes 3 while looking at 1.
    const tree = new TreeState(memory().store);
    tree.toggle(3, 1, false);

    // Going to 8 opens 3 by itself, and leaving 8 closes it again.
    expect(tree.isOpen(3, 8, true)).toBe(true);
    expect(tree.isOpen(3, 1, false)).toBe(false);

    // Closed while 8 is on screen, it stays closed there.
    tree.toggle(3, 8, true);
    expect(tree.isOpen(3, 8, true)).toBe(false);

    // And opens again when pressed.
    tree.toggle(3, 8, true);
    expect(tree.isOpen(3, 8, true)).toBe(true);
  });

  it('opens a folder closed in an earlier visit on the way to the folder on screen', () => {
    const tree = new TreeState(memory({ 'tree.closed': '[3]' }).store);

    expect(tree.isOpen(3, 8, true)).toBe(true);
    expect(tree.isOpen(3, 1, false)).toBe(false);
  });

  it('refuses an id no folder can have, and writes nothing', () => {
    const kept = memory();
    const tree = new TreeState(kept.store);

    tree.toggle(-1, 0, false);
    tree.toggle(1.5, 0, false);
    tree.closeAll([-2, Number.NaN], 0);

    expect(tree.allOpen).toBe(true);
    expect(kept.writes()).toBe(0);
  });

  it('closes every folder named and opens them all again', () => {
    const kept = memory();
    const tree = new TreeState(kept.store);

    tree.closeAll([1, 3], 0);

    expect(tree.isOpen(1, 0, false)).toBe(false);
    expect(tree.isOpen(3, 0, false)).toBe(false);
    expect(tree.isOpen(0, 0, false)).toBe(true);
    expect(kept.held.get('tree.closed')).toBe('[1,3]');

    tree.openAll();

    expect(tree.allOpen).toBe(true);
    expect(kept.held.has('tree.closed')).toBe(false);
  });

  it('writes nothing when opening a tree that is already open', () => {
    const kept = memory();
    const tree = new TreeState(kept.store);

    tree.openAll();
    tree.closeAll([], 0);

    expect(kept.writes()).toBe(0);
  });

  // A folder closed and then deleted stays in the stored list. The header
  // asks about the folders it draws, so it still offers to close them all.
  it('reads the folders drawn as all open when only a deleted one is closed', () => {
    const tree = new TreeState(memory({ 'tree.closed': '[42]' }).store);

    expect(tree.allOpen).toBe(false);
    expect(tree.openAmong([0, 1, 3])).toBe(true);
    expect(tree.openAmong([0, 42])).toBe(false);
  });
});
