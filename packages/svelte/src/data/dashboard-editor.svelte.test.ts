import { flushSync } from 'svelte';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { type DashboardCard, MAX_CARDS, MAX_ROWS } from '../domain';
import {
  DashboardEditor,
  type EditedDashboard,
} from './dashboard-editor.svelte';

const tile = (
  id: string,
  x: number,
  y: number,
  w: number,
  h: number,
): DashboardCard => ({
  id,
  type: 'core/note',
  x,
  y,
  w,
  h,
  title: '',
  config: {},
});

const board = (cards: DashboardCard[] = []): EditedDashboard => ({
  id: 3,
  name: 'Workshop',
  visibility: 'everyone',
  columns: 12,
  density: 'comfortable',
  cards,
  revision: 7,
});

/** The least size of each type, as a card catalogue answers it. */
const LEAST: Readonly<Record<string, { w: number; h: number }>> = {
  'core/printer': { w: 2, h: 3 },
  'core/wide': { w: 6, h: 2 },
};
const leastOf = (type: string) => LEAST[type] ?? { w: 1, h: 1 };

const started = (cards: DashboardCard[] = []): DashboardEditor => {
  const editor = new DashboardEditor(leastOf);
  editor.start(board(cards));

  return editor;
};

afterEach(() => {
  vi.useRealTimers();
});

describe('the editor', () => {
  it('starts clean, from the dashboard it was given', () => {
    const editor = started([tile('a', 0, 0, 4, 2)]);

    expect(editor.editing).toBe(true);
    expect(editor.dashboard).toBe(3);
    expect(editor.dirty).toBe(false);
    expect(editor.canUndo).toBe(false);
    expect(editor.cards.map((held) => held.id)).toEqual(['a']);
  });

  it('adds a tile where it first fits, picks it, and can take it back', () => {
    const editor = started([tile('a', 0, 0, 6, 2)]);
    const id = editor.add('core/clock', { w: 3, h: 2 });

    expect(id).toBeDefined();
    expect(editor.selected).toBe(id);
    expect(editor.card(id ?? '')).toMatchObject({
      type: 'core/clock',
      x: 6,
      y: 0,
      w: 3,
      h: 2,
    });
    expect(editor.dirty).toBe(true);

    editor.undo();
    expect(editor.cards).toHaveLength(1);
    expect(editor.dirty).toBe(false);
    expect(editor.selected).toBeUndefined();

    editor.redo();
    expect(editor.cards).toHaveLength(2);
  });

  it('refuses a tile over the limit the module keeps', () => {
    const many = Array.from({ length: MAX_CARDS }, (_, index) =>
      tile(`t${String(index)}`, 0, index, 1, 1),
    );
    const editor = started(many);

    expect(editor.add('core/note', { w: 1, h: 1 })).toBeUndefined();
    expect(editor.cards).toHaveLength(MAX_CARDS);
  });

  it('writes no step for a change that changes nothing', () => {
    const editor = started([tile('a', 0, 0, 4, 2)]);

    expect(editor.move('a', 0, 0)).toBe(false);
    expect(editor.setColumns(12)).toBe(false);
    expect(editor.setDensity('comfortable')).toBe(false);
    expect(editor.rename('Workshop')).toBe(false);
    expect(editor.configure('a', {})).toBe(false);
    expect(editor.canUndo).toBe(false);
  });

  it('refuses a name the module would refuse, and keeps the one it had', () => {
    const editor = started();

    expect(editor.rename('   ')).toBe(false);
    expect(editor.rename('x'.repeat(81))).toBe(false);
    expect(editor.rename('ž'.repeat(41))).toBe(false);
    expect(editor.draft?.name).toBe('Workshop');
    expect(editor.rename('Wall')).toBe(true);
    expect(editor.draft?.name).toBe('Wall');
  });

  it('never makes a tile smaller than its kind can be read at', () => {
    const editor = started([
      { ...tile('p', 0, 0, 4, 5), type: 'core/printer' },
    ]);

    editor.resize('p', 1, 1);
    expect(editor.card('p')).toMatchObject({ w: 2, h: 3 });
  });

  it('keeps each tile its share of the width when the grid changes', () => {
    const editor = started([tile('a', 0, 0, 6, 2), tile('b', 6, 0, 6, 2)]);

    editor.setColumns(24);
    expect(editor.cards.map((held) => [held.x, held.w])).toEqual([
      [0, 12],
      [12, 12],
    ]);
  });

  it('keeps a tile its least size when the grid gets narrower', () => {
    const editor = started([
      { ...tile('w', 0, 0, 3, 1), type: 'core/wide' },
      tile('n', 3, 0, 3, 1),
    ]);

    expect(editor.setColumns(4)).toBe(true);
    // 'core/wide' needs 6 of 12 columns and 2 rows, which is 2 of 4 columns.
    expect(editor.card('w')).toMatchObject({ w: 2, h: 2 });
    expect(editor.card('n')).toMatchObject({ w: 1, h: 1 });
  });

  it('refuses a move that pushes a tile past the last row, as a step and as a preview', () => {
    const column = Array.from({ length: MAX_ROWS / 32 }, (_, index) =>
      tile(`c${String(index)}`, 0, index * 32, 1, 32),
    );
    const editor = started([...column, tile('x', 1, 0, 1, 1)]);

    expect(editor.move('x', 0, 0)).toBe(false);
    expect(editor.canUndo).toBe(false);
    expect(editor.card('x')).toMatchObject({ x: 1, y: 0 });

    editor.previewMove('x', 0, 0);
    expect(editor.cards).toBe(editor.draft?.cards);
  });

  it('keeps a hundred steps back', () => {
    const editor = started();
    for (let index = 0; index < 150; index++) {
      editor.rename(`Wall ${String(index)}`);
    }

    let back = 0;
    while (editor.canUndo) {
      editor.undo();
      back += 1;
    }

    expect(back).toBe(100);
    expect(editor.draft?.name).toBe('Wall 49');
  });

  it('makes typing into one tile one step back', () => {
    vi.useFakeTimers();
    const editor = started([tile('a', 0, 0, 4, 2)]);

    editor.configure('a', { text: 'H' });
    editor.configure('a', { text: 'He' });
    editor.configure('a', { text: 'Hello' });
    expect(editor.card('a')?.config).toEqual({ text: 'Hello' });

    editor.undo();
    expect(editor.card('a')?.config).toEqual({});
  });

  it('starts a new step once the person paused', () => {
    vi.useFakeTimers();
    const editor = started([tile('a', 0, 0, 4, 2)]);

    editor.configure('a', { text: 'one' });
    vi.advanceTimersByTime(2000);
    editor.configure('a', { text: 'two' });
    editor.undo();
    expect(editor.card('a')?.config).toEqual({ text: 'one' });
  });

  it('shows a drag without a step, and takes it as one on commit', () => {
    const editor = started([tile('a', 0, 0, 4, 2), tile('b', 0, 2, 4, 2)]);

    editor.previewMove('b', 0, 0);
    expect(editor.cards).not.toBe(editor.draft?.cards);
    expect(editor.canUndo).toBe(false);
    expect(editor.cards.find((held) => held.id === 'b')?.y).toBe(0);

    editor.commit();
    expect(editor.cards).toBe(editor.draft?.cards);
    expect(editor.card('b')?.y).toBe(0);
    expect(editor.card('a')?.y).toBe(2);

    editor.undo();
    expect(editor.card('b')?.y).toBe(2);
  });

  it('drops a drag that was abandoned', () => {
    const editor = started([tile('a', 0, 0, 4, 2), tile('b', 0, 2, 4, 2)]);

    editor.previewMove('b', 0, 0);
    editor.cancelPreview();
    expect(editor.card('b')?.y).toBe(2);
    expect(editor.canUndo).toBe(false);
  });

  it('sends the draft against the revision it started from', () => {
    const editor = started([tile('a', 0, 0, 4, 2)]);
    editor.rename(' Wall ');

    expect(editor.request()).toMatchObject({
      name: 'Wall',
      revision: 7,
    });
  });

  it('forgets everything when it stops', () => {
    const editor = started([tile('a', 0, 0, 4, 2)]);
    editor.remove('a');
    editor.stop();

    expect(editor.editing).toBe(false);
    expect(editor.draft).toBeUndefined();
    expect(editor.cards).toEqual([]);
    expect(editor.dirty).toBe(false);
    expect(editor.move('a', 1, 1)).toBe(false);
  });

  it('recomputes what reads the tiles once for one step, and not at all for none', () => {
    const editor = started([tile('a', 0, 0, 4, 2)]);
    let runs = 0;
    let seen: readonly DashboardCard[] = [];
    const stop = $effect.root(() => {
      $effect(() => {
        seen = editor.cards;
        runs += 1;
      });
    });
    flushSync();
    runs = 0;

    editor.move('a', 0, 0);
    flushSync();
    expect(runs).toBe(0);

    editor.move('a', 4, 0);
    flushSync();
    expect(runs).toBe(1);

    expect(seen).toBe(editor.cards);
    stop();
  });
});
