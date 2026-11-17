<!--
  The context a part of the project view is drawn in, for a story or a test:
  a `ProjectBrowser` over one project, set to a view, with copies ticked, an
  object marked and one open, as given.
-->
<script lang="ts">
  import type { Snippet } from 'svelte';

  import {
    ProjectBrowser,
    setProjectBrowser,
  } from '../project-view/browser.svelte';
  import type { CopyRef, Project } from '../project-view/model';

  interface Props {
    project: Project;
    /** The view to open on: a bed's key such as `0:1`, `all` or `off`. */
    view?: string | undefined;
    ticked?: readonly CopyRef[] | undefined;
    active?: number | undefined;
    opened?: number | undefined;
    /** Handed the browser, so a test can read what the parts did to it. */
    onbrowser?: ((browser: ProjectBrowser) => void) | undefined;
    children: Snippet;
  }

  let { project, view, ticked, active, opened, onbrowser, children }: Props =
    $props();

  // The setup is read once, when the frame is drawn, the way a page starts.
  const start = (): ProjectBrowser => {
    const browser = new ProjectBrowser(() => project);
    if (view !== undefined) browser.view = view;
    if (ticked !== undefined) browser.setCopies(ticked, true);
    browser.active = active;
    browser.opened = opened;
    onbrowser?.(browser);

    return browser;
  };

  setProjectBrowser(start());
</script>

{@render children()}
