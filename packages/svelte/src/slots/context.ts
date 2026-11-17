// Where an outlet finds the registry and the loader.
//
// Context and not a module singleton, for the reason `svelte-5.md` gives: a
// library component may render in a consumer that renders on a server, where
// one module-level instance would be shared across requests, and two apps on
// one page would share one set of registrations.
import { getContext, setContext } from 'svelte';

import type { ModuleLoader } from './loader.svelte';
import type { SlotRegistry } from './registry.svelte';

/** What every slot component needs: what is registered, and what loads it. */
export interface SlotContext {
  registry: SlotRegistry;
  /** Absent in a test or an app that loads no module. */
  loader?: ModuleLoader | undefined;
}

const SLOTS = Symbol('prusa.slots');

/** Called once, high in the app's tree. */
export const setSlotContext = (value: SlotContext): SlotContext =>
  setContext(SLOTS, value);

/** Undefined outside a component under `setSlotContext`. */
export const getSlotContext = (): SlotContext | undefined =>
  getContext<SlotContext | undefined>(SLOTS);
