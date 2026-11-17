<!-- A header with all four snippets filled, for the detail-header tests. -->
<script lang="ts">
  import DetailHeader from '../primitives/detail-header/detail-header.svelte';

  let {
    title,
    parts = false,
    loading = false,
  }: { title: string; parts?: boolean; loading?: boolean } = $props();
</script>

{#snippet media()}<div data-testid="media">picture</div>{/snippet}
{#snippet titleAside()}<span data-testid="title-aside">Printing</span>{/snippet}
{#snippet meta()}<div data-testid="meta">lines</div>{/snippet}
{#snippet actions()}<button data-testid="actions" type="button">Act</button
  >{/snippet}

{#if parts}
  <DetailHeader {title} {loading} {media} {titleAside} {meta} {actions} />
{:else}
  <DetailHeader {title} {loading} />
{/if}
