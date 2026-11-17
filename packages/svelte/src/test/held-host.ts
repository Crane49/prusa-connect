// The host the importing test module was set up with, for its page to read.
// A real bundle keeps it the same way, in a module of its own.
import type { ModuleHost } from '../slots/host';

let held: ModuleHost | undefined;

export const holdHost = (host: ModuleHost): void => {
  held = host;
};

export const heldHost = (): ModuleHost => {
  if (held === undefined) throw new Error('the module was not set up');

  return held;
};
