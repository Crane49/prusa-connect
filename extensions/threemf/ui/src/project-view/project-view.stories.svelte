<!--
  The whole project view, over the three projects the threemf Go package tests itself on
  and over projects drawn by hand for the shapes those files lack.
-->
<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import {
    colourChanges,
    gcodeOnly,
    indxPainted,
    layerRanges,
    mmuObjects,
    mmuPainted,
    orcaPainted,
  } from '../test/project-fixtures-multimaterial';

  import {
    bambuPlates,
    generic,
    legacyRow,
    longNames,
    manyBeds,
    multibed,
    notAProject,
    orcaPlates,
    plateThumbnail,
    twoSetups,
    withIssues,
    parked,
  } from '../test/project-fixtures';
  import { Button } from '@prusa/connect-ui/primitives';
  import ProjectView from './project-view.svelte';
  import type { ProjectSelectionActs } from './selection';

  const { Story } = defineMeta({
    title: '3MF/Project view',
    component: ProjectView,
  });
</script>

<Story name="PrusaSlicer 2.9 Multibed" asChild>
  <ProjectView project={multibed} thumbnailUrl={plateThumbnail} />
</Story>

<Story name="With A Page Act" asChild>
  <ProjectView project={multibed} thumbnailUrl={plateThumbnail}>
    {#snippet selectionActs(acts: ProjectSelectionActs)}
      <Button size="sm" disabled={acts.summary.copies === 0}>Print these</Button
      >
    {/snippet}
  </ProjectView>
</Story>

<Story name="On A File's Page" asChild>
  <ProjectView
    project={multibed}
    thumbnailUrl={plateThumbnail}
    picture={false}
  />
</Story>

<Story name="PrusaSlicer 2.8 Row" asChild>
  <ProjectView project={legacyRow} thumbnailUrl={plateThumbnail} />
</Story>

<Story name="PrusaSlicer 3 Two Setups" asChild>
  <ProjectView project={twoSetups} thumbnailUrl={plateThumbnail} />
</Story>

<Story name="Many Beds" asChild>
  <ProjectView project={manyBeds} thumbnailUrl={plateThumbnail} />
</Story>

<Story name="Long Names" asChild>
  <ProjectView project={longNames} thumbnailUrl={plateThumbnail} />
</Story>

<Story name="OrcaSlicer Plates" asChild>
  <ProjectView project={orcaPlates} thumbnailUrl={plateThumbnail} />
</Story>

<Story name="Unsupported BambuStudio" asChild>
  <ProjectView project={bambuPlates} thumbnailUrl={plateThumbnail} />
</Story>

<Story name="Generic 3MF" asChild>
  <ProjectView project={generic} thumbnailUrl={plateThumbnail} />
</Story>

<Story name="With Issues" asChild>
  <ProjectView project={withIssues} thumbnailUrl={plateThumbnail} />
</Story>

<Story name="Loading" asChild>
  <ProjectView loading />
</Story>

<Story name="Failed" asChild>
  <ProjectView fault={notAProject} />
</Story>

<!-- Copies left out of the print, one parked off the beds and one half off
     its bed, an empty bed, and a name wider than its letter. -->
<Story name="Parked Copies And An Empty Bed" asChild>
  <ProjectView project={parked} thumbnailUrl={plateThumbnail} />
</Story>

<Story name="MMU Objects" asChild>
  <ProjectView project={mmuObjects} />
</Story>

<Story name="MMU Painted" asChild>
  <ProjectView project={mmuPainted} />
</Story>

<Story name="INDX Painted" asChild>
  <ProjectView project={indxPainted} />
</Story>

<Story name="OrcaSlicer Multicolour" asChild>
  <ProjectView project={orcaPainted} />
</Story>

<Story name="Filament Changes" asChild>
  <ProjectView project={colourChanges} />
</Story>

<Story name="Layer Ranges" asChild>
  <ProjectView project={layerRanges} />
</Story>

<Story name="G-code Only" asChild>
  <ProjectView project={gcodeOnly} />
</Story>
