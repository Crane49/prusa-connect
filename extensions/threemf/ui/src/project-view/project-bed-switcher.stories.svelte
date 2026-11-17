<!--
  The strip that picks the bed the plan shows. Omitted One Bed: the strip
  draws nothing for a project of one bed and no copy off it.
-->
<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import ProjectBrowsing from '../test/project-browsing.svelte';
  import {
    manyBeds,
    multibed,
    orcaPlates,
    parked,
    plateThumbnail,
    twoSetups,
  } from '../test/project-fixtures';
  import ProjectBedSwitcher from './project-bed-switcher.svelte';

  const { Story } = defineMeta({
    title: '3MF/Project bed switcher',
    component: ProjectBedSwitcher,
  });
</script>

<Story name="Three Beds With Pictures" asChild>
  <ProjectBrowsing project={multibed}>
    <ProjectBedSwitcher project={multibed} thumbnailUrl={plateThumbnail} />
  </ProjectBrowsing>
</Story>

<Story name="Every Bed Shown" asChild>
  <ProjectBrowsing project={multibed} view="all">
    <ProjectBedSwitcher project={multibed} thumbnailUrl={plateThumbnail} />
  </ProjectBrowsing>
</Story>

<Story name="Named Plates" asChild>
  <ProjectBrowsing project={orcaPlates}>
    <ProjectBedSwitcher project={orcaPlates} thumbnailUrl={plateThumbnail} />
  </ProjectBrowsing>
</Story>

<Story name="Two Printer Setups" asChild>
  <ProjectBrowsing project={twoSetups}>
    <ProjectBedSwitcher project={twoSetups} />
  </ProjectBrowsing>
</Story>

<Story name="Nine Beds Scroll" asChild>
  <div class="max-w-96">
    <ProjectBrowsing project={manyBeds}>
      <ProjectBedSwitcher project={manyBeds} />
    </ProjectBrowsing>
  </div>
</Story>

<Story name="Copies On No Bed" asChild>
  <ProjectBrowsing project={parked} view="off">
    <ProjectBedSwitcher project={parked} />
  </ProjectBrowsing>
</Story>
