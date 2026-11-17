import type { Snippet } from 'svelte';
import type { ClassValue } from 'svelte/elements';

import type { FaultText } from '../../i18n';

export interface FaultAlertProps {
  /** Nothing is drawn when the caller holds no refusal. */
  fault?: FaultText | undefined;
  /** Buttons drawn last inside the alert, such as a retry or a dismiss. */
  actions?: Snippet;
  class?: ClassValue | undefined;
}
