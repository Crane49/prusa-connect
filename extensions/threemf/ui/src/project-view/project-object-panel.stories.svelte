<!--
  One object in its panel. Omitted Closed: the panel draws nothing while no
  object is open.
-->
<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import ProjectBrowsing from '../test/project-browsing.svelte';
  import { longNames, multibed, parked } from '../test/project-fixtures';
  import {
    indxPainted,
    layerRanges,
    mmuObjects,
  } from '../test/project-fixtures-multimaterial';
  import { boxMesh, simplifiedMesh, twoPartMesh } from '../test/project-meshes';
  import type { MeshDescription } from '../projects.svelte';
  import ProjectObjectPanel from './project-object-panel.svelte';

  const answered =
    (description: MeshDescription | undefined) =>
    (): MeshDescription | undefined =>
      description;

  const { Story } = defineMeta({
    title: '3MF/Project object panel',
    component: ProjectObjectPanel,
  });
</script>

<Story name="Several Copies" asChild>
  <ProjectBrowsing
    project={multibed}
    opened={0}
    ticked={[{ object: 0, instance: 1 }]}
  >
    <ProjectObjectPanel project={multibed} />
  </ProjectBrowsing>
</Story>

<Story name="Five Parts On Five Extruders" asChild>
  <ProjectBrowsing project={mmuObjects} opened={0}>
    <ProjectObjectPanel project={mmuObjects} />
  </ProjectBrowsing>
</Story>

<Story name="Painted" asChild>
  <ProjectBrowsing project={indxPainted} opened={0}>
    <ProjectObjectPanel project={indxPainted} />
  </ProjectBrowsing>
</Story>

<Story name="Layer Ranges" asChild>
  <ProjectBrowsing project={layerRanges} opened={0}>
    <ProjectObjectPanel project={layerRanges} />
  </ProjectBrowsing>
</Story>

<Story name="Copies That Cannot Be Ticked" asChild>
  <ProjectBrowsing project={parked} opened={5}>
    <ProjectObjectPanel project={parked} />
  </ProjectBrowsing>
</Story>

<Story name="Long Name" asChild>
  <ProjectBrowsing project={longNames} opened={0}>
    <ProjectObjectPanel project={longNames} />
  </ProjectBrowsing>
</Story>

<Story name="In 3D" asChild>
  <ProjectBrowsing project={mmuObjects} opened={0}>
    <ProjectObjectPanel
      project={mmuObjects}
      mesh={answered({ mesh: twoPartMesh })}
    />
  </ProjectBrowsing>
</Story>

<Story name="Shape On Its Way" asChild>
  <ProjectBrowsing project={mmuObjects} opened={0}>
    <ProjectObjectPanel project={mmuObjects} mesh={answered(undefined)} />
  </ProjectBrowsing>
</Story>

<Story name="Shape Made Simpler" asChild>
  <ProjectBrowsing project={multibed} opened={0}>
    <ProjectObjectPanel
      project={multibed}
      mesh={answered({ mesh: simplifiedMesh })}
    />
  </ProjectBrowsing>
</Story>

<Story name="Shape Too Large To Keep" asChild>
  <ProjectBrowsing project={multibed} opened={0}>
    <ProjectObjectPanel project={multibed} mesh={answered({ mesh: boxMesh })} />
  </ProjectBrowsing>
</Story>

<Story name="Shape Refused" asChild>
  <ProjectBrowsing project={multibed} opened={0}>
    <ProjectObjectPanel
      project={multibed}
      mesh={answered({
        fault: {
          code: 'threemf/NO_OBJECT',
          status: 404,
          params: {},
          message: undefined,
          traceId: undefined,
        },
      })}
    />
  </ProjectBrowsing>
</Story>
