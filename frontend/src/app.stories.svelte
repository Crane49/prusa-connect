<!--
  What the app draws before the frame of screens: the sign-in and the first
  run. States it branches on: the sign-in for a first visit, with the
  server's provider, the first-run wizard, and the wizard naming the last
  failed attempt. Skipped: the blank page while the setup and the principal
  are still being asked, which draws nothing; the sign-in after a session
  expired, which needs a session that was signed in a moment earlier; and the
  crash panel, which needs a screen that throws. `PersonScreen` always draws
  its child, keyed by the person, so it has no state of its own to show.
-->
<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import AppAt from '../test/story/app-at.svelte';
  import { json, problem } from './test/stub';

  const { Story } = defineMeta({ title: 'Screens/Sign in' });

  const signedOut = {
    'GET /api/v1/principal': () => problem(401, 'unauthenticated'),
  };
</script>

<Story name="Password">
  <AppAt path="/" overrides={signedOut} />
</Story>

<Story name="Provider">
  <AppAt
    path="/"
    overrides={{
      ...signedOut,
      'GET /api/v1/auth/info': () =>
        json(200, {
          provider: 'Prusa Account',
          credentials: true,
          redirect: true,
          needs_setup: false,
        }),
    }}
  />
</Story>
