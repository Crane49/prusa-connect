<!--
  Whether a file dropped on a printer page is kept on this server, and in
  which storage and folder. It sits under the drop zone it applies to.
-->
<script lang="ts">
  import FolderIcon from '@lucide/svelte/icons/folder';

  import { t } from '../../../i18n';
  import {
    Button,
    Label,
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
    Switch,
  } from '../../../primitives';
  import { trailOf } from '../../storage/presentation';
  import StorageFolderDialog from '../../storage/storage-folder-dialog.svelte';
  import type { SaveToStorageProps } from './types';

  let {
    keep,
    onKeepChange,
    storages,
    storage,
    onStorageChange,
    folder,
    onFolderChange,
    pickerAt,
    pickerFolders,
    pickerFault,
    onPickerWalk,
  }: SaveToStorageProps = $props();

  const switchId = $props.id();

  let picking = $state(false);

  const label = $derived(
    storages.find((row) => row.name === storage)?.label ?? storage,
  );

  // The folder as a trail, so the root reads as the storage's own name.
  const place = $derived(
    trailOf(label, folder)
      .map((crumb) => crumb.label)
      .join(' / '),
  );

  const pick = (): void => {
    picking = true;
    onPickerWalk(folder);
  };
</script>

<div class="flex flex-col gap-2" data-part="save-to-storage">
  <div class="flex items-center gap-2">
    <Switch
      id={switchId}
      size="sm"
      checked={keep}
      onCheckedChange={onKeepChange}
    />
    <Label for={switchId}>
      {t('saveToStorage.toggle')}
    </Label>
  </div>

  {#if keep}
    <div class="flex flex-wrap items-center gap-2">
      {#if storages.length > 1}
        <!-- Named on the content as well as the trigger, as the Storage
             page's own choice is. -->
        <Select
          value={storage}
          onValueChange={onStorageChange}
          items={storages.map((row) => ({ value: row.name, label: row.label }))}
        >
          <SelectTrigger
            size="sm"
            aria-label={t('saveToStorage.storage')}
            class="w-fit shrink-0"
          >
            <SelectValue />
          </SelectTrigger>
          <SelectContent aria-label={t('saveToStorage.storage')}>
            {#each storages as row (row.name)}
              <SelectItem value={row.name}>{row.label}</SelectItem>
            {/each}
          </SelectContent>
        </Select>
      {/if}
      <span
        class="flex min-w-0 items-center gap-1 text-sm text-muted-foreground"
      >
        <FolderIcon aria-hidden="true" class="size-4 shrink-0" />
        <span class="truncate font-mono" data-part="save-to-storage-place">
          {place}
        </span>
      </span>
      <Button variant="secondary" size="sm" class="shrink-0" onclick={pick}>
        {t('saveToStorage.chooseFolder')}
      </Button>
    </div>
    <p class="text-xs text-muted-foreground">{t('saveToStorage.keptNote')}</p>
  {:else}
    <p class="text-xs text-muted-foreground">
      {t('saveToStorage.forgetNote')}
    </p>
  {/if}
</div>

<StorageFolderDialog
  open={picking}
  onOpenChange={(next: boolean) => {
    picking = next;
  }}
  title={t('saveToStorage.dialog.title')}
  description={t('saveToStorage.dialog.note')}
  confirmLabel={t('saveToStorage.dialog.confirm')}
  root={label}
  at={pickerAt}
  folders={pickerFolders}
  fault={pickerFault}
  onWalk={onPickerWalk}
  onConfirm={(path: string) => {
    picking = false;
    onFolderChange(path);
  }}
/>
