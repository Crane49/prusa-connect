<!--
  A listing that has not arrived yet, in the shape of the listing that will.

  Every listing in the product uses this one shape. A card that is simply
  empty while the data is on its way reads as "there is nothing here" rather
  than "this is still coming".

  The box is a `role="status"` with a name. A person who cannot see the pulse
  gets the sentence "loading jobs" and nothing else, so the name is the whole
  of what that reader gets. Rows rather than a spinner: a spinner says only
  that something is happening, and these say what is coming and roughly how
  much of it.

  Two bars in a row, not one. A listing here shows a name and a fact or two
  beside it, and one full-width bar per row looks like a paragraph loading
  rather than a table.

  The printers list and the file browser have their own. Printers load into a
  grid of cards, and a file row starts with a thumbnail and a folder indent.
  A stack of plain rows would promise a shape neither screen then draws.
-->
<script lang="ts">
  import Skeleton from '../skeleton/skeleton.svelte';
  import type { ListingSkeletonProps } from './types';

  let {
    label,
    rows = 5,
    art = false,
    class: className,
  }: ListingSkeletonProps = $props();
</script>

<div
  role="status"
  aria-label={label}
  class={['flex flex-col gap-3', className]}
>
  {#each { length: rows }, row (row)}
    <div class="flex items-center gap-3">
      {#if art}
        <Skeleton class="size-6 shrink-0 rounded-sm" />
      {/if}
      <Skeleton class="h-4 flex-1" />
      <Skeleton class="h-4 w-24 shrink-0" />
    </div>
  {/each}
</div>
