<!--
  The project view of one 3MF file in a storage: what this module draws on
  the file's page, and what it hands other modules as `project-view`.

  A new file is a new view, so what was ticked on one file never carries
  over to the next.
-->
<script lang="ts">
  import ProjectFileBody from './project-file-body.svelte';
  import type { ProjectFileProps } from './types';

  let { storage, path, selectionActs, onselect, picture }: ProjectFileProps =
    $props();
</script>

{#key `${storage}:${path}`}
  <ProjectFileBody {storage} {path} {selectionActs} {onselect} {picture} />
{/key}
