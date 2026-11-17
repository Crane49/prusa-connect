<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import { json } from '../../test/stub';
  import AppAt from '../../../test/story/app-at.svelte';
  import fixtures from '../../../test/ux/fixtures/api.json';

  const { Story } = defineMeta({ title: 'Screens/Settings/Appearance' });

  const ui = JSON.parse(fixtures['GET /api/v1/ui'].body) as Record<
    string,
    unknown
  >;
</script>

<!--
  States the screen branches on: dashboards on and off, as GET /api/v1/ui
  says.
  Omitted Loading: the screen makes no request of its own, and the frame waits for GET /api/v1/ui before it draws any screen.
  Omitted Failed: a failed GET /api/v1/ui leaves dashboards on, which the Dashboards On story shows.
  Omitted Refused: GET /api/v1/ui needs no identity, so no person is refused it.
-->

<Story name="Dashboards On"><AppAt path="/settings/appearance" /></Story>

<Story name="Dashboards Off">
  <AppAt
    path="/settings/appearance"
    overrides={{
      'GET /api/v1/ui': () => json(200, { ...ui, dashboards: false }),
    }}
  />
</Story>
