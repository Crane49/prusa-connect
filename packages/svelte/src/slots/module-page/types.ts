import type { Snippet } from 'svelte';

import type { ModuleLoader } from '../loader.svelte';
import type { SlotRegistry } from '../registry.svelte';

export interface ModulePageProps {
  /** The namespace the core installed the module under. */
  namespace: string;
  /** The page's own path, one label with no slash (ADR-20260911-4p6 rule 14). */
  path: string;
  registry?: SlotRegistry | undefined;
  loader?: ModuleLoader | undefined;
  /** Drawn while the loader is still reading, and when no module claims the route. */
  fallback?: Snippet | undefined;
  class?: string | undefined;
}
