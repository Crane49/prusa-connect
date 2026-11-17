import type { ReadonlyFleet } from '../../domain';
import type { ListingStore } from '../../primitives';

/** A column of the listing. The printers are the one a reader puts away. */
export type TagFieldId = 'tag' | 'printers' | 'actions';

/** A printer carrying a tag, with enough to name it and to reach it. */
export interface TagPrinter {
  id: string;
  name: string;
  detailHref: string;
}

/**
 * One tag and the printers carrying it. The core keeps tags on the device
 * record and serves no list of its own, so every row has a printer behind it.
 */
export interface TagRow {
  /** The tag's key, which two spellings of one tag share. */
  id: string;
  /** The first spelling the fleet carries, which is the one shown. */
  tag: string;
  printers: readonly TagPrinter[];
}

/**
 * What the screen reads of a fleet. The `Fleet` store satisfies it, so this
 * product hands its store straight in.
 */
export type TagFleetSource = Pick<
  ReadonlyFleet,
  'devices' | 'error' | 'loaded' | 'loading'
>;

export interface TagsScreenProps {
  fleet: TagFleetSource;
  /** The address of the printers list filtered to one tag. Without it, no row links there. */
  printersHref?: ((tag: string) => string) | undefined;
  store?: ListingStore;
}
