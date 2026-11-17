<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import FirstRunScreen from './first-run-screen.svelte';

  const { Story } = defineMeta({
    title: 'Shell/FirstRunScreen',
    component: FirstRunScreen,
  });
</script>

<Story
  name="First Run"
  args={{ host: 'printers.local', onCreate: () => undefined }}
/>

<Story
  name="Refused"
  args={{
    host: 'printers.local',
    error: { code: 'core.auth/SETUP_CLOSED', status: 409 },
    onCreate: () => undefined,
  }}
/>

<Story
  name="Busy"
  args={{ host: 'printers.local', busy: true, onCreate: () => undefined }}
/>
