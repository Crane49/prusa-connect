import type { Snippet } from 'svelte';
import type { ClassValue } from 'svelte/elements';

export interface FacetRowProps {
  /**
   * Rich content is allowed. A device row carries its free space and its
   * read-only badge beside its name, and a plain string would make a facet a
   * downgrade from the select it replaced.
   */
  label: Snippet;
  /** Absent renders no number at all: a plain choice rather than a facet. */
  count?: number | undefined;
  checked: boolean;
  onToggle: () => void;
  /**
   * Unpickable for a reason of the caller's own: a printer that cannot accept
   * a file, a device that is not mounted. Distinct from the zero-count rule,
   * which this component decides for itself.
   */
  disabled?: boolean | undefined;
  /** Why it is unpickable, or anything else the row should say after its label. */
  hint?: string | undefined;
  class?: ClassValue | undefined;
}
