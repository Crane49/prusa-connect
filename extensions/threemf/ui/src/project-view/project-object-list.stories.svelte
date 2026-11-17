<!--
  The objects of the bed the plan shows, with a box to tick each.
-->
<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import ProjectBrowsing from '../test/project-browsing.svelte';
  import {
    longNames,
    manyBeds,
    multibed,
    parked,
  } from '../test/project-fixtures';
  import {
    indxPainted,
    mmuObjects,
  } from '../test/project-fixtures-multimaterial';
  import ProjectObjectList from './project-object-list.svelte';

  const { Story } = defineMeta({
    title: '3MF/Project object list',
    component: ProjectObjectList,
  });
</script>

<Story name="One Bed" asChild>
  <ProjectBrowsing project={multibed} view="0:0">
    <ProjectObjectList project={multibed} />
  </ProjectBrowsing>
</Story>

<Story name="Every Bed" asChild>
  <ProjectBrowsing project={multibed} view="all">
    <ProjectObjectList project={multibed} />
  </ProjectBrowsing>
</Story>

<Story name="Some Ticked" asChild>
  <ProjectBrowsing
    project={multibed}
    view="all"
    ticked={[{ object: 0, instance: 0 }]}
  >
    <ProjectObjectList project={multibed} />
  </ProjectBrowsing>
</Story>

<Story name="An Object Marked From The Plan" asChild>
  <ProjectBrowsing project={multibed} view="all" active={1}>
    <ProjectObjectList project={multibed} />
  </ProjectBrowsing>
</Story>

<Story name="Five Extruders" asChild>
  <div class="@container/browser">
    <ProjectBrowsing project={mmuObjects}>
      <ProjectObjectList project={mmuObjects} />
    </ProjectBrowsing>
  </div>
</Story>

<Story name="Painted Object" asChild>
  <div class="@container/browser">
    <ProjectBrowsing project={indxPainted}>
      <ProjectObjectList project={indxPainted} />
    </ProjectBrowsing>
  </div>
</Story>

<Story name="Copies That Cannot Be Ticked" asChild>
  <ProjectBrowsing project={parked} view="off">
    <ProjectObjectList project={parked} />
  </ProjectBrowsing>
</Story>

<Story name="Long Names" asChild>
  <div class="max-w-80">
    <ProjectBrowsing project={longNames}>
      <ProjectObjectList project={longNames} />
    </ProjectBrowsing>
  </div>
</Story>

<Story name="A Hundred Objects" asChild>
  <ProjectBrowsing project={manyBeds} view="all">
    <ProjectObjectList project={manyBeds} />
  </ProjectBrowsing>
</Story>

<Story name="Empty Bed" asChild>
  <ProjectBrowsing project={parked} view="0:1">
    <ProjectObjectList project={parked} />
  </ProjectBrowsing>
</Story>
