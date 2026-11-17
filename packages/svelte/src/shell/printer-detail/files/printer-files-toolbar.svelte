<!-- What sits over the listing. -->
<script lang="ts">
  import FolderPlusIcon from '@lucide/svelte/icons/folder-plus';
  import Trash2Icon from '@lucide/svelte/icons/trash-2';

  import { t } from '../../../i18n';
  import {
    BulkBar,
    Button,
    ListingSearch,
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
    type Selection,
  } from '../../../primitives';
  import type { PrinterFileEntry } from './types';

  let {
    selection,
    deletable,
    query,
    drives,
    drive,
    canCreateFolder,
    nameOf,
    onQuery,
    onDelete,
    onOpenDrive,
    onNewFolder,
  }: {
    selection: Selection<string>;
    /** Of the ticked rows, the ones the printer would let go. */
    deletable: readonly PrinterFileEntry[];
    query: string;
    drives: readonly PrinterFileEntry[];
    drive: string;
    canCreateFolder: boolean;
    nameOf: (entry: PrinterFileEntry) => string;
    onQuery: (next: string) => void;
    onDelete: () => void;
    onOpenDrive: (path: string) => void;
    onNewFolder: () => void;
  } = $props();
</script>

<BulkBar
  count={selection.count}
  label={t('printerDetailFiles.bulk.selected', { count: selection.count })}
  selectAllLabel={selection.count < selection.total
    ? t('printerDetailFiles.bulk.selectAll', { count: selection.total })
    : undefined}
  onSelectAll={() => {
    selection.selectAll();
  }}
  clearLabel={t('printerDetailFiles.bulk.clear')}
  onClear={() => {
    selection.clear();
  }}
  {actions}
  {idle}
/>

{#snippet actions()}
  {#if deletable.length > 0}
    <Button
      variant="destructive"
      size="sm"
      class="shrink-0"
      onclick={() => {
        onDelete();
      }}
    >
      <Trash2Icon aria-hidden="true" />
      {t('printerDetailFiles.action.delete')}
    </Button>
  {/if}
{/snippet}

{#snippet idle()}
  <ListingSearch
    class="min-w-20 flex-1"
    value={query}
    onValue={(next: string) => {
      onQuery(next);
    }}
    label={t('printerDetailFiles.search.label')}
    placeholder={t('printerDetailFiles.search.placeholder')}
  />
  {#if drives.length > 0 || canCreateFolder}
    <div class="ms-auto flex items-center gap-3">
      {#if drives.length > 0}
        <!-- Named on the content as well as the trigger. -->
        <Select
          value={drive}
          onValueChange={(path: string) => onOpenDrive(path)}
          items={drives.map((entry) => ({
            value: entry.path,
            label: nameOf(entry),
          }))}
        >
          <SelectTrigger
            size="sm"
            aria-label={t('printerDetailFiles.drive.label')}
            class="w-fit shrink-0"
          >
            <SelectValue />
          </SelectTrigger>
          <SelectContent aria-label={t('printerDetailFiles.drive.label')}>
            {#each drives as entry (entry.path)}
              <SelectItem value={entry.path}>
                {nameOf(entry)}
              </SelectItem>
            {/each}
          </SelectContent>
        </Select>
      {/if}
      {#if canCreateFolder}
        <Button
          variant="secondary"
          size="sm"
          class="shrink-0"
          onclick={() => {
            onNewFolder();
          }}
        >
          <FolderPlusIcon aria-hidden="true" />
          {t('printerDetailFiles.action.newFolder')}
        </Button>
      {/if}
    </div>
  {/if}
{/snippet}
