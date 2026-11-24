// The one store and the bound `t`, held for the module's components. The host
// arrives before any component is mounted, so there is no component for
// `setContext` to hang them on.
import type { ModuleHost } from '@prusa/connect-ui/slots';

import { Buddy3d } from './buddy3d.svelte';

interface Held {
  readonly store: Buddy3d;
  readonly t: ModuleHost['t'];
}

let current: Held | undefined;

export const hold = (host: ModuleHost): void => {
  current = { store: new Buddy3d(host), t: host.t };
};

export const held = (): Held => {
  if (current === undefined) throw new Error('buddy3d-camera: the module was not set up');

  return current;
};
