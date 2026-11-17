<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import { failed, pending, refused } from '../../../test/story/api';
  import AppAt from '../../../test/story/app-at.svelte';
  import fixtures from '../../../test/ux/fixtures/api.json';
  import { json } from '../../test/stub';

  const { Story } = defineMeta({ title: 'Screens/Settings/Diagnostics' });

  const EXTENSIONS = 'GET /api/v1/diagnostics';

  const ui = JSON.parse(fixtures['GET /api/v1/ui'].body) as Record<
    string,
    unknown
  >;
</script>

<!--
  The screen is behind FEATURES.diagnostics, which GET /api/v1/ui sets and the
  fixture leaves on. States the screen branches on: no extension adds files,
  extensions listed, the read of them loading, failed and refused (only an
  admin may read it), a server with the
  upload address set (Send button), and a server with diagnostics off (the
  not-found screen). Building and sending a bundle writes or opens a window
  and is skipped.
-->

<Story name="No Extensions">
  <AppAt
    path="/settings/diagnostics"
    overrides={{ [EXTENSIONS]: () => json(200, { extensions: [] }) }}
  />
</Story>

<Story name="With Extensions">
  <AppAt
    path="/settings/diagnostics"
    overrides={{
      [EXTENSIONS]: () =>
        json(200, { extensions: ['Printer log lines', 'Spool tracker'] }),
    }}
  />
</Story>

<Story name="Send Offered">
  <AppAt
    path="/settings/diagnostics"
    overrides={{
      [EXTENSIONS]: () => json(200, { extensions: [] }),
      'GET /api/v1/ui': () =>
        json(200, {
          ...ui,
          diagnostics_send_url: 'https://support.example/diagnostics',
        }),
    }}
  />
</Story>

<Story name="Loading">
  <AppAt path="/settings/diagnostics" overrides={{ [EXTENSIONS]: pending }} />
</Story>

<Story name="Failed">
  <AppAt path="/settings/diagnostics" overrides={{ [EXTENSIONS]: failed }} />
</Story>

<Story name="Refused">
  <AppAt path="/settings/diagnostics" overrides={{ [EXTENSIONS]: refused }} />
</Story>

<Story name="Switched Off">
  <AppAt
    path="/settings/diagnostics"
    overrides={{
      'GET /api/v1/ui': () => json(200, { ...ui, diagnostics: false }),
    }}
  />
</Story>
