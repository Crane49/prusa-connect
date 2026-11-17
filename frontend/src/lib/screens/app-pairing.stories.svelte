<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import { failed, pending, refused } from '../../../test/story/api';
  import AppAt from '../../../test/story/app-at.svelte';
  import { json } from '../../test/stub';

  const { Story } = defineMeta({ title: 'Screens/Pair an app' });

  const READ = 'GET /api/v1/auth/app-pairings/BCDF-GHJK';
  const PATH = '/pair?code=BCDF-GHJK';

  const asks = () =>
    json(200, {
      user_code: 'BCDF-GHJK',
      app_name: 'Kitchen display',
      scopes: ['read', 'operate'],
      client_address: '192.0.2.41',
      expires_at: new Date(Date.now() + 8 * 60_000).toISOString(),
    });
</script>

<!--
  States the page branches on: no code yet, an app that asks, loading, failed
  and refused. The approval and the denial need a click and are drawn in the
  library's own stories.
-->

<Story name="No Code Yet"><AppAt path="/pair" /></Story>

<Story name="An App Asks">
  <AppAt path={PATH} overrides={{ [READ]: asks }} />
</Story>

<Story name="Loading">
  <AppAt path={PATH} overrides={{ [READ]: pending }} />
</Story>

<Story name="Failed">
  <AppAt path={PATH} overrides={{ [READ]: failed }} />
</Story>

<Story name="Refused">
  <AppAt path={PATH} overrides={{ [READ]: refused }} />
</Story>
