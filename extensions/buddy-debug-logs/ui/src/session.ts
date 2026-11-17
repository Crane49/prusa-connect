// What the page and the printer tab read, held for them from the module's
// setup.
//
// A module-level holder and not Svelte context: the setup function runs
// before any component of this module is mounted, so there is no component
// for `setContext` to hang on. It is set once, in `index.ts`, and let go when
// the person who admitted the module leaves.
import type { ModuleHost } from '@prusa/connect-ui/slots';

import type { LogsApi } from './api';
import type { StatusStore } from './status.svelte';

/** Everything the page reads, built from the host and from nothing else. */
export interface Session {
  readonly host: ModuleHost;
  readonly api: LogsApi;
  /** The ports, the counts and the sources, shared by every view. */
  readonly status: StatusStore;
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
    throw new Error(
      'the Prusa printer logs module drew a view before its setup ran',
    );
  }

  return held;
};
