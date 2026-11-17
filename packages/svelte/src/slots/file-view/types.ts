import type { Snippet } from 'svelte';

import type { ModuleLoader } from '../loader.svelte';
import type { SlotRegistry } from '../registry.svelte';

/** The part of a file's page a module may draw (ADR-20261027-wvj rule 16). */
export interface FileViewProps {
  /** The file, as `<storage>:<path>`. It is what a module's view is handed. */
  fileRef: string;
  /** The core's own part, drawn when no module draws one for this file's type. */
  fallback: Snippet;
  /** Defaults to the registry in context. */
  registry?: SlotRegistry | undefined;
  /** The loader the view wakes on mount. Defaults to the one in context. */
  loader?: ModuleLoader | undefined;
}
