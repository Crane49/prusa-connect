<!--
  A few parts inside a dashboard card: the picture, the name and the newest
  version. A wide card draws them as a grid of tiles and a narrow one as rows,
  so the same card reads at any size it is given.

  Each part links to its page in the catalogue. While the dashboard is being
  changed nothing links, because a click there moves the card (ADR-20261008-hna
  rule 21).
-->
<script lang="ts">
  import type { CardSize } from '@prusa/connect-ui/slots';

  import PartPicture from '../parts-page/part-picture.svelte';
  import { pictureUrl } from '../presentation';
  import { heldSession } from '../session';
  import type { CardPart } from './card-parts.svelte';
  import { partHref } from './settings';

  let {
    rows,
    size,
    editing,
  }: {
    rows: readonly CardPart[];
    size: CardSize;
    editing: boolean;
  } = $props();

  const { host, files } = heldSession();
  const t = host.t;
  const page = host.pageHref('parts');

  /** A card three cells wide or less draws rows: a tile there is too small to read. */
  const asRows = $derived(size.w <= 3);
</script>

<ul
  class={['parts-grid min-h-0 flex-1 overflow-y-auto', asRows && 'as-rows']}
  aria-label={t('parts.listing.parts')}
  data-part="dashboard-parts"
>
  {#each rows as part (part.id)}
    <li>
      <svelte:element
        this={editing ? 'span' : 'a'}
        href={editing ? undefined : partHref(page, part.id)}
        class="part-link"
        data-part="dashboard-part"
      >
        <PartPicture
          url={pictureUrl(files, part.picture)}
          class="part-picture"
        />
        <span class="grid min-w-0 gap-0.5">
          <span class="truncate text-sm font-medium">{part.name}</span>
          {#if part.version > 0}
            <span class="truncate text-xs text-muted-foreground">
              {t('parts.version.long', { number: part.version })}
            </span>
          {/if}
        </span>
      </svelte:element>
    </li>
  {/each}
</ul>

<style>
  .parts-grid {
    display: grid;
    grid-template-columns: repeat(
      auto-fill,
      minmax(calc(var(--spacing) * 28), 1fr)
    );
    align-content: start;
    gap: calc(var(--spacing) * 2);
  }

  .parts-grid.as-rows {
    grid-template-columns: minmax(0, 1fr);
    gap: calc(var(--spacing) * 1);
  }

  .part-link {
    display: grid;
    gap: calc(var(--spacing) * 1.5);
    padding: calc(var(--spacing) * 1.5);
    border-radius: var(--corner-sm);
    color: inherit;
    text-decoration: none;
  }

  a.part-link:hover {
    background-color: var(--accent);
  }

  a.part-link:focus-visible {
    outline: 2px solid var(--ring);
    outline-offset: 2px;
  }

  .part-link :global(.part-picture) {
    aspect-ratio: 4 / 3;
    inline-size: 100%;
  }

  .as-rows .part-link {
    grid-template-columns: calc(var(--spacing) * 10) minmax(0, 1fr);
    align-items: center;
  }

  .as-rows .part-link :global(.part-picture) {
    aspect-ratio: 1;
  }
</style>
