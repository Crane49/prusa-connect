<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import { emptyList, failed, pending, refused } from '../../../test/story/api';
  import AppAt from '../../../test/story/app-at.svelte';

  const { Story } = defineMeta({ title: 'Screens/Settings/API credentials' });

  const LIST = 'GET /api/v1/auth/credentials';
</script>

<!--
  States the screen branches on: populated, empty, loading, failed and
  refused. Minting a credential (the one-time secret panel) and revoking one
  need typed input or a confirmation and are skipped.
-->

<Story name="Populated"><AppAt path="/settings/tokens" /></Story>

<Story name="Empty">
  <AppAt path="/settings/tokens" overrides={{ [LIST]: emptyList }} />
</Story>

<Story name="Loading">
  <AppAt path="/settings/tokens" overrides={{ [LIST]: pending }} />
</Story>

<Story name="Failed">
  <AppAt path="/settings/tokens" overrides={{ [LIST]: failed }} />
</Story>

<Story name="Refused">
  <AppAt path="/settings/tokens" overrides={{ [LIST]: refused }} />
</Story>
