<!--
  The grid a dashboard's tiles sit on.

  The grid is CSS: as many columns as the dashboard has, rows of the height
  its density asks for, and each tile placed by four custom properties
  (frontend-css.md, the input goes to CSS and never the result). Below the
  width where a row of tiles stops being readable, the tiles stack in reading
  order, one under the other, each as tall as it was.

  While the dashboard is being changed, a tile moves by the pointer or by the
  keyboard (tile-mover.svelte.ts), and each move is a step the person can take
  back. The settings of a tile carry nothing about its place. What moved is
  said in a live region, so a person who cannot see the grid hears where the
  tile landed.
-->
<script lang="ts">
  import { untrack } from 'svelte';

  import type { DashboardCard, DashboardDensity } from '../../../domain';
  import { rowsOf, stackedTiles } from '../../../domain';
  import { t } from '../../../i18n';
  import { dashboardContext } from '../context';
  import TileFrame from './tile-frame.svelte';
  import { TileMover } from './tile-mover.svelte';

  let {
    tiles,
    columns,
    density,
    editing,
    fit = false,
    revealed,
    onsettings,
  }: {
    tiles: readonly DashboardCard[];
    columns: number;
    density: DashboardDensity;
    editing: boolean;
    /** Stretches the rows so the layout fills the height of the screen. */
    fit?: boolean;
    /** A tile just added, scrolled into sight and said in the live region. */
    revealed?: string | undefined;
    /** Opens the settings of one tile. A grid nobody changes needs none. */
    onsettings?: (id: string) => void;
  } = $props();

  const { editor, cards } = dashboardContext();

  let grid = $state<HTMLElement | undefined>(undefined);

  const nameOf = (tile: DashboardCard): string => {
    if (tile.title.trim() !== '') return tile.title;

    return cards.titleOf(tile.type);
  };

  const mover = new TileMover(editor, {
    element: () => grid,
    columns: () => columns,
    editing: () => editing,
    tiles: () => tiles,
    nameOf,
    settings: (id: string) => {
      onsettings?.(id);
    },
  });

  const dragging = $derived(mover.dragging);

  /** Where the tile being dragged or resized will land, drawn under it. */
  const landing = $derived(
    dragging === undefined
      ? undefined
      : tiles.find((tile) => tile.id === dragging.id),
  );

  /** The order a narrow screen stacks the tiles in. */
  const order = $derived(
    new Map(stackedTiles(tiles).map((tile, index) => [tile.id, index])),
  );

  const instructionsId = $props.id();

  $effect(() => () => {
    mover.dispose();
  });

  // Scrolling is a side effect on the page. Only a new tile runs it, never a
  // later move of the tiles.
  $effect(() => {
    const id = revealed;
    if (id !== undefined) {
      untrack(() => {
        mover.reveal(id);
      });
    }
  });
</script>

<div class="board-frame">
  <div
    bind:this={grid}
    data-part="board-grid"
    data-density={density}
    data-editing={editing ? '' : undefined}
    data-fit={fit ? '' : undefined}
    data-dragging={dragging?.mode}
    class="board-grid"
    style="--columns: {columns}; --rows: {Math.max(1, rowsOf(tiles))}"
    role="presentation"
    onpointermove={mover.follow}
    onpointerup={mover.finish}
    onpointercancel={mover.abandon}
  >
    {#if landing !== undefined}
      <div
        data-part="drop-spot"
        aria-hidden="true"
        class="drop-spot rounded-sm border-2 border-dashed border-primary bg-primary/5"
        style="--x: {landing.x}; --y: {landing.y}; --w: {landing.w}; --h: {landing.h}"
      ></div>
    {/if}
    {#each tiles as tile (tile.id)}
      <TileFrame
        {tile}
        {editing}
        name={nameOf(tile)}
        order={order.get(tile.id) ?? 0}
        selected={editing && editor.selected === tile.id}
        dragged={dragging?.id === tile.id ? dragging.mode : undefined}
        offsetX={dragging?.id === tile.id ? mover.offset.x : 0}
        offsetY={dragging?.id === tile.id ? mover.offset.y : 0}
        describedBy={editing ? instructionsId : undefined}
        onkeydown={(event: KeyboardEvent) => {
          mover.keyed(event, tile);
        }}
        ongrab={(event: PointerEvent) => {
          mover.start(event, tile, 'move');
        }}
        onstretch={(event: PointerEvent) => {
          mover.start(event, tile, 'resize');
        }}
        onsettings={() => {
          onsettings?.(tile.id);
        }}
        onremove={() => {
          mover.remove(tile);
        }}
      />
    {/each}
  </div>
</div>

{#if editing}
  <p id={instructionsId} class="sr-only">{t('dashboard.grid.instructions')}</p>
{/if}
<p class="sr-only" aria-live="polite" data-part="grid-said">{mover.said}</p>

<style>
  /* The box the grid measures its own width by. A container query reads the
     box around an element and never the element itself. */
  .board-frame {
    container: board / inline-size;
  }

  .board-grid {
    display: grid;
    grid-template-columns: repeat(var(--columns), minmax(0, 1fr));
    grid-auto-rows: var(--row);
    gap: var(--gap);
    --row: calc(var(--spacing) * 16);
    --gap: calc(var(--spacing) * 3);
  }

  .board-grid[data-density='compact'] {
    --row: calc(var(--spacing) * 12);
    --gap: calc(var(--spacing) * 2);
  }

  .board-grid[data-density='spacious'] {
    --row: calc(var(--spacing) * 20);
    --gap: calc(var(--spacing) * 4);
  }

  /* On a wall the rows share the height of the screen, under the head of
     the wall view, and never shrink below a compact row. The fit sets the
     height of a row for every density, so its selector outranks the density
     rules above; the density still sets the gap. */
  .board-grid[data-density][data-fit] {
    --row: max(
      calc(var(--spacing) * 10),
      calc(
        (100dvh - var(--spacing) * 24 - (var(--rows) - 1) * var(--gap)) /
          var(--rows)
      )
    );
  }

  /* While a tile is being changed the cells are drawn, so a person sees
     where a tile can land. The lines are the theme's border colour at the
     pitch of one cell. */
  .board-grid[data-editing] {
    --line: calc(var(--spacing) * 0.25);
    background-image:
      linear-gradient(to right, var(--border) var(--line), transparent 0),
      linear-gradient(to bottom, var(--border) var(--line), transparent 0);
    background-size: calc((100% + var(--gap)) / var(--columns))
      calc(var(--row) + var(--gap));
    background-position: calc(var(--gap) * -0.5) calc(var(--gap) * -0.5);
    min-block-size: calc((var(--row) + var(--gap)) * 6);
  }

  /* Too narrow for a row of tiles to read: one column, in reading order. */
  @container board (width < 40rem) {
    /* Stacked, a tile keeps its height as a floor and may grow past it, so
       counts that wrap onto a second line on a phone are not cut off. */
    .board-grid {
      grid-template-columns: minmax(0, 1fr);
      grid-auto-rows: minmax(var(--row), auto);
    }

    .board-grid[data-editing] {
      background-image: none;
    }
  }

  .drop-spot {
    grid-column: calc(var(--x) + 1) / span var(--w);
    grid-row: calc(var(--y) + 1) / span var(--h);
  }

  .board-grid[data-dragging] {
    user-select: none;
  }

  .board-grid[data-dragging='move'] {
    cursor: grabbing;
  }

  .board-grid[data-dragging='resize'] {
    cursor: nwse-resize;
  }
</style>
