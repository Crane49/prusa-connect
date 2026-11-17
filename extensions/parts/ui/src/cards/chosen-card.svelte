<!--
  A dashboard card of parts a person chose one by one, such as the spare parts
  a workshop prints most.
-->
<script lang="ts">
  import type { CardProps } from '@prusa/connect-ui/slots';

  import { heldSession } from '../session';
  import CardFrame from './card-frame.svelte';
  import ChosenParts from './chosen-parts.svelte';
  import { idsOf } from './settings';

  let { config, title, size, editing }: CardProps = $props();

  const t = heldSession().host.t;

  const ids = $derived(idsOf(config.parts));
  const read = $derived(ids.join(','));
</script>

<CardFrame
  part="parts-chosen-card"
  heading={title.trim() === '' ? t('parts.dashboard.chosen.name') : title}
>
  {#key read}
    <ChosenParts {ids} {size} {editing} />
  {/key}
</CardFrame>
