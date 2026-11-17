<!--
  A 3MF project, for the page of its file, in the order a person reads it:
  what it is at a glance, then the beds with what stands on them and the
  detail of the bed shown, and last the settings.

  The beds are browsed one at a time: a strip picks the bed, the plan draws
  it large, and the list beside it names its objects. A page that does
  something with ticked copies gets a box to tick each; elsewhere a press on
  a copy opens its object.
  Ticking, the pointer and the open object are the view's own state, in a
  `ProjectBrowser` every part reads from context, so the plan, the list, the
  selection bar and the object panel always agree.
-->
<script lang="ts">
  import { type Snippet, untrack } from 'svelte';

  import FileCodeIcon from '@lucide/svelte/icons/file-code';
  import FileXIcon from '@lucide/svelte/icons/file-x';
  import LayoutGridIcon from '@lucide/svelte/icons/layout-grid';
  import SlidersHorizontalIcon from '@lucide/svelte/icons/sliders-horizontal';

  import type { MeshDescription } from '../projects.svelte';
  import { describeFault, t } from '../i18n';
  import {
    Card,
    CardContent,
    EmptyState,
    Skeleton,
  } from '@prusa/connect-ui/primitives';
  import {
    type BedSelection,
    ProjectBrowser,
    setProjectBrowser,
  } from './browser.svelte';
  import { bedsWithGCode, isGCodeOnly, type Project } from './model';
  import ProjectBedSwitcher from './project-bed-switcher.svelte';
  import ProjectBeds from './project-beds.svelte';
  import ProjectObjectList from './project-object-list.svelte';
  import ProjectObjectPanel from './project-object-panel.svelte';
  import ProjectOverview from './project-overview.svelte';
  import ProjectPlan from './project-plan.svelte';
  import ProjectSelectionBar from './project-selection-bar.svelte';
  import ProjectSettings from './project-settings.svelte';
  import type { ProjectSelectionActs } from './selection';

  interface Props {
    /** The project, once the server has read it. */
    project?: Project | undefined;
    /** Why the server could not read the project. Rendered by `describeFault`. */
    fault?: unknown;
    /** The read is on its way. */
    loading?: boolean | undefined;
    /** Where the picture at an index of the project's thumbnails is served. */
    thumbnailUrl?: ((index: number) => string) | undefined;
    /** The object at an index as triangles, once the server has answered. */
    mesh?: ((object: number) => MeshDescription | undefined) | undefined;
    /** Asks the server for the object at an index as triangles. */
    loadMesh?: ((object: number) => void) | undefined;
    /**
     * A page's buttons for the ticked copies, drawn in the selection bar,
     * such as one that prints them.
     */
    selectionActs?: Snippet<[ProjectSelectionActs]> | undefined;
    /**
     * Told the ticked copies, one selection per bed, each time a person
     * changes them.
     */
    onselect?: ((selections: readonly BedSelection[]) => void) | undefined;
    /**
     * Whether the overview draws the project's picture. False on a file's
     * page, whose header already shows it.
     */
    picture?: boolean | undefined;
  }

  let {
    project,
    fault,
    loading = false,
    thumbnailUrl,
    mesh,
    loadMesh,
    selectionActs,
    onselect,
    picture = true,
  }: Props = $props();

  // The browser reads the project through a getter, so a new answer for the
  // same file keeps what was ticked where it still applies.
  const EMPTY: Project = {
    producer: { supported: true, dialect: 'generic' },
    counts: {
      setups: 0,
      beds: 0,
      objects: 0,
      instances: 0,
      non_printable: 0,
      partly_outside: 0,
      outside: 0,
    },
    setups: [],
    objects: [],
    thumbnails: [],
    issues: [],
  };
  // Ticking copies is for a page that does something with them. Read once,
  // because a page does not change what it offers while it is open.
  const browser = new ProjectBrowser(
    () => project ?? EMPTY,
    (object) => loadMesh?.(object),
    (selections) => onselect?.(selections),
    untrack(() => selectionActs !== undefined || onselect !== undefined),
  );
  setProjectBrowser(browser);

  const refusal = $derived(
    fault === undefined ? undefined : describeFault(fault),
  );

  const gcodeOnly = $derived(project !== undefined && isGCodeOnly(project));
</script>

<div
  class="@container/project grid grid-cols-1 content-start gap-6"
  data-slot="project-view"
>
  {#if loading}
    <Card>
      <CardContent
        class="grid grid-cols-1 content-start gap-5 @2xl/project:flex"
        role="status"
        aria-label={t('projectView.loading')}
      >
        <Skeleton class="aspect-square w-full max-w-72 @2xl/project:w-56" />
        <div class="grid flex-1 grid-cols-1 content-start gap-3">
          <Skeleton class="h-6 w-48" />
          <Skeleton class="h-5 w-64" />
          <Skeleton class="h-8 w-80 max-w-full" />
          <Skeleton class="h-4 w-56" />
        </div>
      </CardContent>
    </Card>
    <Card>
      <CardContent>
        <Skeleton class="aspect-video w-full" />
      </CardContent>
    </Card>
  {:else if refusal !== undefined}
    <Card>
      <CardContent>
        <EmptyState
          icon={FileXIcon}
          heading={t('projectView.failed.heading')}
          headingLevel="h2"
          description={refusal.title}
        >
          {#snippet action()}
            {#if refusal.code !== undefined}
              <code class="font-mono text-xs text-muted-foreground">
                {refusal.code}
              </code>
            {/if}
          {/snippet}
        </EmptyState>
      </CardContent>
    </Card>
  {:else if project !== undefined}
    <Card>
      <CardContent>
        <ProjectOverview {project} {thumbnailUrl} {picture} />
      </CardContent>
    </Card>

    {#if gcodeOnly}
      <Card>
        <CardContent class="grid grid-cols-1 content-start gap-4">
          <EmptyState
            icon={FileCodeIcon}
            heading={t('projectView.gcodeOnly.heading')}
            headingLevel="h2"
            description={t('projectView.gcodeOnly.description', {
              count: bedsWithGCode(project),
            })}
          />
          <ProjectBeds {project} {thumbnailUrl} />
        </CardContent>
      </Card>
    {:else}
      <Card>
        <CardContent
          class="@container/browser grid grid-cols-1 content-start gap-4"
          data-slot="project-browser"
        >
          <h2 class="flex items-center gap-2 text-sm font-medium">
            <LayoutGridIcon
              aria-hidden="true"
              class="size-4 text-muted-foreground"
            />
            {t('projectView.browser.heading')}
          </h2>
          <ProjectBedSwitcher {project} {thumbnailUrl} />
          <div
            class="grid grid-cols-1 items-start gap-6 @4xl/browser:grid-cols-5"
          >
            <div class="min-w-0 @4xl/browser:col-span-3">
              <ProjectPlan {project} />
            </div>
            <div class="min-w-0 @4xl/browser:col-span-2">
              <ProjectObjectList {project} />
            </div>
          </div>
          {#if browser.focus !== undefined && browser.focus !== 'off'}
            <ProjectBeds {project} {thumbnailUrl} only={browser.focus} />
          {/if}
          {#if browser.ticking}
            <ProjectSelectionBar {project} acts={selectionActs} />
          {/if}
        </CardContent>
      </Card>
      <ProjectObjectPanel {project} {mesh} />
    {/if}

    <Card>
      <CardContent class="grid grid-cols-1 content-start gap-4">
        <h2 class="flex items-center gap-2 text-sm font-medium">
          <SlidersHorizontalIcon
            aria-hidden="true"
            class="size-4 text-muted-foreground"
          />
          {t('projectView.settings.heading')}
        </h2>
        <ProjectSettings {project} />
      </CardContent>
    </Card>
  {/if}
</div>
