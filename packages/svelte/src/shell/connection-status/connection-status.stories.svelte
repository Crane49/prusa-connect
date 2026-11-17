<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import ConnectionStatus from './connection-status.svelte';

  const { Story } = defineMeta({
    title: 'Shell/ConnectionStatus',
    component: ConnectionStatus,
  });
</script>

<Story name="Badge Online" args={{ presentation: 'badge', state: 'online' }} />

<Story
  name="Badge Offline"
  args={{ presentation: 'badge', state: 'offline' }}
/>

<Story name="Dot Online" args={{ presentation: 'dot', state: 'online' }} />

<Story name="Dot Offline" args={{ presentation: 'dot', state: 'offline' }} />
