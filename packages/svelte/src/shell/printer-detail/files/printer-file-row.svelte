<script lang="ts">
  import ChevronRightIcon from '@lucide/svelte/icons/chevron-right';
  import HardDriveDownloadIcon from '@lucide/svelte/icons/hard-drive-download';
  import PlayIcon from '@lucide/svelte/icons/play';

  import { formatBytes, formatDateTime, t } from '../../../i18n';
  import {
    ABOVE_ROW_LINK,
    Badge,
    Button,
    ClippedName,
    FileThumbnail,
    LINKED_ROW,
    ROW_LINK,
    SelectCheckbox,
    TableCell,
    TableRow,
    THUMBNAIL_ROW_MIN_HEIGHT,
  } from '../../../primitives';
  import PrinterFileActions from './printer-file-actions.svelte';
  import PrinterFileRow from './printer-file-row.svelte';
  import type { PrinterFileEntry, PrinterFileRowContext } from './types';

  let {
    entry,
    depth,
    row,
  }: {
    entry: PrinterFileEntry;
    /** How far in a disclosed folder's children are drawn. */
    depth: number;
    row: PrinterFileRowContext;
  } = $props();

  const print = $derived(row.printControl(entry));
  const open = $derived(row.isOpen(entry));
  const children = $derived(open ? row.childrenOf(entry) : undefined);
</script>

{#snippet badges()}
  <!-- The row's own Flash button drops its label in a narrow container. -->
  {#if row.isFirmware(entry)}
    <Badge variant="outline" class="shrink-0">
      {t('printerDetail.files.badge.firmware')}
    </Badge>
  {:else if row.isModel(entry)}
    <Badge variant="outline" class="shrink-0">
      {t('printerDetailFiles.badge.modelOnly')}
    </Badge>
  {/if}
{/snippet}

<TableRow
  data-state={row.selection.has(entry.path) ? 'selected' : undefined}
  class={[THUMBNAIL_ROW_MIN_HEIGHT, entry.folder ? LINKED_ROW : undefined]}
>
  <TableCell class="w-11">
    <span class={['flex size-11 items-center justify-center', ABOVE_ROW_LINK]}>
      <SelectCheckbox
        selection={row.selection}
        key={entry.path}
        label={t('printerDetailFiles.select.row', {
          name: row.nameOf(entry),
        })}
      />
    </span>
  </TableCell>
  <TableCell class="max-w-64">
    <div
      class="flex items-center gap-3"
      style="padding-inline-start: calc(var(--spacing) * {depth * 4})"
    >
      {#if row.discloses() && entry.folder}
        <!-- The row's own press walks into the folder. -->
        <Button
          variant="ghost"
          size="icon-md"
          class={[
            'hidden @md/printer-file-listing:inline-flex',
            ABOVE_ROW_LINK,
          ]}
          aria-expanded={open}
          label={open
            ? t('printerDetailFiles.tree.collapse', { name: row.nameOf(entry) })
            : t('printerDetailFiles.tree.expand', { name: row.nameOf(entry) })}
          onclick={() => row.onDisclose(entry)}
        >
          <ChevronRightIcon
            aria-hidden="true"
            class={['transition-transform', open && 'rotate-90']}
          />
        </Button>
      {:else}
        <span
          class="hidden size-9 shrink-0 @md/printer-file-listing:block"
          aria-hidden="true"
        ></span>
      {/if}
      <FileThumbnail
        name={row.nameOf(entry)}
        glyph={row.glyphOf(entry)}
        thumbnailUrl={row.thumbnailOf(entry)}
      />
      {#if entry.folder}
        <!-- eslint-disable-next-line svelte/no-restricted-html-elements -- a ROW_LINK button, the documented composition for a row that opens a folder -->
        <button
          type="button"
          class={['min-w-0 flex-1 overflow-hidden text-start', ROW_LINK]}
          onclick={() => row.onOpenFolder(entry.path)}
        >
          <ClippedName name={row.nameOf(entry)} />
        </button>
      {:else}
        <ClippedName
          name={row.nameOf(entry)}
          class="min-w-0 flex-1 overflow-hidden"
        />
      {/if}
      {@render badges()}
    </div>
  </TableCell>
  {#if row.shows('size')}
    <TableCell class="font-mono text-xs whitespace-nowrap">
      {entry.folder ? '' : formatBytes(entry.size)}
    </TableCell>
  {/if}
  {#if row.shows('modified')}
    <TableCell
      class="hidden whitespace-nowrap @xl/printer-file-listing:table-cell"
    >
      {entry.modified === undefined ? '' : formatDateTime(entry.modified)}
    </TableCell>
  {/if}
  <TableCell class="text-end">
    <div class="flex items-center justify-end gap-1">
      {#if children !== undefined}
        <!-- Only an expanded folder carries a count. -->
        <span class="shrink-0 text-xs text-muted-foreground">
          {t('printerDetailFiles.tree.items', { count: children.length })}
        </span>
      {/if}
      <!-- Gone where the row has no width to spare. -->
      {#if print}
        <Button
          variant="secondary"
          size="sm"
          class={[
            'hidden @md/printer-file-listing:inline-flex',
            ABOVE_ROW_LINK,
          ]}
          disabled={print.disabled}
          aria-label={print.reason === undefined
            ? t('printerDetail.files.printFor', { name: row.nameOf(entry) })
            : t('printerDetailFiles.action.printRefusedFor', {
                name: row.nameOf(entry),
                reason: print.reason,
              })}
          {...print.reason === undefined ? {} : { title: print.reason }}
          onclick={() => row.onPrint(entry)}
        >
          <PlayIcon aria-hidden="true" />
          {t('printerDetailFiles.action.print')}
        </Button>
      {/if}
      <!-- Where Print sits on a plate. -->
      {#if row.canFlash(entry)}
        <Button
          size="sm"
          class={[
            'hidden @md/printer-file-listing:inline-flex',
            ABOVE_ROW_LINK,
          ]}
          aria-label={t('printerDetail.files.flashFor', {
            name: row.nameOf(entry),
          })}
          onclick={() => {
            row.onFlash(entry);
          }}
        >
          <HardDriveDownloadIcon aria-hidden="true" />
          {t('printerDetail.files.action.flash')}
        </Button>
      {/if}
      <PrinterFileActions {entry} {row} />
    </div>
  </TableCell>
</TableRow>
{#if children !== undefined}
  {#each children as child (child.path)}
    <PrinterFileRow entry={child} depth={depth + 1} {row} />
  {/each}
{/if}
