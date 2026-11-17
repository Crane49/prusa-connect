// What the page reads, held for it from the module's setup.
//
// A module-level holder and not Svelte context: the setup function runs
// before any component of this module is mounted, so there is no component
// for `setContext` to hang on. It is set once, in `index.ts`, and let go when
// the person who admitted the module leaves.
import type { ModuleHost } from '@prusa/connect-ui/slots';

import type { SlicerApi } from './api';

/** Everything the page reads, built from the host and from nothing else. */
export interface Session {
  readonly host: ModuleHost;
  readonly api: SlicerApi;
}

let held: Session | undefined;

/** Called once by the module's setup function. */
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
      'the server-slicing module drew a view before its setup ran',
    );
  }

  return held;
};
