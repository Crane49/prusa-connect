import type { Snippet } from 'svelte';
import type { ClassValue } from 'svelte/elements';

/**
 * The six tones the theme carries as `--state-*`. They are the same six words
 * the domain layer gives a printer state, so a card and a badge agree.
 */
export type StatusTone =
  'active' | 'done' | 'idle' | 'attention' | 'fault' | 'quiet';

/** How large the reading is drawn. */
export type StatusFigure = 'display' | 'fluid';

export type StatusHeadingLevel = 'h2' | 'h3' | 'h4';

/** Which way the reading moved since the one before it. */
export type StatusTrend = 'up' | 'down' | 'flat';

export interface StatusCardProps {
  /** What the card is about: "Fractal is on", a printer's name. */
  title: string;
  tone?: StatusTone;
  /** The state in a word, for a screen reader, when the dot is the only sign. */
  toneLabel?: string | undefined;
  /** The one reading the card exists for, with its unit. */
  value?: string | undefined;
  /** One line under the reading. */
  caption?: string | undefined;
  /** Which way the reading moved. It needs `trendLabel` to mean anything. */
  trend?: StatusTrend | undefined;
  /** What the trend is, in words: "3 more than yesterday". */
  trendLabel?: string | undefined;
  /** When the reading was taken, already formatted. */
  timestamp?: string | undefined;
  /** A glyph or a silhouette at the start of the header. */
  icon?: Snippet;
  /** A `Badge` at the end of the header, for the state in one word. */
  badge?: Snippet;
  /** Anything under the reading: a key and value list, a bar. */
  children?: Snippet;
  /** The one thing to press. It sits at the foot of the card. */
  action?: Snippet;
  /**
   * Marks the tone with a coloured start edge instead of the dot, and tightens
   * the card, for a row of counts inside a tile. The title is then drawn as a
   * small muted label, so the figure carries the card.
   */
  edge?: boolean;
  /**
   * The reading's type size. `display` is the fixed display size. `fluid`
   * scales with the card, so the card must sit in a container with a size.
   * A `quiet` tone draws the reading muted either way.
   */
  figure?: StatusFigure;
  /** The tag of the title. It follows the heading above the card. */
  headingLevel?: StatusHeadingLevel;
  class?: ClassValue | undefined;
}
