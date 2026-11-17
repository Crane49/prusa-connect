<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import AppAt from '../../../test/story/app-at.svelte';

  const { Story } = defineMeta({ title: 'Screens/Settings/Server Address' });
</script>

<Story name="Redirect">
  <AppAt path="/settings/server" />
</Story>
