<!--
  One action in a short numbered list, with its number in a round badge. The
  first two steps of the guide draw their actions this way, so a person can
  look back from the printer and find their place. The number is drawn for the
  eye; the ordered list around it is what a screen reader counts.
-->
<script lang="ts">
  import type { Snippet } from 'svelte';

  let {
    number,
    part,
    children,
  }: {
    number: number;
    /** A name for tests and for the screenshots of the guide. */
    part?: string | undefined;
    children: Snippet;
  } = $props();
</script>

<li class="flex items-start gap-3 text-sm" data-part={part}>
  <span
    aria-hidden="true"
    class="flex size-6 shrink-0 items-center justify-center rounded-full bg-secondary text-xs font-semibold text-secondary-foreground tabular-nums"
  >
    {number}
  </span>
  <div class="flex min-w-0 flex-1 flex-col gap-2 pt-0.5">
    {@render children()}
  </div>
</li>
