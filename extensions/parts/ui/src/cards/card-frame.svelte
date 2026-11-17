<!--
  The frame both parts cards draw in: the heading, the link to the catalogue
  beside it when there is one, and the parts under it, given what is left of
  the card's height.
-->
<script lang="ts">
  import FolderIcon from '@lucide/svelte/icons/folder';
  import { TilePanel } from '@prusa/connect-ui/primitives';
  import type { Snippet } from 'svelte';

  let {
    heading,
    href,
    link,
    part,
    children,
  }: {
    heading: string;
    href?: string | undefined;
    /** The words of the link, for a screen reader. */
    link?: string | undefined;
    /** The card's `data-part`, which a test finds it by. */
    part: string;
    children: Snippet;
  } = $props();
</script>

<div class="contents" data-part={part}>
  <TilePanel title={heading}>
    {#snippet trailing()}
      {#if href !== undefined}
        <a
          {href}
          class="shrink-0 rounded-sm p-1 text-muted-foreground hover:bg-accent hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
          aria-label={link}
          data-part="dashboard-open"
        >
          <FolderIcon aria-hidden="true" class="size-4" />
        </a>
      {/if}
    {/snippet}
    {@render children()}
  </TilePanel>
</div>
