<!--
  What PrusaSlicer shows while it signs the person in, drawn in the frame the
  app gives it. The app draws it only when the slicer opened the page and
  nobody is signed in yet.
-->
<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import EmbedFrame from '../embed-frame.svelte';
  import SlicerWaiting from './slicer-waiting.svelte';

  const { Story } = defineMeta({
    title: 'Parts/PrusaSlicer/Waiting',
    component: SlicerWaiting,
  });
</script>

<Story name="Waiting" asChild>
  <EmbedFrame><SlicerWaiting /></EmbedFrame>
</Story>

<!-- The slicer sent again the token this server refused. -->
<Story name="Sign in again" asChild>
  <EmbedFrame><SlicerWaiting stale /></EmbedFrame>
</Story>
