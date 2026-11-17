<!-- Each bed as a card. The picked story shows one object's buttons pressed. -->
<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import {
    colourChanges,
    customLayers,
    gcodeOnly,
    sequential,
  } from '../test/project-fixtures-multimaterial';

  import {
    generic,
    manyBeds,
    multibed,
    orcaPlates,
    plateThumbnail,
    twoSetups,
    parked,
    parkedBed,
  } from '../test/project-fixtures';
  import ProjectBeds from './project-beds.svelte';

  const { Story } = defineMeta({
    title: '3MF/Project beds',
    component: ProjectBeds,
  });

  const emptyBed = {
    ...multibed,
    setups: multibed.setups.map((setup) => ({
      ...setup,
      beds: [
        ...setup.beds,
        {
          index: 3,
          offset: { x: 700, y: 0 },
          size: { x: 250, y: 210 },
          instances: [],
        },
      ],
    })),
  };
</script>

<Story name="Three Beds" asChild>
  <ProjectBeds project={multibed} thumbnailUrl={plateThumbnail} />
</Story>

<Story name="Named And Locked Plates With Pictures" asChild>
  <ProjectBeds project={orcaPlates} thumbnailUrl={plateThumbnail} />
</Story>

<Story name="Two Setups And A Copy On No Bed" asChild>
  <ProjectBeds project={twoSetups} />
</Story>

<Story name="Nine Beds" asChild>
  <ProjectBeds project={manyBeds} />
</Story>

<Story name="An Empty Bed" asChild>
  <ProjectBeds project={emptyBed} />
</Story>

<Story name="Bed Of Unknown Size" asChild>
  <ProjectBeds project={generic} />
</Story>

<!-- Copies left out of the print, one parked off the beds and one half off
     its bed, an empty bed, and a name wider than its letter. -->
<Story name="Parked Copies And An Empty Bed" asChild>
  <ProjectBeds project={parked} />
</Story>

<!-- A bed that holds only copies left out of the print says so. -->
<Story name="A Bed Of Parked Copies Only" asChild>
  <ProjectBeds project={parkedBed} />
</Story>

<Story name="Filament Changes" asChild>
  <ProjectBeds project={colourChanges} />
</Story>

<Story name="Custom G-code By Height" asChild>
  <ProjectBeds project={customLayers} />
</Story>

<Story name="Object By Object" asChild>
  <ProjectBeds project={sequential} />
</Story>

<Story name="G-code Only" asChild>
  <ProjectBeds project={gcodeOnly} />
</Story>
