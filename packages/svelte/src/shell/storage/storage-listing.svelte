<!-- The files in the open folder. -->
<script lang="ts">
  import type { Component, Snippet } from 'svelte';

  import InboxIcon from '@lucide/svelte/icons/inbox';
  import PencilIcon from '@lucide/svelte/icons/pencil';
  import Share2Icon from '@lucide/svelte/icons/share-2';
  import Trash2Icon from '@lucide/svelte/icons/trash-2';

  import { isPrintFile } from '../../domain';
  import { type FaultText, formatBytes, formatDateTime, t } from '../../i18n';
  import {
    ABOVE_ROW_LINK,
    Button,
    ClippedName,
    EmptyState,
    FaultAlert,
    FileThumbnail,
    LINKED_ROW,
    ListingNoMatch,
    type ListingState,
    ListingSkeleton,
    Pager,
    ROW_LINK,
    SelectAllCheckbox,
    SelectCheckbox,
    type Selection,
    Table,
    TableBody,
    TableCaption,
    TableCell,
    TableHead,
    TableHeader,
    TableRow,
    THUMBNAIL_ROW_MIN_HEIGHT,
    Tile,
    PrinterGlyphIcon,
    Switch,
  } from '../../primitives';
  import type { FolderSharing, StorageFieldId, StorageRow } from './types';

  const CARD_GRID =
    'grid grid-cols-2 gap-3 @2xl/storage:grid-cols-4 @5xl/storage:grid-cols-6';

  let {
    listing,
    filesFault,
    loading,
    emptyFolder,
    searching,
    query,
    writable,
    printable,
    selection,
    shows,
    glyphOf,
    thumbnailOf,
    fileHref,
    storageName,
    fileActions,
    sharing,
    onOpen,
    onPrint,
    onRename,
    onDelete,
    onClearSearch,
    onShare,
  }: {
    listing: ListingState<StorageRow>;
    filesFault?: FaultText | undefined;
    loading: boolean;
    emptyFolder: boolean;
    searching: boolean;
    query: string;
    writable: boolean;
    printable: boolean;
    /** The paths of the ticked rows. */
    selection: Selection<string>;
    shows: (field: StorageFieldId) => boolean;
    glyphOf: (entry: StorageRow) => Component;
    thumbnailOf: (entry: StorageRow) => string | undefined;
    fileHref?: ((entry: StorageRow) => string) | undefined;
    /** The storage the listed rows are on, handed to `fileActions`. */
    storageName?: string | undefined;
    /** Drawn at the end of each file's row, with its storage and the row. */
    fileActions?: Snippet<[string, StorageRow]> | undefined;
    /** The folders this server shares. With it the table has a Shared column. */
    sharing?: FolderSharing | undefined;
    onOpen: (path: string) => void;
    onPrint: (entry: StorageRow) => void;
    onRename: (entry: StorageRow) => void;
    onDelete: (entry: StorageRow) => void;
    onClearSearch: () => void;
    /** The reader flipped the share of a folder. */
    onShare: (entry: StorageRow, shared: boolean) => void;
  } = $props();

  const rootOf = (entry: StorageRow): string | undefined =>
    sharing?.sharedRoot(storageName ?? '', entry.path);

  // The switch reads the share and never holds it, so a share the reader
  // does not confirm leaves it off.
  const shareOf =
    (entry: StorageRow) =>
    (next: boolean): void => {
      onShare(entry, next);
    };

  const baseName = (path: string): string =>
    path.slice(path.lastIndexOf('/') + 1);
</script>

{#snippet pick(entry: StorageRow)}
  <SelectCheckbox
    {selection}
    key={entry.path}
    label={t('storage.select.row', { name: entry.name })}
  />
{/snippet}

{#snippet tile(entry: StorageRow)}
  <div class="flex w-full min-w-0 flex-col items-center gap-2 text-center">
    <FileThumbnail
      name={entry.name}
      thumbnailUrl={thumbnailOf(entry)}
      glyph={glyphOf(entry)}
    />
    <ClippedName name={entry.name} class="max-w-full text-xs" />
    {#if shows('size')}
      {#if entry.folder}
        {#if entry.entries !== undefined}
          <span class="text-xs text-muted-foreground">
            {t('storage.entries', { count: entry.entries })}
          </span>
        {/if}
      {:else}
        <span class="font-mono text-xs">{formatBytes(entry.size)}</span>
      {/if}
    {/if}
    <!-- The tile only marks a share. The switch is in the table. -->
    {#if entry.folder && rootOf(entry) !== undefined}
      <span
        class="inline-flex items-center gap-1 text-xs text-muted-foreground"
      >
        <Share2Icon aria-hidden="true" class="size-3.5" />
        {t('storage.share.on')}
      </span>
    {/if}
  </div>
{/snippet}

{#if filesFault}
  <FaultAlert fault={filesFault} />
{:else if loading}
  <ListingSkeleton label={t('storage.files.loading')} art rows={6} />
{:else if emptyFolder}
  <EmptyState
    icon={InboxIcon}
    heading={t('storage.empty.heading')}
    headingLevel="h2"
    description={t('storage.empty.note')}
  />
{:else if searching && listing.total === 0}
  <ListingNoMatch
    heading={t('storage.noMatch.heading')}
    headingLevel="h2"
    description={t('storage.noMatch.note', { query: query.trim() })}
    clearLabel={t('primitives.listing.clearSearch')}
    onClear={onClearSearch}
  />
{:else}
  {#if listing.view.value === 'cards'}
    <div class={CARD_GRID}>
      {#each listing.page as entry (entry.path)}
        <Tile
          selected={selection.has(entry.path)}
          onclick={entry.folder ? () => onOpen(entry.path) : undefined}
        >
          {#snippet top()}
            <span class="self-start">{@render pick(entry)}</span>
          {/snippet}
          {@render tile(entry)}
        </Tile>
      {/each}
    </div>
  {:else}
    <Table>
      <TableCaption class="sr-only">
        {t('storage.caption')}
      </TableCaption>
      <TableHeader>
        <TableRow>
          <TableHead class="w-11">
            <span class="flex size-11 items-center justify-center">
              <SelectAllCheckbox
                {selection}
                keys={listing.page.map((entry) => entry.path)}
                label={t('storage.select.page')}
              />
            </span>
          </TableHead>
          <TableHead>{t('storage.column.name')}</TableHead>
          {#if shows('size')}
            <TableHead>{t('storage.column.size')}</TableHead>
          {/if}
          {#if shows('modified')}
            <TableHead class="hidden @xl/storage:table-cell">
              {t('storage.column.modified')}
            </TableHead>
          {/if}
          {#if sharing}
            <TableHead>{t('storage.column.shared')}</TableHead>
          {/if}
          <TableHead class="sr-only">
            {t('storage.column.actions')}
          </TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {#each listing.page as entry (entry.path)}
          <TableRow
            data-state={selection.has(entry.path) ? 'selected' : undefined}
            class={[
              THUMBNAIL_ROW_MIN_HEIGHT,
              // A file's name is a stretched link too, and without the row to
              // position it against its hit area covers the whole table.
              entry.folder || fileHref !== undefined ? LINKED_ROW : undefined,
            ]}
          >
            <TableCell class="w-11">
              <!-- Over the folder row's own link, so the tick is not a step in. -->
              <span
                class={[
                  'flex size-11 items-center justify-center',
                  ABOVE_ROW_LINK,
                ]}
              >
                {@render pick(entry)}
              </span>
            </TableCell>
            <TableCell class="max-w-64">
              <div class="flex items-center gap-3">
                <FileThumbnail
                  name={entry.name}
                  thumbnailUrl={thumbnailOf(entry)}
                  glyph={glyphOf(entry)}
                />
                {#if entry.folder}
                  <!-- eslint-disable-next-line svelte/no-restricted-html-elements -- ROW_LINK button stretched over the table row -->
                  <button
                    type="button"
                    class={['min-w-0 text-start', ROW_LINK]}
                    onclick={() => onOpen(entry.path)}
                  >
                    <ClippedName name={entry.name} />
                  </button>
                {:else if fileHref}
                  <a
                    href={fileHref(entry)}
                    class={['min-w-0 text-start', ROW_LINK]}
                  >
                    <ClippedName name={entry.name} />
                  </a>
                {:else}
                  <ClippedName name={entry.name} class="min-w-0" />
                {/if}
              </div>
            </TableCell>
            {#if shows('size')}
              <!-- A folder holds no bytes of its own; what it holds is rows. -->
              <TableCell class="text-xs whitespace-nowrap">
                {#if entry.folder}
                  {#if entry.entries !== undefined}
                    <span class="text-muted-foreground">
                      {t('storage.entries', { count: entry.entries })}
                    </span>
                  {/if}
                {:else}
                  <span class="font-mono">{formatBytes(entry.size)}</span>
                {/if}
              </TableCell>
            {/if}
            {#if shows('modified')}
              <TableCell
                class="hidden whitespace-nowrap @xl/storage:table-cell"
              >
                {formatDateTime(entry.modified)}
              </TableCell>
            {/if}
            {#if sharing}
              <!-- A folder inside a shared one is shared with it, so it says
                   which, and only the folder that is shared has the switch. -->
              <TableCell class="whitespace-nowrap">
                {#if entry.folder}
                  {@const root = rootOf(entry)}
                  {#if root !== undefined && root !== entry.path}
                    <span class="text-xs text-muted-foreground">
                      {t('storage.share.inside', { folder: baseName(root) })}
                    </span>
                  {:else if sharing.canChange}
                    <Switch
                      size="sm"
                      class={ABOVE_ROW_LINK}
                      aria-label={t('storage.share.for', { name: entry.name })}
                      bind:checked={() => root !== undefined, shareOf(entry)}
                    />
                  {:else if root !== undefined}
                    <span
                      class="inline-flex items-center gap-1.5 text-xs text-muted-foreground"
                    >
                      <Share2Icon aria-hidden="true" class="size-3.5" />
                      {t('storage.share.on')}
                    </span>
                  {/if}
                {/if}
              </TableCell>
            {/if}
            <TableCell class="text-end whitespace-nowrap">
              {#if printable && !entry.folder && isPrintFile(entry.name)}
                <Button
                  variant="ghost"
                  size="sm"
                  class={ABOVE_ROW_LINK}
                  aria-label={t('storage.printFor', {
                    name: entry.name,
                  })}
                  onclick={() => {
                    onPrint(entry);
                  }}
                >
                  <PrinterGlyphIcon aria-hidden="true" />
                </Button>
              {/if}
              {#if writable}
                <Button
                  variant="ghost"
                  size="sm"
                  class={ABOVE_ROW_LINK}
                  aria-label={t('storage.renameFor', {
                    name: entry.name,
                  })}
                  onclick={() => {
                    onRename(entry);
                  }}
                >
                  <PencilIcon aria-hidden="true" />
                </Button>
                <Button
                  variant="ghost"
                  size="sm"
                  class={ABOVE_ROW_LINK}
                  aria-label={t('storage.deleteFor', {
                    name: entry.name,
                  })}
                  onclick={() => {
                    onDelete(entry);
                  }}
                >
                  <Trash2Icon aria-hidden="true" />
                </Button>
              {/if}
              {#if fileActions && storageName !== undefined && !entry.folder}
                {@render fileActions(storageName, entry)}
              {/if}
            </TableCell>
          </TableRow>
        {/each}
      </TableBody>
    </Table>
  {/if}

  {#if listing.pager}
    <Pager {...listing.pager} total={listing.total} />
  {/if}
{/if}
