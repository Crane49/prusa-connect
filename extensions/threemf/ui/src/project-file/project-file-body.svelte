<!--
  One 3MF file, read through the store and drawn by the project view. The
  read is asked for when the view is mounted, and stopped when it leaves.
-->
<script lang="ts">
  import { onMount } from 'svelte';

  import { ProjectView } from '../project-view';
  import { heldProjects } from '../session';
  import type { ProjectFileProps } from './types';

  let { storage, path, selectionActs, onselect, picture }: ProjectFileProps =
    $props();

  const projects = heldProjects();

  const read = $derived(projects.described(storage, path));

  onMount(() => {
    const reading = new AbortController();
    void projects.describe(storage, path, reading.signal);

    return () => {
      reading.abort();
    };
  });
</script>

<ProjectView
  project={read?.project}
  fault={read?.fault}
  loading={read === undefined}
  thumbnailUrl={(index: number) => projects.thumbnailUrl(storage, path, index)}
  mesh={(object: number) => projects.describedMesh(storage, path, object)}
  loadMesh={(object: number) => {
    void projects.describeMesh(storage, path, object);
  }}
  {selectionActs}
  {onselect}
  {picture}
/>
