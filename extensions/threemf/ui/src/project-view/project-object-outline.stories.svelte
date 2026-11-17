<!--
  One copy seen from above, with its width and depth.
-->
<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import { multibed, parked } from '../test/project-fixtures';
  import ProjectObjectOutline from './project-object-outline.svelte';

  const { Story } = defineMeta({
    title: '3MF/Project object outline',
    component: ProjectObjectOutline,
  });

  const cube = multibed.objects[0]?.instances[0];
  const sign = parked.objects[5]?.instances[0];
</script>

{#if cube !== undefined}
  <Story name="In Its Filament Colour" asChild>
    <div class="size-72 bg-muted/40 p-2">
      <ProjectObjectOutline
        instance={cube}
        colour="#E25C15"
        label="A cube seen from above"
      />
    </div>
  </Story>

  <Story name="No Colour Named" asChild>
    <div class="size-72 bg-muted/40 p-2">
      <ProjectObjectOutline instance={cube} label="A cube seen from above" />
    </div>
  </Story>
{/if}

{#if sign !== undefined}
  <Story name="A Long Flat Copy" asChild>
    <div class="size-72 bg-muted/40 p-2">
      <ProjectObjectOutline
        instance={sign}
        colour="#FFFFFF"
        label="A sign seen from above"
      />
    </div>
  </Story>
{/if}
