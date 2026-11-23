<!--
  The frame of a module page opened with `?embed=1`: no sidebar and no
  header, only the page.
-->
<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';
  import { EmptyState } from '@prusa/connect-ui/primitives';

  import EmbedFrame from './embed-frame.svelte';

  const { Story } = defineMeta({
    title: 'Parts/Embed Frame',
    component: EmbedFrame,
  });
</script>

<Story name="A module page" asChild>
  <EmbedFrame>
    <EmptyState headingLevel="h1" heading="Spools" />
  </EmbedFrame>
</Story>
