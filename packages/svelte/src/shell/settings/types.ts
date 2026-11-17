import type { Snippet } from 'svelte';

export interface SettingsScreenProps {
  /** The open pane. Which one that is belongs to the host, not to the page. */
  children?: Snippet | undefined;
}
