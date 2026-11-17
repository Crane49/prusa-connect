<!--
  What the ticked copies add up to.
-->
<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import ProjectBrowsing from '../test/project-browsing.svelte';
  import { multibed } from '../test/project-fixtures';
  import { mmuObjects } from '../test/project-fixtures-multimaterial';
  import { Button } from '@prusa/connect-ui/primitives';
  import ProjectSelectionBar from './project-selection-bar.svelte';
  import type { ProjectSelectionActs } from './selection';

  const { Story } = defineMeta({
    title: '3MF/Project selection bar',
    component: ProjectSelectionBar,
  });
</script>

<Story name="Nothing Ticked" asChild>
  <ProjectBrowsing project={multibed}>
    <ProjectSelectionBar project={multibed} />
  </ProjectBrowsing>
</Story>

<Story name="Copies On Two Beds" asChild>
  <ProjectBrowsing
    project={multibed}
    ticked={multibed.objects.flatMap((object, o) =>
      object.instances.map((_, i) => ({ object: o, instance: i })),
    )}
  >
    <ProjectSelectionBar project={multibed} />
  </ProjectBrowsing>
</Story>

<Story name="Several Extruders" asChild>
  <ProjectBrowsing
    project={mmuObjects}
    ticked={[
      { object: 0, instance: 0 },
      { object: 1, instance: 0 },
    ]}
  >
    <ProjectSelectionBar project={mmuObjects} />
  </ProjectBrowsing>
</Story>

<Story name="With A Page Act" asChild>
  <ProjectBrowsing project={multibed} ticked={[{ object: 0, instance: 0 }]}>
    <ProjectSelectionBar project={multibed}>
      {#snippet acts(held: ProjectSelectionActs)}
        <Button size="sm" onclick={held.clear}>
          Print {held.summary.copies}
        </Button>
      {/snippet}
    </ProjectSelectionBar>
  </ProjectBrowsing>
</Story>
