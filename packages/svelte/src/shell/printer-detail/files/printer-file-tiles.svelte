<!-- The same listing as a grid of tiles. -->
<script lang="ts">
  import type { LucideIcon } from '@lucide/svelte';
  import type { Snippet } from 'svelte';

  import { formatBytes, formatDateTime, t } from '../../../i18n';
  import {
    ClippedName,
    SelectCheckbox,
    type Selection,
    Tile,
  } from '../../../primitives';
  import type { PrinterFileEntry } from './types';

  let {
    rows,
    name,
    glyph,
    thumbnail,
    shows,
    selection,
    onOpen,
    actions,
  }: {
    rows: readonly PrinterFileEntry[];
    /** What a person reads the entry as, which is not what the printer addresses it by. */
    name: (entry: PrinterFileEntry) => string;
    glyph: (entry: PrinterFileEntry) => LucideIcon;
    /** The plate the file was sliced from, absent for one this server never sent. */
    thumbnail: (entry: PrinterFileEntry) => string | undefined;
    shows: (field: string) => boolean;
    selection: Selection<string>;
    onOpen: (path: string) => void;
    actions: Snippet<[PrinterFileEntry]>;
  } = $props();

  // The thumbnail route answers 404 for a copy that carries no picture, and
  // nothing in the listing says which copies do, so a tile draws the image and
  // falls back to its glyph on the failure. Keyed by the printer's own path,
  // which is what the listing keys a tile by as well.
  const broken = $state<Record<string, boolean>>({});

  const facts = (entry: PrinterFileEntry): string =>
    [
      shows('size') && !entry.folder ? formatBytes(entry.size) : undefined,
      shows('modified') && entry.modified !== undefined
        ? formatDateTime(entry.modified)
        : undefined,
    ]
      .filter((value) => value !== undefined)
      .join(' · ');
</script>

<div
  class="grid grid-cols-2 gap-3 @xl/printer-file-listing:grid-cols-3 @4xl/printer-file-listing:grid-cols-4 @6xl/printer-file-listing:grid-cols-6"
>
  {#each rows as entry (entry.path)}
    {@const Glyph = glyph(entry)}
    {@const plate = broken[entry.path] === true ? undefined : thumbnail(entry)}
    <Tile
      data-part="printer-file-tile"
      size="sm"
      selected={selection.has(entry.path)}
      onclick={entry.folder ? () => onOpen(entry.path) : undefined}
    >
      {#snippet top()}
        <div class="flex items-start justify-between gap-1">
          <span class="flex size-11 shrink-0 items-center justify-center">
            <SelectCheckbox
              {selection}
              key={entry.path}
              label={t('printerDetailFiles.select.row', {
                name: name(entry),
              })}
            />
          </span>
          {@render actions(entry)}
        </div>
      {/snippet}
      <span class="flex min-w-0 flex-col gap-2">
        <span
          class="flex aspect-square w-full items-center justify-center overflow-hidden rounded-sm bg-surface"
        >
          {#if plate === undefined}
            <Glyph aria-hidden="true" class="size-12 text-muted-foreground" />
          {:else}
            <!-- Decoration: the name sits below it in the tile. -->
            <img
              src={plate}
              alt=""
              class="size-full object-cover"
              onerror={() => {
                broken[entry.path] = true;
              }}
            />
          {/if}
        </span>
        <ClippedName name={name(entry)} class="w-full min-w-0 text-xs" />
        <span class="w-full text-xs text-muted-foreground">{facts(entry)}</span>
      </span>
    </Tile>
  {/each}
</div>
