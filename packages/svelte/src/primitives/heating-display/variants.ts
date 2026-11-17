import type { HeaterState } from '../../domain';

// Static strings chosen by key (svelte-5.md). Tailwind reads this file, so a
// utility written here is emitted; one assembled at run time is not.

export const base = 'inline-flex items-center gap-0.5';

/**
 * The tint each state carries.
 *
 * `--warning-text` for heating and `--info-text` for cooling, never
 * `--destructive`, which this tree keeps for a fault. Both measure AA
 * (>=4.5:1) as text against `--card`, `--surface` and `--background` in both
 * themes; the tightest is `--info-text` on light `--surface`, 4.55:1.
 *
 * `at-temperature` keeps the caller's own foreground, so it names no colour.
 * `off` and `not-reported` borrow `--muted-foreground`, the quiet tone the
 * rest of a row's secondary text already uses.
 */
export const tints: Record<HeaterState, string> = {
  heating: 'animate-heating-pulse text-warning-text',
  'at-temperature': '',
  cooling: 'text-info-text',
  off: 'text-muted-foreground',
  'not-reported': 'text-muted-foreground',
};

/** The box the trend arrow draws in, at the size of the text beside it. */
export const arrow = 'size-3.5 shrink-0';
