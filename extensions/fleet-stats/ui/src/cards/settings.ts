// The settings of the module's dashboard cards, read out of what the core
// hands a card. The core has already held every value to the schema in
// module.yaml, so this only gives each one its type.
import type { CardSettings } from '@prusa/connect-ui/slots';

import { type Days, daysOf } from '../data/stats.svelte';
import type { Scope } from '../domain/job-stats';
import { searchOf } from '../view/page-query';

/** What every card of this module counts: a period and the printers in it. */
export interface CardView {
  readonly days: Days;
  readonly scope: Scope;
}

const texts = (value: unknown): string[] =>
  Array.isArray(value)
    ? value.filter((item): item is string => typeof item === 'string')
    : [];

export const cardViewOf = (config: CardSettings): CardView => ({
  days: daysOf(config.period),
  scope: {
    printers: texts(config.printers),
    tag: typeof config.tag === 'string' ? config.tag : '',
  },
});

/** How many failures a failures card lists. */
export const failureLimitOf = (config: CardSettings): number =>
  typeof config.limit === 'number' && Number.isInteger(config.limit)
    ? Math.min(20, Math.max(1, config.limit))
    : 5;

/**
 * The page's address for what a card shows. The page narrows to one printer
 * at most, so a card of several printers opens on all of them.
 */
export const pageSearchOf = (view: CardView): string =>
  searchOf({
    days: view.days,
    printer:
      view.scope.printers.length === 1 ? (view.scope.printers[0] ?? '') : '',
    tag: view.scope.tag,
  });
