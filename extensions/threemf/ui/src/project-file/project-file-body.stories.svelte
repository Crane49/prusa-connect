<!--
  The body of the project file view, which asks the store for the file when
  it is mounted. `project-file.stories.svelte` draws every state through the
  view around it; these are the two a body draws on its own.
-->
<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import { holdFakeProjects } from '../test/projects';
  import ProjectFileBody from './project-file-body.svelte';

  holdFakeProjects();

  const { Story } = defineMeta({
    title: '3MF/Project file body',
    component: ProjectFileBody,
  });
</script>

<Story name="Loaded" asChild>
  <ProjectFileBody storage="local" path="projects/multibed.3mf" />
</Story>

<Story name="Loading" asChild>
  <ProjectFileBody storage="local" path="projects/loading.3mf" />
</Story>
