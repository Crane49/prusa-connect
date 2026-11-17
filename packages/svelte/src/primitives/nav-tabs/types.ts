import type { ClassValue } from 'svelte/elements';

export interface NavTab {
  id: string;
  href: string;
  label: string;
}

export interface NavTabsProps {
  /** The accessible name of the navigation landmark. */
  label: string;
  tabs: readonly NavTab[];
  /** The `id` of the tab whose route is open. */
  active: string;
  class?: ClassValue | undefined;
}
