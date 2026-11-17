<!--
  The plan of the beds alone, in the context of a project view. A copy takes
  the colour of its filament; the ticked stories show the brand outline on
  the ticked copies and the others faded.
-->
<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import {
    generic,
    legacyRow,
    manyBeds,
    multibed,
    orcaPlates,
    twoSetups,
    parked,
    parkedBed,
  } from '../test/project-fixtures';
  import {
    indxPainted,
    mmuObjects,
  } from '../test/project-fixtures-multimaterial';
  import ProjectBrowsing from '../test/project-browsing.svelte';
  import ProjectPlan from './project-plan.svelte';

  const { Story } = defineMeta({
    title: '3MF/Project plan',
    component: ProjectPlan,
  });
</script>

<Story name="Every Bed Of PrusaSlicer 2.9" asChild>
  <ProjectBrowsing project={multibed} view="all">
    <ProjectPlan project={multibed} />
  </ProjectBrowsing>
</Story>

<Story name="One Bed Of PrusaSlicer 2.9" asChild>
  <ProjectBrowsing project={multibed} view="0:0">
    <ProjectPlan project={multibed} />
  </ProjectBrowsing>
</Story>

<Story name="PrusaSlicer 2.8 Row" asChild>
  <ProjectBrowsing project={legacyRow} view="all">
    <ProjectPlan project={legacyRow} />
  </ProjectBrowsing>
</Story>

<Story name="Two Printer Setups" asChild>
  <ProjectBrowsing project={twoSetups} view="all">
    <ProjectPlan project={twoSetups} />
  </ProjectBrowsing>
</Story>

<Story name="Nine Beds Of 109 Copies" asChild>
  <ProjectBrowsing project={manyBeds} view="all">
    <ProjectPlan project={manyBeds} />
  </ProjectBrowsing>
</Story>

<Story name="One Bed Of Many Copies" asChild>
  <ProjectBrowsing project={manyBeds} view="0:0">
    <ProjectPlan project={manyBeds} />
  </ProjectBrowsing>
</Story>

<Story name="OrcaSlicer Plates" asChild>
  <ProjectBrowsing project={orcaPlates} view="all">
    <ProjectPlan project={orcaPlates} />
  </ProjectBrowsing>
</Story>

<Story name="Bed Of Unknown Size" asChild>
  <ProjectBrowsing project={generic}>
    <ProjectPlan project={generic} />
  </ProjectBrowsing>
</Story>

<Story name="Copies In Filament Colours" asChild>
  <ProjectBrowsing project={mmuObjects}>
    <ProjectPlan project={mmuObjects} />
  </ProjectBrowsing>
</Story>

<Story name="Painted Object" asChild>
  <ProjectBrowsing project={indxPainted}>
    <ProjectPlan project={indxPainted} />
  </ProjectBrowsing>
</Story>

<Story name="Copies Ticked" asChild>
  <ProjectBrowsing
    project={multibed}
    view="all"
    ticked={[
      { object: 0, instance: 0 },
      { object: 0, instance: 2 },
    ]}
  >
    <ProjectPlan project={multibed} />
  </ProjectBrowsing>
</Story>

<Story name="An Object Marked" asChild>
  <ProjectBrowsing project={multibed} view="all" active={1}>
    <ProjectPlan project={multibed} />
  </ProjectBrowsing>
</Story>

<!-- Copies left out of the print, one parked off the beds and one half off
     its bed, an empty bed, and a name wider than its letter. -->
<Story name="Parked Copies And An Empty Bed" asChild>
  <ProjectBrowsing project={parked} view="all">
    <ProjectPlan project={parked} />
  </ProjectBrowsing>
</Story>

<Story name="Only The Copies On No Bed" asChild>
  <ProjectBrowsing project={parked} view="off">
    <ProjectPlan project={parked} />
  </ProjectBrowsing>
</Story>

<!-- A bed that holds only copies left out of the print says so. -->
<Story name="A Bed Of Parked Copies Only" asChild>
  <ProjectBrowsing project={parkedBed} view="all">
    <ProjectPlan project={parkedBed} />
  </ProjectBrowsing>
</Story>
