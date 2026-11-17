<!--
  The printers page over the story core. States it branches on: the listing
  (populated, empty, loading, failed, refused) and the nearby-printers panel
  (printers found, none found, lookup failed; the panel draws nothing while
  it is still looking or when none are found). The listing
  follows the `printers` toolbar switch. A printer's card line, its tag filter
  and a retire in flight need product state a story cannot set, so they are
  not drawn.
-->
<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import { failed, pending, refused, emptyList } from '../../../test/story/api';
  import AppAt from '../../../test/story/app-at.svelte';

  const { Story } = defineMeta({ title: 'Screens/Printers' });

  const NEARBY = 'GET /api/v1/discovery/nearby';
</script>

<Story name="Populated"><AppAt path="/printers" /></Story>

<Story name="Empty">
  <AppAt path="/printers" overrides={{ 'GET /api/v1/devices': emptyList }} />
</Story>

<Story name="Loading">
  <AppAt path="/printers" overrides={{ 'GET /api/v1/devices': pending }} />
</Story>

<Story name="Failed">
  <AppAt path="/printers" overrides={{ 'GET /api/v1/devices': failed }} />
</Story>

<Story name="Refused">
  <AppAt path="/printers" overrides={{ 'GET /api/v1/devices': refused }} />
</Story>

<Story name="Nothing nearby">
  <AppAt path="/printers" overrides={{ [NEARBY]: emptyList }} />
</Story>

<Story name="Nearby lookup failed">
  <AppAt path="/printers" overrides={{ [NEARBY]: failed }} />
</Story>
