<!--
  What could not be read. Omitted None: the component draws nothing for a
  project read whole.
-->
<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import { bambuPlates, withIssues } from '../test/project-fixtures';
  import ProjectIssues from './project-issues.svelte';

  const { Story } = defineMeta({
    title: '3MF/Project issues',
    component: ProjectIssues,
  });
</script>

<Story name="Several Issues" asChild>
  <ProjectIssues issues={withIssues.issues} />
</Story>

<Story name="Several Issues Opened" asChild>
  <ProjectIssues issues={withIssues.issues} open />
</Story>

<Story name="Unsupported Producer" asChild>
  <ProjectIssues issues={bambuPlates.issues} open />
</Story>

<Story name="Unsupported Extension" asChild>
  <ProjectIssues
    open
    issues={[
      {
        code: 'unsupported_extension',
        detail:
          'http://schemas.microsoft.com/3dmanufacturing/beamlattice/2017/02',
      },
    ]}
  />
</Story>
