// What the theme allows a route to carry, one line per count.
//
// A ceiling is read from the tokens, not chosen here: three radii because the
// theme declares three roles, five sizes because it declares five. The test
// beside this file checks that, so a token added in `packages/svelte` with no
// line here fails rather than widening the gate quietly.
import type { Metric } from './measure.ts';

export interface Ceiling {
  /** The most a route may carry once the craft tasks have landed. */
  readonly allowed: number;
  /** Why that is the number, in one sentence. */
  readonly because: string;
}

export const CEILINGS: Readonly<Record<Metric, Ceiling>> = {
  radii: {
    allowed: 3,
    because:
      'the theme declares one radius per role, and there are three roles: xs, sm and md',
  },
  sizes: {
    allowed: 5,
    because:
      'the type scale is five sizes, xs to 2xl, and nothing on a page is set in anything else',
  },
  greys: {
    allowed: 3,
    because:
      'text is the strong foreground, the quiet line under it, or the white a coloured fill carries',
  },
  borders: {
    allowed: 1,
    because:
      'a surface is told from the one under it by tone, so the one border left is where two tones are equal',
  },
  primaryButtons: {
    allowed: 1,
    because:
      'one accent means one thing to press per route, and a printing card’s Pause is the named exception below',
  },
  icons: {
    allowed: 12,
    because:
      'an icon appears only where it carries meaning a word does not, and a full page holds about a dozen of those',
  },
  namelessIcons: {
    allowed: 0,
    because:
      'a glyph is decorative and says so with aria-hidden, or it is read out: there is no third case',
  },
  mixedPunctuation: {
    allowed: 0,
    because:
      'a sentence carries a period and a label carries none, so one region never punctuates two ways',
  },
  looseFigures: {
    allowed: 0,
    because: 'a figure in a column lines up only in tabular figures',
  },
};

/**
 * Where a route is allowed more than the ceiling, and why. The owner ruled that
 * a printing card keeps Pause as its primary button, so a screen showing a
 * printer that is printing carries the page's own action and that one.
 *
 * Neither route is drawn today: merge 41a1efb7 removed both screens. The
 * ruling is kept here so the allowance comes back with them.
 */
export const EXCEPTIONS: Readonly<
  Record<string, Partial<Record<Metric, number>>>
> = {
  fleet: { primaryButtons: 2 },
  device: { primaryButtons: 2 },
};

/** The ceiling for one route and one count, exceptions counted in. */
export const ceilingFor = (route: string, metric: Metric): number =>
  EXCEPTIONS[route]?.[metric] ?? CEILINGS[metric].allowed;
