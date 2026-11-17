<!-- The settings summary, in each slicer's words read into the same rows. -->
<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import {
    generic,
    manyBeds,
    multibed,
    orcaPlates,
    twoSetups,
  } from '../test/project-fixtures';
  import ProjectSettings from './project-settings.svelte';

  const { Story } = defineMeta({
    title: '3MF/Project settings',
    component: ProjectSettings,
  });
</script>

<Story name="PrusaSlicer 2.9" asChild>
  <ProjectSettings project={multibed} />
</Story>

<Story name="Two Filaments And A High Flow Nozzle" asChild>
  <ProjectSettings project={manyBeds} />
</Story>

<Story name="Two Printer Setups" asChild>
  <ProjectSettings project={twoSetups} />
</Story>

<Story name="OrcaSlicer" asChild>
  <ProjectSettings project={orcaPlates} />
</Story>

<Story name="No Settings" asChild>
  <ProjectSettings project={generic} />
</Story>
