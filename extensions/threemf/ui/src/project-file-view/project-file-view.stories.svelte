<!--
  What this module draws in the file-view slot of a .3mf file's page. The
  page hands the file as `<storage>:<path>`.
-->
<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import { holdFakeProjects } from '../test/projects';
  import ProjectFileView from './project-file-view.svelte';

  holdFakeProjects();

  const { Story } = defineMeta({
    title: '3MF/File view',
    component: ProjectFileView,
  });
</script>

<Story name="Loaded" asChild>
  <ProjectFileView fileRef="local:projects/multibed.3mf" />
</Story>

<Story name="Loading" asChild>
  <ProjectFileView fileRef="local:projects/loading.3mf" />
</Story>

<Story name="Failed" asChild>
  <ProjectFileView fileRef="local:projects/damaged.3mf" />
</Story>

<Story name="Not A File Reference" asChild>
  <ProjectFileView fileRef="no-colon" />
</Story>
