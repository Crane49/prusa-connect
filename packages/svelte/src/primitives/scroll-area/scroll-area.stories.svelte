<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import ScrollArea from './scroll-area.svelte';

  const { Story } = defineMeta({
    title: 'Primitives/ScrollArea',
    component: ScrollArea,
  });

  const lines = Array.from(
    { length: 40 },
    (_, index) =>
      `[00:00:${String(index).padStart(2, '0')}] heater target reached`,
  );
</script>

{#snippet logViewer()}
  <ScrollArea class="h-48 w-72 rounded-sm border border-border p-4">
    <div class="flex flex-col gap-1 font-mono text-xs text-muted-foreground">
      {#each lines as line (line)}
        <p>{line}</p>
      {/each}
    </div>
  </ScrollArea>
{/snippet}

<Story name="Default" asChild children={logViewer} />

<!-- The scrollbar is mounted once the pointer enters the area, so there is no
     story of a hovered thumb: it cannot be reached from a still picture. -->
<Story
  name="Overflowing Viewport Is Keyboard Reachable"
  tags={['!dev', '!autodocs']}
  asChild
  children={logViewer}
/>
