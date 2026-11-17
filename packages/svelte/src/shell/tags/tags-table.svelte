<!-- This draws every tag as a row, with the printers carrying it. -->
<script lang="ts">
  import type { Snippet } from 'svelte';

  import { t } from '../../i18n';
  import {
    Table,
    TableBody,
    TableCaption,
    TableCell,
    TableHead,
    TableHeader,
    TableRow,
  } from '../../primitives';
  import TagPrinters from './tag-printers.svelte';
  import type { TagFieldId, TagRow } from './types';

  let {
    rows,
    shows,
    action,
  }: {
    rows: readonly TagRow[];
    shows: (field: TagFieldId) => boolean;
    /** The row's own action, written once by the screen and drawn in both views. */
    action?: Snippet<[TagRow]> | undefined;
  } = $props();
</script>

<Table>
  <TableCaption class="sr-only">{t('tags.caption')}</TableCaption>
  <TableHeader>
    <TableRow>
      <TableHead>{t('tags.tag')}</TableHead>
      {#if shows('printers')}<TableHead>{t('tags.printers')}</TableHead>{/if}
      {#if action}<TableHead class="sr-only">{t('tags.actions')}</TableHead
        >{/if}
    </TableRow>
  </TableHeader>
  <TableBody>
    {#each rows as row (row.id)}
      <TableRow>
        <TableCell class="font-medium">{row.tag}</TableCell>
        {#if shows('printers')}
          <TableCell><TagPrinters {row} /></TableCell>
        {/if}
        {#if action}
          <TableCell class="text-end">{@render action(row)}</TableCell>
        {/if}
      </TableRow>
    {/each}
  </TableBody>
</Table>
