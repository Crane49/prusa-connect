// The settings of the module's dashboard cards, read out of what the core
// hands a card. The core has already held every value to the schema in
// module.yaml, so this only gives each one its type.
//
// A card of chosen parts keeps their ids as one line of text, such as
// "12,40,7", because the schema's kinds hold no list of the module's own
// ids. The line is the order the person chose them in.
import type { CardSettings } from '@prusa/connect-ui/slots';

import { searchOf, TOP } from '../navigation-query';

/** What a folder card shows: the parts of one folder, newest or by name. */
export interface FolderCardSettings {
  /** The folder, and 0 for the top of the catalogue. */
  readonly folder: number;
  /** True to take in the parts of every folder under it as well. */
  readonly deep: boolean;
  /** Only parts with this tag, or every part when empty. */
  readonly tag: string;
  readonly order: 'name' | 'changed';
  /** How many parts the card shows at most. */
  readonly limit: number;
}

const whole = (value: unknown, fallback: number): number =>
  typeof value === 'number' && Number.isInteger(value) ? value : fallback;

export const folderCardSettings = (
  config: CardSettings,
): FolderCardSettings => ({
  folder: whole(config.folder, 0),
  deep: config.deep === true,
  tag: typeof config.tag === 'string' ? config.tag : '',
  order: config.order === 'name' ? 'name' : 'changed',
  limit: whole(config.limit, 8),
});

/** The most parts a card of chosen parts holds. */
export const MAX_CHOSEN = 12;

/** The ids a line of text names, each once, in its order. */
export const idsOf = (written: unknown): number[] => {
  if (typeof written !== 'string') return [];
  const out: number[] = [];
  for (const piece of written.split(',')) {
    const trimmed = piece.trim();
    if (!/^\d{1,10}$/.test(trimmed)) continue;
    const id = Number(trimmed);
    if (id > 0 && !out.includes(id)) out.push(id);
    if (out.length === MAX_CHOSEN) break;
  }

  return out;
};

/** The ids as the card keeps them. */
export const writtenIds = (ids: readonly number[]): string => ids.join(',');

/** The address of one part on the module's page. */
export const partHref = (page: string, part: number): string =>
  `${page}${searchOf({ ...TOP, part })}`;

/** The address of one folder on the module's page. */
export const folderHref = (page: string, folder: number): string =>
  `${page}${searchOf({ ...TOP, folder })}`;
