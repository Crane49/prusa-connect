<!--
  The picture that stands for a part: a picture file of its own, or the plate
  a slicer drew into a print file. A part with neither, and a picture that
  fails to load, draws the glyph of its kind instead, so a card is never an
  empty box.

  The address that failed is kept rather than a flag, which is what resets the
  fallback when the box is handed another part.
-->
<script lang="ts">
  import PackageIcon from '@lucide/svelte/icons/package';
  import type { ClassValue } from 'svelte/elements';

  let {
    url,
    class: className,
  }: { url: string | undefined; class?: ClassValue | undefined } = $props();

  let brokenUrl = $state<string | undefined>(undefined);
  const shown = $derived(url !== undefined && brokenUrl !== url);
</script>

<div
  class={[
    'flex items-center justify-center overflow-hidden rounded-sm bg-surface',
    className,
  ]}
  data-part="picture"
>
  {#if shown}
    <!-- Decoration: the part's name is beside it wherever it is drawn. -->
    <img
      src={url}
      alt=""
      loading="lazy"
      class="size-full object-contain"
      onerror={() => {
        brokenUrl = url;
      }}
    />
  {:else}
    <PackageIcon aria-hidden="true" class="size-1/3 text-muted-foreground" />
  {/if}
</div>
