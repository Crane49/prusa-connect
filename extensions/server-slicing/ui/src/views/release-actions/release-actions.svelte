<!--
  What one version's row can do: open its detail, and for an administrator
  install it, make it the default or remove it.
-->
<script lang="ts">
  import {
    DropdownMenuItem,
    DropdownMenuSeparator,
    RowActions,
  } from '@prusa/connect-ui/primitives';
  import DownloadIcon from '@lucide/svelte/icons/download';
  import InfoIcon from '@lucide/svelte/icons/info';
  import StarIcon from '@lucide/svelte/icons/star';
  import StarOffIcon from '@lucide/svelte/icons/star-off';
  import Trash2Icon from '@lucide/svelte/icons/trash-2';

  import type { Translate } from '../../api';
  import {
    canInstall,
    canPin,
    canRemove,
    canUnpin,
    type ReleaseActs,
  } from '../../release-acts.svelte';
  import type { ReleaseRow } from '../../releases';

  let {
    row,
    administrator,
    acts,
    detailHref,
    t,
  }: {
    row: ReleaseRow;
    administrator: boolean;
    acts: ReleaseActs;
    detailHref: string;
    t: Translate;
  } = $props();
</script>

<RowActions name={t('slicer.release.name', { version: row.version })}>
  <DropdownMenuItem>
    {#snippet child({ props })}
      <a {...props} href={detailHref}>
        <InfoIcon aria-hidden="true" />
        {t('slicer.action.details')}
      </a>
    {/snippet}
  </DropdownMenuItem>
  {#if canInstall(row, administrator) || canPin(row, administrator) || canUnpin(row, administrator) || canRemove(row, administrator)}
    <DropdownMenuSeparator />
  {/if}
  {#if canInstall(row, administrator)}
    <DropdownMenuItem onSelect={() => acts.install(row)}>
      <DownloadIcon aria-hidden="true" />
      {row.state === 'failed'
        ? t('slicer.action.retry')
        : t('slicer.action.install')}
    </DropdownMenuItem>
  {/if}
  {#if canPin(row, administrator)}
    <DropdownMenuItem onSelect={() => acts.pin(row)}>
      <StarIcon aria-hidden="true" />
      {t('slicer.action.pin')}
    </DropdownMenuItem>
  {/if}
  {#if canUnpin(row, administrator)}
    <DropdownMenuItem onSelect={() => acts.unpin()}>
      <StarOffIcon aria-hidden="true" />
      {t('slicer.action.unpin')}
    </DropdownMenuItem>
  {/if}
  {#if canRemove(row, administrator)}
    <DropdownMenuItem variant="destructive" onSelect={() => acts.remove(row)}>
      <Trash2Icon aria-hidden="true" />
      {t('slicer.action.remove')}
    </DropdownMenuItem>
  {/if}
</RowActions>
