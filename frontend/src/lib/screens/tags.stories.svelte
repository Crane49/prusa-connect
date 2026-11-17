<!--
  The tags screen. The route is hidden behind FEATURES.tags (ADR-20260930-rq4), which
  is off, so the app draws its not-found page at this address. The story
  records that state and draws the tags page as soon as the flag is on.
-->
<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import AppAt from '../../../test/story/app-at.svelte';
  import { PATHS } from '../routes';

  const { Story } = defineMeta({ title: 'Screens/Tags' });
</script>

<Story name="Tags"><AppAt path={PATHS.tags} /></Story>
