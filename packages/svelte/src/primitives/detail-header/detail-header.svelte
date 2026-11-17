<!--
  The caller fills the snippets and sets no geometry. The breakpoint where the
  acts take their own line is a container query, so the header answers the room
  it is in rather than the width of the window.

  It draws no trail. A detail screen reaches its parent by href or by putting
  its own listing back, and `PageTrail` already takes both.
-->
<script lang="ts">
  import ClippedName from '../clipped-name/clipped-name.svelte';
  import Skeleton from '../skeleton/skeleton.svelte';
  import type { DetailHeaderProps } from './types';

  let {
    title,
    media,
    titleAside,
    meta,
    actions,
    loading = false,
    class: className,
  }: DetailHeaderProps = $props();
</script>

<div
  data-slot="detail-header"
  class={['@container/detail-header w-full', className]}
>
  {#if loading}
    <div class="flex flex-col gap-4 @[40rem]/detail-header:flex-row">
      <Skeleton class="size-32 shrink-0" />
      <div class="flex min-w-0 flex-1 flex-col gap-2">
        <Skeleton class="h-6 w-64" />
        <Skeleton class="h-4 w-48" />
        <Skeleton class="h-4 w-40" />
      </div>
    </div>
  {:else}
    <div
      class="flex flex-col gap-4 @[40rem]/detail-header:flex-row @[40rem]/detail-header:items-start"
    >
      {#if media}
        <div class="shrink-0">{@render media()}</div>
      {/if}
      <div class="flex min-w-0 flex-1 flex-col gap-2">
        <div class="flex items-center gap-x-3 gap-y-2">
          <h1 class="min-w-0 text-lg font-semibold">
            <ClippedName name={title} />
          </h1>
          {#if titleAside}{@render titleAside()}{/if}
        </div>
        {#if meta}{@render meta()}{/if}
      </div>
      {#if actions}
        <div
          class="flex flex-wrap items-center gap-2 @[40rem]/detail-header:justify-end"
        >
          {@render actions()}
        </div>
      {/if}
    </div>
  {/if}
</div>
