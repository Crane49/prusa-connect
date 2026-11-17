<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import SignInScreen from './sign-in-screen.svelte';

  const { Story } = defineMeta({
    title: 'Shell/SignInScreen',
    component: SignInScreen,
  });
</script>

<Story
  name="Sign In"
  args={{ host: 'printer.local', onSignIn: () => undefined }}
/>

<Story
  name="Session Expired"
  args={{ host: 'printer.local', ended: 'refused', onSignIn: () => undefined }}
/>

<Story
  name="Refused"
  args={{
    host: 'printer.local',
    error: { code: 'core.auth/LOGIN_REFUSED', status: 401 },
    onSignIn: () => undefined,
  }}
/>

<Story
  name="Busy"
  args={{ host: 'printer.local', busy: true, onSignIn: () => undefined }}
/>

<Story
  name="Through A Provider"
  args={{
    host: 'printer.local',
    redirectProvider: 'sso.example.com',
    onRedirect: () => undefined,
    onSignIn: () => undefined,
  }}
/>

<Story
  name="Provider Sign In Failed"
  args={{
    host: 'printer.local',
    redirectProvider: 'auth.example.com',
    failure: 'id_token',
    onRedirect: () => undefined,
    onSignIn: () => undefined,
  }}
/>
