<!--
  A path this build answers with no screen. It has one state: it names the
  path. A dashboard id that is not a number draws it too, under
  Screens/Dashboard.
-->
<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import AppAt from '../../../test/story/app-at.svelte';

  const { Story } = defineMeta({ title: 'Screens/Not found' });
</script>

<Story name="Not found">
  <AppAt path="/no-such-page" />
</Story>
