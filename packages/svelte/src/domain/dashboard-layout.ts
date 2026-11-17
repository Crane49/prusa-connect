// Where the tiles of a dashboard sit on its grid, and how they make room for
// each other.
//
// A tile covers a rectangle of whole cells: `x` and `y` are its first column
// and row, counted from 0, and `w` and `h` are how many it covers. Two tiles
// never cover the same cell once a layout has been settled here.
//
// Settling is gravity: every tile rises until it meets the top of the grid or
// a tile above it. That is what a person expects when they move one tile out
// of the way and the gap it leaves closes. The tile being moved is settled
// first at the place it was dropped, so it keeps that place and the others
// move around it.
//
// Every function is pure and answers a new array. The editor keeps the old
// one for its undo, and a test reads the answer against literals.

/** The part of a tile the layout reads and writes. */
export interface Placed {
  readonly id: string;
  readonly x: number;
  readonly y: number;
  readonly w: number;
  readonly h: number;
}

/** The most rows one tile may cover, as the module holds it. */
export const MAX_TILE_ROWS = 32;

/** The last row a tile may reach, as the module holds it. */
export const MAX_ROWS = 512;

/** The fewest and the most columns a grid may have. */
export const MIN_COLUMNS = 4;
export const MAX_COLUMNS = 24;

/** True when two tiles share a cell. A tile never meets itself. */
export const tilesOverlap = (a: Placed, b: Placed): boolean =>
  a.id !== b.id &&
  a.x < b.x + b.w &&
  b.x < a.x + a.w &&
  a.y < b.y + b.h &&
  b.y < a.y + a.h;

/** Holds one tile inside the grid: at least one cell, never past the edge. */
export const clampTile = <T extends Placed>(tile: T, columns: number): T => {
  const w = Math.min(Math.max(1, Math.round(tile.w)), columns);
  const h = Math.min(Math.max(1, Math.round(tile.h)), MAX_TILE_ROWS);
  const x = Math.min(Math.max(0, Math.round(tile.x)), columns - w);
  const y = Math.min(Math.max(0, Math.round(tile.y)), MAX_ROWS - h);

  if (x === tile.x && y === tile.y && w === tile.w && h === tile.h) return tile;

  return { ...tile, x, y, w, h };
};

/** The order tiles are read in: by row, then by column. */
const reading = (a: Placed, b: Placed): number => a.y - b.y || a.x - b.x;

/**
 * Settles a layout. Tiles are placed in reading order, each at the highest row
 * where it meets nothing already placed, and never lower than it was. The tile
 * named `first` is placed before every other one, at the row it asks for.
 *
 * A tile pushed down below others can end past `MAX_ROWS`, which the core
 * refuses. The answer then fails `layoutFits`, and the editor does not take
 * it as a step.
 */
export const settleTiles = <T extends Placed>(
  tiles: readonly T[],
  columns: number,
  first?: string,
): T[] => {
  const held = tiles.map((tile) => clampTile(tile, columns));
  const order = [...held].sort((a, b) => {
    if (a.id === first) return -1;
    if (b.id === first) return 1;

    return reading(a, b);
  });

  const placed: T[] = [];
  const at = new Map<string, T>();

  for (const tile of order) {
    let next = tile;
    if (tile.id !== first) {
      // Rise while the row above is free, then fall below anything this tile
      // still meets. The rise looks through the tile that was dropped, so a
      // tile it was dropped onto can move up into the place it left, which
      // is how two tiles swap. The fall is what pushes a tile out from under
      // it when there is no such place.
      const above = placed.filter((other) => other.id !== first);
      while (next.y > 0 && !meetsAny({ ...next, y: next.y - 1 }, above)) {
        next = { ...next, y: next.y - 1 };
      }
      while (meetsAny(next, placed)) {
        next = { ...next, y: next.y + 1 };
      }
    }
    placed.push(next);
    at.set(next.id, next);
  }

  // The answer keeps the order the tiles came in, so a list the page draws
  // keeps its keys in place and only the positions move.
  return held.map((tile) => at.get(tile.id) ?? tile);
};

const meetsAny = (tile: Placed, others: readonly Placed[]): boolean =>
  others.some((other) => tilesOverlap(tile, other));

/**
 * True when the core would take this layout: every tile inside the columns
 * and above `MAX_ROWS`, and no two tiles on one cell.
 */
export const layoutFits = (
  tiles: readonly Placed[],
  columns: number,
): boolean =>
  tiles.every(
    (tile, index) =>
      tile.x >= 0 &&
      tile.y >= 0 &&
      tile.x + tile.w <= columns &&
      tile.y + tile.h <= MAX_ROWS &&
      !meetsAny(tile, tiles.slice(index + 1)),
  );

/**
 * Settles a layout around one tile that was just moved or resized. The tile
 * keeps its place while the others make room, and then the whole layout rises
 * once more, so the tile does not hang under a gap above it.
 */
const settleAround = <T extends Placed>(
  tiles: readonly T[],
  id: string,
  columns: number,
): T[] => settleTiles(settleTiles(tiles, columns, id), columns);

/** Moves one tile to a cell, and settles every other one around it. */
export const moveTile = <T extends Placed>(
  tiles: readonly T[],
  id: string,
  x: number,
  y: number,
  columns: number,
): T[] =>
  settleAround(
    tiles.map((tile) =>
      tile.id === id ? clampTile({ ...tile, x, y }, columns) : tile,
    ),
    id,
    columns,
  );

/** Gives one tile a new size, and settles every other one around it. */
export const resizeTile = <T extends Placed>(
  tiles: readonly T[],
  id: string,
  w: number,
  h: number,
  columns: number,
): T[] =>
  settleAround(
    tiles.map((tile) =>
      tile.id === id ? clampTile({ ...tile, w, h }, columns) : tile,
    ),
    id,
    columns,
  );

/**
 * The first cell a tile of one size fits at without meeting another, read
 * row by row. A grid with no such cell answers the row under every tile.
 */
export const freeCell = (
  tiles: readonly Placed[],
  w: number,
  h: number,
  columns: number,
): { x: number; y: number } => {
  const width = Math.min(Math.max(1, w), columns);
  const bottom = tiles.reduce((low, tile) => Math.max(low, tile.y + tile.h), 0);

  for (let y = 0; y <= bottom; y++) {
    for (let x = 0; x + width <= columns; x++) {
      const probe = { id: '', x, y, w: width, h };
      if (!meetsAny(probe, tiles)) return { x, y };
    }
  }

  return { x: 0, y: bottom };
};

/** How many rows the layout takes, which is how tall the grid is drawn. */
export const rowsOf = (tiles: readonly Placed[]): number =>
  tiles.reduce((low, tile) => Math.max(low, tile.y + tile.h), 0);

/**
 * Fits a layout made for one width of grid into another. A tile keeps its
 * share of the width, so a tile across half of 12 columns covers half of 24.
 * A tile never ends smaller than `leastOf` answers for it, in cells of the
 * new grid, so a narrower grid does not shrink a card below what it can be
 * read at.
 */
export const refitTiles = <T extends Placed>(
  tiles: readonly T[],
  from: number,
  to: number,
  leastOf: (tile: T) => { readonly w: number; readonly h: number } = () => ({
    w: 1,
    h: 1,
  }),
): T[] => {
  if (from === to) return [...tiles];

  const scale = to / from;

  return settleTiles(
    tiles.map((tile) => {
      const least = leastOf(tile);

      return clampTile(
        {
          ...tile,
          x: Math.floor(tile.x * scale),
          w: Math.max(1, least.w, Math.round(tile.w * scale)),
          h: Math.max(least.h, tile.h),
        },
        to,
      );
    }),
    to,
  );
};

/** The tiles in the order a narrow screen stacks them: by row, then by column. */
export const stackedTiles = <T extends Placed>(tiles: readonly T[]): T[] =>
  [...tiles].sort(reading);
