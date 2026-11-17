import type { LucideIcon } from '@lucide/svelte';
import type { Snippet } from 'svelte';
import type { ClassValue } from 'svelte/elements';

export type BannerTone = 'warning';

export interface BannerProps {
  tone?: BannerTone | undefined;
  /** Drawn before the message. */
  icon?: LucideIcon | undefined;
  children: Snippet;
  /** What a person can do about it, drawn after the message. */
  actions?: Snippet;
  class?: ClassValue | undefined;
}
