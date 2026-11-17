<!--
  This lists the printers carrying one tag, behind a count of them. It is a
  panel rather than an expanded row, so a tag on forty printers does not push
  the other tags down the page. The panel scrolls past its own height.
-->
<script lang="ts">
  import ChevronDownIcon from '@lucide/svelte/icons/chevron-down';

  import { t } from '../../i18n';
  import {
    Button,
    Popover,
    PopoverContent,
    PopoverTrigger,
  } from '../../primitives';
  import type { TagRow } from './types';

  let { row }: { row: TagRow } = $props();

  const count = $derived(row.printers.length);
</script>

<Popover>
  <PopoverTrigger>
    {#snippet child({ props })}
      <Button {...props} variant="link">
        {t('tags.count', { count })}
        <ChevronDownIcon aria-hidden="true" class="size-3" />
      </Button>
    {/snippet}
  </PopoverTrigger>
  <PopoverContent
    align="start"
    aria-label={t('tags.printersOn', { tag: row.tag })}
    class="max-h-72 w-80 overflow-y-auto p-2"
  >
    <ul class="flex flex-col">
      {#each row.printers as printer (printer.id)}
        <li>
          <a
            href={printer.detailHref}
            class="flex min-h-11 items-center rounded-sm px-2 text-sm hover:bg-accent focus-visible:bg-accent focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background focus-visible:outline-none"
          >
            <span class="truncate">{printer.name}</span>
          </a>
        </li>
      {/each}
    </ul>
  </PopoverContent>
</Popover>
