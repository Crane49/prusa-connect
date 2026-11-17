<!--
  A small drawing of a template: its tiles as blocks on a 12-column grid, each
  tinted by what it shows, so a person sees the shape of a dashboard before
  choosing it. The drawing is decoration; the name and the line beside it say
  the same in words.

-->
<script lang="ts">
  import { type DashboardTemplate, templateCards } from '../../../domain';
  import { namespaceOf } from '../card-catalogue';

  let { template }: { template: DashboardTemplate } = $props();

  const tiles = $derived(templateCards(template));
</script>

<div aria-hidden="true" class="template-picture" data-template={template}>
  {#each tiles as tile, index (index)}
    <span
      data-kind={tile.type.slice(namespaceOf(tile.type).length + 1)}
      style="--x: {tile.x}; --y: {tile.y}; --w: {tile.w}; --h: {tile.h}"
    ></span>
  {/each}
</div>

<style>
  .template-picture {
    display: grid;
    grid-template-columns: repeat(12, minmax(0, 1fr));
    grid-template-rows: repeat(10, minmax(0, 1fr));
    gap: calc(var(--spacing) * 0.75);
    aspect-ratio: 16 / 9;
    padding: calc(var(--spacing) * 1.5);
    border: 1px solid var(--border);
    border-radius: var(--corner-sm);
    background-color: var(--muted);
  }

  /* An empty template draws its cells, so it reads as a grid waiting for
     tiles and not as a box that failed to load. */
  .template-picture[data-template='blank'] {
    background-image:
      linear-gradient(to right, var(--border) 1px, transparent 1px),
      linear-gradient(to bottom, var(--border) 1px, transparent 1px);
    background-size: calc(100% / 12) calc(100% / 6);
  }

  .template-picture > span {
    grid-column: calc(var(--x) + 1) / span var(--w);
    grid-row: calc(var(--y) + 1) / span var(--h);
    border-radius: var(--corner-sm);
    border: 1px solid var(--border);
    border-inline-start-width: 3px;
    background-color: var(--card);
  }

  .template-picture > [data-kind='printer'],
  .template-picture > [data-kind='printers'] {
    border-inline-start-color: var(--state-active-edge);
  }

  .template-picture > [data-kind='summary'],
  .template-picture > [data-kind='clock'] {
    border-inline-start-color: var(--primary);
  }

  .template-picture > [data-kind='finishing'] {
    border-inline-start-color: var(--state-done-edge);
  }

  .template-picture > [data-kind='issues'] {
    border-inline-start-color: var(--state-attention-edge);
  }

  .template-picture > [data-kind='temperature'] {
    border-inline-start-color: var(--state-fault-edge);
  }

  .template-picture > [data-kind='camera'] {
    border-inline-start-color: var(--state-quiet-edge);
  }
</style>
