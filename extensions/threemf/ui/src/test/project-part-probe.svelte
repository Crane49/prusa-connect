<!--
  One or more parts of the project view in one browser, for a test: the parts
  named in `parts`, in that order, all reading the same `ProjectBrowser`.
-->
<script lang="ts">
  import type { ProjectBrowser } from '../project-view/browser.svelte';
  import type { CopyRef, Project } from '../project-view/model';
  import ProjectBedSwitcher from '../project-view/project-bed-switcher.svelte';
  import ProjectObjectList from '../project-view/project-object-list.svelte';
  import ProjectObjectPanel from '../project-view/project-object-panel.svelte';
  import ProjectPlan from '../project-view/project-plan.svelte';
  import ProjectSelectionBar from '../project-view/project-selection-bar.svelte';
  import ProjectBrowsing from './project-browsing.svelte';

  type Part = 'switcher' | 'plan' | 'list' | 'bar' | 'panel';

  interface Props {
    project: Project;
    parts: readonly Part[];
    view?: string | undefined;
    ticked?: readonly CopyRef[] | undefined;
    opened?: number | undefined;
    thumbnailUrl?: ((index: number) => string) | undefined;
    onbrowser?: ((browser: ProjectBrowser) => void) | undefined;
  }

  let { project, parts, view, ticked, opened, thumbnailUrl, onbrowser }: Props =
    $props();
</script>

<ProjectBrowsing {project} {view} {ticked} {opened} {onbrowser}>
  <div class="@container/browser grid grid-cols-1 content-start gap-4">
    {#each parts as part (part)}
      {#if part === 'switcher'}
        <ProjectBedSwitcher {project} {thumbnailUrl} />
      {:else if part === 'plan'}
        <ProjectPlan {project} />
      {:else if part === 'list'}
        <ProjectObjectList {project} />
      {:else if part === 'bar'}
        <ProjectSelectionBar {project} />
      {:else}
        <ProjectObjectPanel {project} />
      {/if}
    {/each}
  </div>
</ProjectBrowsing>
