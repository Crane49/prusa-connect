<!--
  Hands the toolbar's `printers` global to whatever the story draws, so
  `AppAt` follows it without a story template reading Storybook's untyped
  globals. Nothing is wrapped around the story.
-->
<script lang="ts">
  import { setContext, type Snippet } from 'svelte';

  import { type FleetSize, SHOWN_FLEET } from './fleet';

  interface Props {
    size: FleetSize;
    children?: Snippet;
  }

  const { size, children }: Props = $props();

  setContext(SHOWN_FLEET, () => size);
</script>

{@render children?.()}
