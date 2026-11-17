<!--
  The project view of a file in a storage, as this module draws it on the
  file's page and hands it to other modules. It reads the store, here over a
  fake service that answers by path.
-->
<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import { Button } from '@prusa/connect-ui/primitives';

  import type { ProjectSelectionActs } from '../project-view';
  import { holdFakeProjects } from '../test/projects';
  import ProjectFile from './project-file.svelte';

  holdFakeProjects();

  const { Story } = defineMeta({
    title: '3MF/Project file',
    component: ProjectFile,
  });
</script>

<Story name="Loaded" asChild>
  <ProjectFile storage="local" path="projects/multibed.3mf" />
</Story>

<Story name="With A Page Act" asChild>
  <ProjectFile storage="local" path="projects/multibed.3mf">
    {#snippet selectionActs(acts: ProjectSelectionActs)}
      <Button size="sm" disabled={acts.summary.copies === 0}>Slice these</Button
      >
    {/snippet}
  </ProjectFile>
</Story>

<Story name="Loading" asChild>
  <ProjectFile storage="local" path="projects/loading.3mf" />
</Story>

<Story name="Failed" asChild>
  <ProjectFile storage="local" path="projects/damaged.3mf" />
</Story>

<Story name="Refused" asChild>
  <ProjectFile storage="local" path="projects/hidden.3mf" />
</Story>
