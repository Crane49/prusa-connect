<!--
  A sample of a listing shape, in the shape of the thing chosen.

  Deliberately schematic and deliberately not a screenshot: a wireframe of
  bars and boxes says "one big panel" against "a grid of small ones" at 80px
  wide, where a scaled-down render of the real screen says only "grey".

  `aria-hidden`, and always beside the option's own words, so nothing here is
  the only carrier of anything.
-->
<script lang="ts">
  import type { ViewModeName } from '../../primitives';

  let { kind }: { kind: ViewModeName } = $props();

  const BAR = 'rounded-xs bg-muted-foreground/30';
</script>

<span
  aria-hidden="true"
  class="flex h-14 w-20 shrink-0 flex-col gap-1 rounded-sm border border-border bg-surface p-1.5"
>
  {#if kind === 'table'}
    <!-- A header rule and four rows of it. -->
    <span class={[BAR, 'h-1 w-full']}></span>
    {#each [0, 1, 2, 3] as row (row)}
      <span
        class="h-1 rounded-xs bg-muted-foreground/20"
        style="width: {100 - row * 12}%"
      ></span>
    {/each}
  {:else if kind === 'rows'}
    <!-- Fewer, taller rows, each with a second line: what a table becomes
         where there is no room for columns. -->
    {#each [0, 1] as row (row)}
      <span
        class="flex flex-1 flex-col justify-center gap-1 rounded-xs bg-muted-foreground/15 px-1"
      >
        <span class={[BAR, 'h-1 w-3/4']}></span>
        <span class={[BAR, 'h-1 w-1/2']}></span>
      </span>
    {/each}
  {:else if kind === 'cards'}
    <!-- A tile per item, two by two. -->
    <span class="grid flex-1 grid-cols-2 gap-1">
      {#each [0, 1, 2, 3] as cell (cell)}
        <span class="rounded-xs bg-muted-foreground/25"></span>
      {/each}
    </span>
  {:else}
    <!-- Every camera at once: two pictures side by side. -->
    <span class="grid flex-1 grid-cols-2 gap-1">
      {#each [0, 1] as cell (cell)}
        <span class="rounded-xs bg-primary/25"></span>
      {/each}
    </span>
  {/if}
</span>
