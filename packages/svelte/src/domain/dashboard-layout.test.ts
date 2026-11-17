import { describe, expect, it } from 'vitest';

import {
  clampTile,
  freeCell,
  layoutFits,
  MAX_ROWS,
  moveTile,
  tilesOverlap,
  type Placed,
  refitTiles,
  resizeTile,
  rowsOf,
  settleTiles,
  stackedTiles,
} from './dashboard-layout';

const tile = (
  id: string,
  x: number,
  y: number,
  w: number,
  h: number,
): Placed => ({
  id,
  x,
  y,
  w,
  h,
});

/** The layout as `id@x,y wxh`, sorted by id, so a case reads as one line. */
const shape = (tiles: readonly Placed[]): string =>
  [...tiles]
    .sort((a, b) => a.id.localeCompare(b.id))
    .map(
      (t) =>
        `${t.id}@${String(t.x)},${String(t.y)} ${String(t.w)}x${String(t.h)}`,
    )
    .join(' ');

/** No two tiles of a settled layout share a cell. */
const settled = (tiles: readonly Placed[]): boolean =>
  tiles.every((a) => tiles.every((b) => !tilesOverlap(a, b)));

describe('overlaps', () => {
  it('tells tiles that share a cell from tiles that only touch', () => {
    expect(tilesOverlap(tile('a', 0, 0, 2, 2), tile('b', 1, 1, 2, 2))).toBe(
      true,
    );
    expect(tilesOverlap(tile('a', 0, 0, 2, 2), tile('b', 2, 0, 2, 2))).toBe(
      false,
    );
    expect(tilesOverlap(tile('a', 0, 0, 2, 2), tile('b', 0, 2, 2, 2))).toBe(
      false,
    );
  });

  it('never counts a tile against itself', () => {
    const one = tile('a', 0, 0, 2, 2);
    expect(tilesOverlap(one, one)).toBe(false);
  });
});

describe('clamp', () => {
  it('holds a tile inside the grid and at least one cell large', () => {
    expect(clampTile(tile('a', 10, -3, 6, 0), 12)).toEqual(
      tile('a', 6, 0, 6, 1),
    );
    expect(clampTile(tile('a', 0, 0, 40, 99), 12)).toEqual(
      tile('a', 0, 0, 12, 32),
    );
  });

  it('answers the same object for a tile already inside', () => {
    const inside = tile('a', 1, 1, 2, 2);
    expect(clampTile(inside, 12)).toBe(inside);
  });
});

describe('settle', () => {
  it('lifts every tile until it meets the top or a tile above it', () => {
    const answer = settleTiles(
      [tile('a', 0, 3, 4, 2), tile('b', 0, 9, 4, 2), tile('c', 6, 5, 2, 2)],
      12,
    );
    expect(shape(answer)).toBe('a@0,0 4x2 b@0,2 4x2 c@6,0 2x2');
  });

  it('pushes apart tiles that share cells', () => {
    const answer = settleTiles(
      [tile('a', 0, 0, 4, 2), tile('b', 2, 1, 4, 2)],
      12,
    );
    expect(shape(answer)).toBe('a@0,0 4x2 b@2,2 4x2');
    expect(settled(answer)).toBe(true);
  });

  it('keeps the order the tiles came in', () => {
    const answer = settleTiles(
      [tile('z', 0, 4, 1, 1), tile('a', 0, 0, 1, 1)],
      12,
    );
    expect(answer.map((t) => t.id)).toEqual(['z', 'a']);
  });
});

describe('move', () => {
  it('swaps two tiles of one column when one is dropped onto the other', () => {
    const start = [tile('a', 0, 0, 4, 2), tile('b', 0, 2, 4, 2)];

    expect(shape(moveTile(start, 'b', 0, 0, 12))).toBe('a@0,2 4x2 b@0,0 4x2');
    expect(shape(moveTile(start, 'a', 0, 2, 12))).toBe('a@0,2 4x2 b@0,0 4x2');
  });

  it('leaves the order alone for a drag shorter than the tile it passes', () => {
    const start = [tile('a', 0, 0, 4, 2), tile('b', 0, 2, 4, 2)];

    expect(shape(moveTile(start, 'a', 0, 1, 12))).toBe('a@0,0 4x2 b@0,2 4x2');
  });

  it('closes the gap a tile leaves and lets a dropped tile rise', () => {
    const start = [
      tile('a', 0, 0, 4, 2),
      tile('b', 0, 2, 4, 2),
      tile('c', 0, 4, 4, 2),
    ];
    const answer = moveTile(start, 'a', 8, 6, 12);

    expect(shape(answer)).toBe('a@8,0 4x2 b@0,0 4x2 c@0,2 4x2');
    expect(settled(answer)).toBe(true);
  });

  it('pushes down whatever a wide tile is dropped across', () => {
    const start = [
      tile('a', 0, 0, 3, 2),
      tile('b', 3, 0, 3, 2),
      tile('wide', 0, 2, 6, 2),
    ];
    const answer = moveTile(start, 'wide', 0, 0, 12);

    expect(shape(answer)).toBe('a@0,2 3x2 b@3,2 3x2 wide@0,0 6x2');
    expect(settled(answer)).toBe(true);
  });

  it('holds a tile dropped past the edge inside the grid', () => {
    const answer = moveTile([tile('a', 0, 0, 4, 2)], 'a', 11, 0, 12);
    expect(shape(answer)).toBe('a@8,0 4x2');
  });
});

describe('resize', () => {
  it('pushes the tile below down as one above grows', () => {
    const answer = resizeTile(
      [tile('a', 0, 0, 4, 2), tile('b', 0, 2, 4, 2)],
      'a',
      4,
      5,
      12,
    );
    expect(shape(answer)).toBe('a@0,0 4x5 b@0,5 4x2');
  });

  it('lets the tile below rise as one above shrinks', () => {
    const answer = resizeTile(
      [tile('a', 0, 0, 4, 5), tile('b', 0, 5, 4, 2)],
      'a',
      4,
      2,
      12,
    );
    expect(shape(answer)).toBe('a@0,0 4x2 b@0,2 4x2');
  });

  it('never grows a tile past the edge', () => {
    const answer = resizeTile([tile('a', 8, 0, 4, 2)], 'a', 9, 2, 12);
    expect(shape(answer)).toBe('a@3,0 9x2');
  });
});

describe('freeCell', () => {
  it('answers the first cell a tile fits at, read row by row', () => {
    const tiles = [tile('a', 0, 0, 6, 2), tile('b', 6, 0, 4, 2)];
    expect(freeCell(tiles, 2, 2, 12)).toEqual({ x: 10, y: 0 });
    expect(freeCell(tiles, 4, 2, 12)).toEqual({ x: 0, y: 2 });
  });

  it('answers the top corner of an empty grid', () => {
    expect(freeCell([], 4, 3, 12)).toEqual({ x: 0, y: 0 });
  });
});

describe('refit', () => {
  it('keeps the share of the width each tile had', () => {
    const answer = refitTiles(
      [tile('a', 0, 0, 6, 2), tile('b', 6, 0, 6, 2)],
      12,
      24,
    );
    expect(shape(answer)).toBe('a@0,0 12x2 b@12,0 12x2');
  });

  it('settles tiles that would meet in a narrower grid', () => {
    const answer = refitTiles(
      [tile('a', 0, 0, 5, 2), tile('b', 5, 0, 7, 2)],
      12,
      4,
    );
    expect(settled(answer)).toBe(true);
    expect(answer.every((t) => t.x + t.w <= 4)).toBe(true);
  });
});

describe('refit and the least size of a card', () => {
  it('never makes a tile narrower or shorter than its least size in the new grid', () => {
    const answer = refitTiles(
      [tile('wide', 0, 0, 3, 1), tile('free', 3, 0, 3, 1)],
      12,
      4,
      (t) => (t.id === 'wide' ? { w: 2, h: 3 } : { w: 1, h: 1 }),
    );

    expect(shape(answer)).toBe('free@1,3 1x1 wide@0,0 2x3');
  });

  it('holds a least size wider than the grid to the grid', () => {
    const answer = refitTiles([tile('a', 0, 0, 12, 1)], 12, 4, () => ({
      w: 6,
      h: 1,
    }));

    expect(shape(answer)).toBe('a@0,0 4x1');
  });
});

describe('layoutFits', () => {
  it('takes a layout inside the grid with no two tiles on one cell', () => {
    expect(layoutFits([tile('a', 0, 0, 2, 2), tile('b', 2, 0, 2, 2)], 4)).toBe(
      true,
    );
    expect(layoutFits([], 4)).toBe(true);
  });

  it('refuses tiles on one cell, past the edge, or past the last row', () => {
    expect(layoutFits([tile('a', 0, 0, 2, 2), tile('b', 1, 1, 2, 2)], 4)).toBe(
      false,
    );
    expect(layoutFits([tile('a', 3, 0, 2, 1)], 4)).toBe(false);
    expect(layoutFits([tile('a', 0, MAX_ROWS - 1, 1, 2)], 4)).toBe(false);
    expect(layoutFits([tile('a', 0, MAX_ROWS - 2, 1, 2)], 4)).toBe(true);
  });

  it('refuses a settled layout that pushed a tile past the last row', () => {
    const column = Array.from({ length: MAX_ROWS / 32 }, (_, index) =>
      tile(`c${String(index)}`, 0, index * 32, 1, 32),
    );
    const answer = moveTile([...column, tile('x', 1, 0, 1, 1)], 'x', 0, 0, 4);

    expect(rowsOf(answer)).toBe(MAX_ROWS + 1);
    expect(layoutFits(answer, 4)).toBe(false);
  });
});

describe('rowsOf and stacked', () => {
  it('measures the layout and reads it by row', () => {
    const tiles = [
      tile('b', 4, 0, 2, 3),
      tile('c', 0, 2, 2, 4),
      tile('a', 0, 0, 2, 2),
    ];

    expect(rowsOf(tiles)).toBe(6);
    expect(stackedTiles(tiles).map((t) => t.id)).toEqual(['a', 'b', 'c']);
    expect(rowsOf([])).toBe(0);
  });
});
