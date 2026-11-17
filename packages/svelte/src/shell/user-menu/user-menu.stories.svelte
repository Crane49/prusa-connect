<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import UserMenu from './user-menu.svelte';

  const { Story } = defineMeta({
    title: 'Shell/UserMenu',
    component: UserMenu,
  });
</script>

<Story
  name="Signed In"
  args={{ principal: 'user:alice', onSignOut: () => undefined }}
/>

<Story
  name="Signed In At A Provider"
  args={{
    principal: 'Ada Admin',
    principalDetail: 'ada@example.com',
    onSignOut: () => undefined,
  }}
/>

<Story name="No Sign Out" args={{ principal: 'user:alice' }} />

<Story name="Signed Out" args={{}} />
