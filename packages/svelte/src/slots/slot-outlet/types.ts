import type { SectionOutletTarget } from '../generated';
import type { ModuleLoader } from '../loader.svelte';
import type { SlotRegistry } from '../registry.svelte';
import type { WithSlotDevice } from '../device-selector';

/** A section slot, typed to the screen it sits on and what that screen hands it. */
export type SlotOutletProps = WithSlotDevice<SectionOutletTarget> & {
  /**
   * The registry to read. It defaults to the one in context, so an app that
   * called `setSlotContext` passes neither this nor `loader`.
   */
  registry?: SlotRegistry | undefined;
  /** The loader the outlet wakes on mount. Defaults to the one in context. */
  loader?: ModuleLoader | undefined;
  class?: string | undefined;
  /** For a tab: draw only the registration `<namespace>.<id>`. */
  entry?: string | undefined;
  /**
   * Draw only the registrations of the module under this namespace. A
   * camera's live view is drawn by the module that offers the camera's type
   * and by no other (ADR-20261106-km2).
   */
  namespace?: string | undefined;
};
