<!-- The parts of one object and its layer ranges, as the object table opens them. -->
<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import { longNames } from '../test/project-fixtures';
  import {
    indxPainted,
    layerRanges,
    mmuObjects,
    orcaPainted,
  } from '../test/project-fixtures-multimaterial';
  import ProjectObjectParts from './project-object-parts.svelte';

  const { Story } = defineMeta({
    title: '3MF/Project object parts',
    component: ProjectObjectParts,
  });
</script>

<Story name="Parts On Five Extruders" asChild>
  <ProjectObjectParts project={mmuObjects} object={1} />
</Story>

<Story name="Painted With Four Tools" asChild>
  <ProjectObjectParts project={indxPainted} object={0} />
</Story>

<Story name="Painted Colours And Seam" asChild>
  <ProjectObjectParts project={orcaPainted} object={0} />
</Story>

<Story name="Modifiers, Negative Volumes And Support" asChild>
  <ProjectObjectParts project={longNames} object={0} />
</Story>

<Story name="Layer Ranges" asChild>
  <ProjectObjectParts project={layerRanges} object={2} />
</Story>
