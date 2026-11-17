<!--
  The parts of one folder, read once when the card is drawn. The card draws
  this again whenever its settings change, so a read always matches them.
-->
<script lang="ts">
  import type { CardSize } from '@prusa/connect-ui/slots';
  import { onMount } from 'svelte';

  import { partsOn } from '../parts';
  import { heldSession } from '../session';
  import CardState from './card-state.svelte';
  import { CardParts } from './card-parts.svelte';
  import PartsGrid from './parts-grid.svelte';
  import type { FolderCardSettings } from './settings';

  let {
    settings,
    size,
    editing,
  }: { settings: FolderCardSettings; size: CardSize; editing: boolean } =
    $props();

  const { host } = heldSession();
  const t = host.t;
  const parts = new CardParts(partsOn(host));

  onMount(() => {
    void parts.readFolder(settings);

    return () => {
      parts.stop();
    };
  });

  const more = $derived(parts.matched - parts.rows.length);
</script>

<CardState
  {parts}
  empty={{
    heading: t('parts.dashboard.folder.empty'),
    note: t('parts.dashboard.folder.emptyNote'),
  }}
>
  <PartsGrid rows={parts.rows} {size} {editing} />
  {#if more > 0}
    <p class="text-xs text-muted-foreground" data-part="dashboard-more">
      {t('parts.dashboard.more', { count: more })}
    </p>
  {/if}
</CardState>
