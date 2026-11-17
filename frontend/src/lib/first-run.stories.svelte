<!--
  The first-run wizard, as the app draws it when the server has recorded no
  setup decision. States it branches on here: a server never set up, and one
  whose last attempt failed. The wizard's own steps follow the setup code,
  which the story core does not answer, and the page that says the wizard's
  code did not load needs a `load` the app never hands it; both are covered
  by the library's stories and `first-run.svelte.test.ts`.
-->
<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import AppAt from '../../test/story/app-at.svelte';
  import { json } from '../test/stub';

  const { Story } = defineMeta({ title: 'Screens/First run' });

  const setup = (extra: Record<string, unknown> = {}) => ({
    'GET /api/v1/setup': () =>
      json(200, {
        state: 'pending',
        version: '0.0.0-dev',
        issuer: false,
        modules: [],
        steps: [],
        ...extra,
      }),
  });
</script>

<Story name="Not set up">
  <AppAt path="/" overrides={setup()} />
</Story>

<Story name="Last attempt failed">
  <AppAt
    path="/"
    overrides={setup({
      last_error: {
        message: 'listen tcp :80: permission denied',
        at: '2026-10-15T11:00:00Z',
      },
    })}
  />
</Story>
