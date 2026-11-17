<!--
  The project at a glance, over the fixtures the threemf Go package tests itself on and
  over projects drawn by hand for the shapes those files lack.
-->
<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import {
    bambuPlates,
    generic,
    longNames,
    multibed,
    parked,
    plateThumbnail,
    twoSetups,
    withIssues,
  } from '../test/project-fixtures';
  import {
    gcodeOnly,
    indxPainted,
    mmuObjects,
  } from '../test/project-fixtures-multimaterial';
  import ProjectOverview from './project-overview.svelte';

  const { Story } = defineMeta({
    title: '3MF/Project overview',
    component: ProjectOverview,
  });
</script>

<Story name="PrusaSlicer 2.9 With Pictures" asChild>
  <ProjectOverview project={multibed} thumbnailUrl={plateThumbnail} />
</Story>

<Story name="On A File's Page" asChild>
  <ProjectOverview
    project={multibed}
    thumbnailUrl={plateThumbnail}
    picture={false}
  />
</Story>

<Story name="No Picture" asChild>
  <ProjectOverview project={multibed} />
</Story>

<Story name="Five Filaments" asChild>
  <ProjectOverview project={mmuObjects} />
</Story>

<Story name="INDX Painted" asChild>
  <ProjectOverview project={indxPainted} />
</Story>

<Story name="Two Printer Setups" asChild>
  <ProjectOverview project={twoSetups} thumbnailUrl={plateThumbnail} />
</Story>

<Story name="Copies Left Out And Off The Beds" asChild>
  <ProjectOverview project={parked} thumbnailUrl={plateThumbnail} />
</Story>

<Story name="Unsupported BambuStudio" asChild>
  <ProjectOverview project={bambuPlates} thumbnailUrl={plateThumbnail} />
</Story>

<Story name="Generic 3MF" asChild>
  <ProjectOverview project={generic} />
</Story>

<Story name="With Issues" asChild>
  <ProjectOverview project={withIssues} />
</Story>

<Story name="Long Names" asChild>
  <ProjectOverview project={longNames} />
</Story>

<Story name="G-code Only" asChild>
  <ProjectOverview project={gcodeOnly} />
</Story>
