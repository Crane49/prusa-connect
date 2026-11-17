// How a person moves the tiles of the grid while the dashboard is changed.
// The grid draws the tiles, and this moves them, by the pointer or by the
// keyboard. Each grid builds its own, so it is a class and not a factory
// (svelte-5.md).
//
//   the pointer   drag the grip to move a tile, drag the corner to resize it.
//                 The others make room while the pointer moves, and the tile
//                 follows the pointer between cells. Escape, a cancelled
//                 pointer or a grip that loses the pointer drops the drag
//   the keyboard  the arrow keys move the tile that has focus by one cell,
//                 and with Shift they resize it. Delete removes it, Enter
//                 opens its settings
//
// What moved is written to `said`, for a live region.
import { tick } from 'svelte';

import type { DashboardEditor } from '../../../data';
import type { DashboardCard } from '../../../domain';
import { stackedTiles } from '../../../domain';
import { t } from '../../../i18n';

export type DragMode = 'move' | 'resize';

/** What the mover reads of the grid it moves tiles on. */
export interface MoverGrid {
  /** The grid element, once it is drawn. */
  element(): HTMLElement | undefined;
  columns(): number;
  editing(): boolean;
  tiles(): readonly DashboardCard[];
  nameOf(tile: DashboardCard): string;
  /** Opens the settings of one tile. */
  settings(id: string): void;
}

export type MoverEditor = Pick<
  DashboardEditor,
  | 'selected'
  | 'cards'
  | 'card'
  | 'previewMove'
  | 'previewResize'
  | 'commit'
  | 'cancelPreview'
  | 'move'
  | 'resize'
  | 'remove'
>;

/** A drag in progress: what is dragged, from where, and the pitch of a cell. */
interface Drag {
  readonly id: string;
  readonly mode: DragMode;
  readonly fromX: number;
  readonly fromY: number;
  readonly origin: { x: number; y: number; w: number; h: number };
  readonly pitchX: number;
  readonly pitchY: number;
}

const STEP: Readonly<Record<string, readonly [number, number]>> = {
  ArrowLeft: [-1, 0],
  ArrowRight: [1, 0],
  ArrowUp: [0, -1],
  ArrowDown: [0, 1],
};

const NO_OFFSET = { x: 0, y: 0 } as const;

export class TileMover {
  readonly #editor: MoverEditor;
  readonly #grid: MoverGrid;
  #drag = $state.raw<Drag | undefined>(undefined);
  #offset = $state.raw<{ readonly x: number; readonly y: number }>(NO_OFFSET);
  #said = $state('');

  constructor(editor: MoverEditor, grid: MoverGrid) {
    this.#editor = editor;
    this.#grid = grid;
  }

  /** The tile being dragged and how. Read-only: only a drag changes it. */
  get dragging(): { readonly id: string; readonly mode: DragMode } | undefined {
    return this.#drag;
  }

  /** How far the pointer is from where the dragged tile is drawn, in pixels. */
  get offset(): { readonly x: number; readonly y: number } {
    return this.#offset;
  }

  /** The last thing that moved, in words. Read-only: a move writes it. */
  get said(): string {
    return this.#said;
  }

  start(event: PointerEvent, tile: DashboardCard, mode: DragMode): void {
    if (!this.#grid.editing() || event.button !== 0 || this.#isStacked()) {
      return;
    }
    const cells = this.#pitch();
    if (cells === undefined) return;
    event.preventDefault();
    if (event.currentTarget instanceof Element) {
      event.currentTarget.setPointerCapture(event.pointerId);
    }

    this.#editor.selected = tile.id;
    this.#drag = {
      id: tile.id,
      mode,
      fromX: event.clientX,
      fromY: event.clientY,
      origin: { x: tile.x, y: tile.y, w: tile.w, h: tile.h },
      pitchX: cells.x,
      pitchY: cells.y,
    };
    this.#offset = NO_OFFSET;
    // Escape is read from the window, because focus may be anywhere: Safari
    // does not focus a button a pointer presses. A grip that unmounts loses
    // the pointer, and the browser says so on the document.
    window.addEventListener('keydown', this.#escape);
    window.addEventListener('lostpointercapture', this.abandon);
  }

  follow = (event: PointerEvent): void => {
    const held = this.#drag;
    if (held === undefined) return;

    const dx = event.clientX - held.fromX;
    const dy = event.clientY - held.fromY;
    const cellsX = Math.round(dx / held.pitchX);
    const cellsY = Math.round(dy / held.pitchY);

    if (held.mode === 'resize') {
      this.#editor.previewResize(
        held.id,
        held.origin.w + cellsX,
        held.origin.h + cellsY,
      );

      return;
    }

    this.#editor.previewMove(
      held.id,
      held.origin.x + cellsX,
      held.origin.y + cellsY,
    );
    const landed = this.#editor.cards.find((tile) => tile.id === held.id);
    this.#offset =
      landed === undefined
        ? NO_OFFSET
        : {
            x: dx - (landed.x - held.origin.x) * held.pitchX,
            y: dy - (landed.y - held.origin.y) * held.pitchY,
          };
  };

  finish = (): void => {
    const held = this.#drag;
    if (held === undefined) return;
    this.#end();
    this.#editor.commit();
    const landed = this.#editor.card(held.id);
    if (landed !== undefined) this.#announce(landed, held.mode);
  };

  /** Drops the drag in progress, and the place the preview showed. */
  abandon = (): void => {
    if (this.#drag === undefined) return;
    this.#end();
    this.#editor.cancelPreview();
  };

  keyed(event: KeyboardEvent, tile: DashboardCard): void {
    if (!this.#grid.editing() || event.target !== event.currentTarget) return;

    const by = STEP[event.key];
    if (by !== undefined) {
      event.preventDefault();
      const [x, y] = by;
      const mode: DragMode = event.shiftKey ? 'resize' : 'move';
      if (mode === 'resize') {
        this.#editor.resize(tile.id, tile.w + x, tile.h + y);
      } else {
        this.#editor.move(tile.id, tile.x + x, tile.y + y);
      }
      const landed = this.#editor.card(tile.id);
      if (landed !== undefined) this.#announce(landed, mode);

      return;
    }

    if (event.key === 'Delete' || event.key === 'Backspace') {
      event.preventDefault();
      this.remove(tile);

      return;
    }

    if (event.key === 'Enter') {
      event.preventDefault();
      this.#grid.settings(tile.id);
    }
  }

  /**
   * Removes one tile and moves the focus to the tile after it in reading
   * order, or the one before it, so a keyboard is never left on the page's
   * body with nowhere to go.
   */
  remove(tile: DashboardCard): void {
    const order = stackedTiles(this.#grid.tiles());
    const at = order.findIndex((held) => held.id === tile.id);
    const next = order[at + 1] ?? order[at - 1];

    this.#editor.remove(tile.id);
    this.#said = t('dashboard.grid.removed', {
      name: this.#grid.nameOf(tile),
    });

    const host = this.#grid.element();
    if (next === undefined || host === undefined) return;
    void tick().then(() => {
      host
        .querySelector<HTMLElement>(
          `[data-tile="${CSS.escape(next.id)}"] [role='group']`,
        )
        ?.focus();
    });
  }

  /**
   * Scrolls a tile just added into sight and says where it landed, because
   * the first free place is often below the fold. A person who asked for less
   * motion gets the jump without the glide.
   */
  reveal(id: string): void {
    const tile = this.#grid.tiles().find((held) => held.id === id);
    if (tile === undefined) return;
    this.#said = t('dashboard.grid.added', {
      name: this.#grid.nameOf(tile),
      column: tile.x + 1,
      row: tile.y + 1,
    });

    const host = this.#grid.element();
    if (host === undefined) return;
    void tick().then(() => {
      const still = globalThis.matchMedia(
        '(prefers-reduced-motion: reduce)',
      ).matches;
      host
        .querySelector<HTMLElement>(`[data-tile="${CSS.escape(id)}"]`)
        ?.scrollIntoView({
          block: 'nearest',
          behavior: still ? 'auto' : 'smooth',
        });
    });
  }

  /** Lets go of the window when the grid goes, in the middle of a drag or not. */
  dispose(): void {
    this.abandon();
  }

  #escape = (event: KeyboardEvent): void => {
    if (event.key !== 'Escape') return;
    event.preventDefault();
    this.abandon();
  };

  #end(): void {
    this.#drag = undefined;
    this.#offset = NO_OFFSET;
    window.removeEventListener('keydown', this.#escape);
    window.removeEventListener('lostpointercapture', this.abandon);
  }

  #announce(tile: DashboardCard, mode: DragMode): void {
    const name = this.#grid.nameOf(tile);
    this.#said =
      mode === 'move'
        ? t('dashboard.grid.moved', {
            name,
            column: tile.x + 1,
            row: tile.y + 1,
          })
        : t('dashboard.grid.resized', {
            name,
            width: tile.w,
            height: tile.h,
          });
  }

  /**
   * The pitch of one cell, in pixels. Only the layout knows it, and the drag
   * needs it to turn a pointer's travel into cells (frontend-css.md, when JS
   * is really required). It is read once, when the drag starts.
   */
  #pitch(): { x: number; y: number } | undefined {
    const grid = this.#grid.element();
    if (grid === undefined) return undefined;
    const columns = this.#grid.columns();
    const style = getComputedStyle(grid);
    const gapX = Number.parseFloat(style.columnGap) || 0;
    const gapY = Number.parseFloat(style.rowGap) || 0;
    const row = Number.parseFloat(style.gridAutoRows) || 0;
    const width = grid.getBoundingClientRect().width;
    const cell = (width - gapX * (columns - 1)) / columns;

    return { x: cell + gapX, y: row + gapY };
  }

  /** True while the grid stacks its tiles, where a drag has no columns to land in. */
  #isStacked(): boolean {
    const grid = this.#grid.element();

    return (
      grid !== undefined &&
      getComputedStyle(grid).gridTemplateColumns.split(' ').length === 1
    );
  }
}
