// What the page reads, held for it from the module's setup.
//
// A module-level holder and not Svelte context: the setup function runs
// before any component of this module is mounted, so there is no component
// for `setContext` to hang on. It is set once, in `index.ts`, before the page
// can be drawn, and let go when the person who admitted the module leaves.
import type { DeviceStates } from '@prusa/connect-ui/data';
import type { ModuleHost } from '@prusa/connect-ui/slots';

import type { Catalogue } from './catalogue.svelte';
import type { FilesService } from './files';
import type { FolderDrop } from './folder-drop.svelte';
import type { Navigation } from './navigation.svelte';
import type { TreeState } from './tree-state.svelte';

/** Everything the page reads, built from the host and from nothing else. */
export interface Session {
  readonly host: ModuleHost;
  readonly catalogue: Catalogue;
  readonly navigation: Navigation;
  /** The core's storage and print routes: picture addresses, uploads, the print. */
  readonly files: FilesService;
  /** The moves between states each printer kind declared, for the picker. */
  readonly states: DeviceStates;
  /** Which folders of the tree the person closed. */
  readonly tree: TreeState;
  /** Files dropped onto a folder, on their way to becoming parts. */
  readonly drop: FolderDrop;
}

let held: Session | undefined;

/**
 * Called once by the module's setup function. A setup that was still running
 * when the person left resumes with a signal that already fired, and what it
 * built is not held.
 */
export const holdSession = (session: Session): void => {
  const signal = session.host.signal;
  if (signal.aborted) return;
  held = session;
  signal.addEventListener(
    'abort',
    () => {
      if (held === session) held = undefined;
    },
    { once: true },
  );
};

/** What the page reads. It throws rather than drawing an empty page. */
export const heldSession = (): Session => {
  if (!held) {
    throw new Error('the parts module drew its page before its setup ran');
  }

  return held;
};
