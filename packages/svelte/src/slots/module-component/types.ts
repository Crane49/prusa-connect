import type { Component, Snippet } from 'svelte';

import type { ComponentUnavailable } from '../host';

/** Another module's component, drawn by a module's bundle (ADR-20261027-wvj rule 24). */
export interface ModuleComponentProps<Props extends Record<string, unknown>> {
  /** What answers the component, typically `() => host.components.load(module, name)`. */
  load: () => Promise<Component<Props>>;
  /** The props and the callbacks the component is drawn with. */
  props: Props;
  /**
   * Drawn in place of the component when it is not available, handed why.
   * Without it a muted line says the part is not available.
   */
  fallback?: Snippet<[ComponentUnavailable]> | undefined;
}
