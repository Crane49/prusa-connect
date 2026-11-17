<!-- A box smaller than its own content, for the scroll-area tests. -->
<script lang="ts">
  import type { ScrollAreaType } from 'bits-ui';

  import ScrollArea from '../primitives/scroll-area/scroll-area.svelte';

  let {
    lines,
    type = 'hover',
  }: {
    lines: readonly string[];
    type?: ScrollAreaType;
  } = $props();
</script>

<ScrollArea {type} style="height: 12rem; width: 18rem">
  <div>
    {#each lines as line (line)}
      <p>{line}</p>
    {/each}
  </div>
</ScrollArea>
