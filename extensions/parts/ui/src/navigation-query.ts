// Where in the catalogue a person is, as the query of the address says it.
// Plain functions over a string, so a test reads them without a browser and
// the store that keeps the place holds no parsing of its own.

/** One place in the catalogue. */
export interface Spot {
  /** The folder on screen, and 0 for the top of the tree. */
  readonly folder: number;
  /** The part open on screen, and undefined for none. */
  readonly part: number | undefined;
  /** What the person searched for. Empty is no search. */
  readonly q: string;
  /** The tag the listing is narrowed to. Empty is no tag. */
  readonly tag: string;
}

/** The top of the tree, with nothing open. */
export const TOP: Spot = { folder: 0, part: undefined, q: '', tag: '' };

/** The longest search the module answers, as its fragment states. */
export const MAX_QUERY = 120;

/** A whole number the address carries, or undefined for anything else. */
const counted = (written: string | null): number | undefined => {
  if (written === null || !/^\d{1,10}$/.test(written)) return undefined;

  return Number(written);
};

/** Reads one place out of a query string. Anything it cannot read is left out. */
export const spotOf = (search: string): Spot => {
  const query = new URLSearchParams(search);

  return {
    folder: counted(query.get('folder')) ?? 0,
    part: counted(query.get('part')),
    q: (query.get('q') ?? '').slice(0, MAX_QUERY),
    tag: (query.get('tag') ?? '').slice(0, MAX_QUERY),
  };
};

/** Writes one place as a query string, with every empty field left out. */
export const searchOf = (spot: Spot): string => {
  const query = new URLSearchParams();
  if (spot.folder !== 0) query.set('folder', String(spot.folder));
  if (spot.part !== undefined) query.set('part', String(spot.part));
  if (spot.q !== '') query.set('q', spot.q);
  if (spot.tag !== '') query.set('tag', spot.tag);
  const written = query.toString();

  return written === '' ? '' : `?${written}`;
};
