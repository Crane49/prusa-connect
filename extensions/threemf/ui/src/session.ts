// The store the views read, held from the module's setup.
//
// A module-level holder and not Svelte context: the setup function runs
// before any component of this module is mounted, and the exported project
// view is mounted inside another module's page, under that module's
// context. It is set once, in `index.ts`, and let go when the person who
// admitted the module leaves.
import type { Projects } from './projects.svelte';

let held: Projects | undefined;

/** Called once by the setup. A setup whose signal already fired holds nothing. */
export const holdProjects = (projects: Projects, signal: AbortSignal): void => {
  if (signal.aborted) return;
  held = projects;
  signal.addEventListener(
    'abort',
    () => {
      if (held === projects) held = undefined;
    },
    { once: true },
  );
};

/** The store the views read. It throws rather than drawing an empty view. */
export const heldProjects = (): Projects => {
  if (!held) {
    throw new Error('the 3MF module drew a view before its setup ran');
  }

  return held;
};
