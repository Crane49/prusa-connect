<!--
  Every tag the catalogue uses, the most used first. A tag narrows the listing
  to the parts that carry it, across every folder, and pressing it again
  takes the narrowing off.
-->
<script lang="ts">
  import { Chip } from '@prusa/connect-ui/primitives';

  import { heldSession } from '../session';
  import FolderLink from './folder-link.svelte';

  const { catalogue, navigation, host } = heldSession();
  const t = host.t;

  /** How many tags the sidebar shows before the rest are behind a search. */
  const SHOWN = 30;

  const tags = $derived(catalogue.tags.slice(0, SHOWN));
  const chosen = $derived(navigation.spot.tag);
</script>

{#if tags.length > 0}
  <section class="grid gap-y-2" aria-labelledby="parts-tags">
    <h2
      id="parts-tags"
      class="text-xs font-medium tracking-wide text-muted-foreground uppercase"
    >
      {t('parts.tags.heading')}
    </h2>
    <ul class="flex flex-wrap gap-x-1.5 gap-y-1.5" data-part="tag-list">
      {#each tags as tag (tag.tag)}
        <li>
          <Chip
            selected={chosen === tag.tag}
            label={t('parts.tags.filter', { tag: tag.tag, count: tag.parts })}
          >
            {#snippet child({ props })}
              <FolderLink
                {...props}
                spot={{
                  folder: navigation.spot.folder,
                  part: undefined,
                  q: navigation.spot.q,
                  tag: chosen === tag.tag ? '' : tag.tag,
                }}
                current={chosen === tag.tag}
                label={t('parts.tags.filter', {
                  tag: tag.tag,
                  count: tag.parts,
                })}
              >
                <span>{tag.tag}</span>
                <span class="tabular-nums">{tag.parts}</span>
              </FolderLink>
            {/snippet}
          </Chip>
        </li>
      {/each}
    </ul>
  </section>
{/if}
