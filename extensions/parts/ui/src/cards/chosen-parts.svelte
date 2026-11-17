<!--
  The parts a person chose, read once when the card is drawn, in the order
  they were chosen.
-->
<script lang="ts">
  import type { CardSize } from '@prusa/connect-ui/slots';
  import { onMount } from 'svelte';

  import { partsOn } from '../parts';
  import { heldSession } from '../session';
  import CardState from './card-state.svelte';
  import { CardParts } from './card-parts.svelte';
  import PartsGrid from './parts-grid.svelte';

  let {
    ids,
    size,
    editing,
  }: { ids: readonly number[]; size: CardSize; editing: boolean } = $props();

  const { host } = heldSession();
  const t = host.t;
  const parts = new CardParts(partsOn(host));

  onMount(() => {
    void parts.readChosen(ids);

    return () => {
      parts.stop();
    };
  });
</script>

<CardState
  {parts}
  empty={ids.length === 0
    ? {
        heading: t('parts.dashboard.chosen.none'),
        note: t('parts.dashboard.chosen.noneNote'),
      }
    : {
        heading: t('parts.dashboard.chosen.gone'),
        note: t('parts.dashboard.chosen.goneNote'),
      }}
>
  <PartsGrid rows={parts.rows} {size} {editing} />
  {#if parts.missing > 0}
    <p class="text-xs text-muted-foreground" data-part="dashboard-missing">
      {t('parts.dashboard.chosen.missing', { count: parts.missing })}
    </p>
  {/if}
</CardState>
