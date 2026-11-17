// The Extensions listing as rows: every module this server offers or has
// installed, once each, with what the table and the stacked rows both read.
//
// Two lists meet here. The index is what can be installed, from this server's
// own index file and from the marketplace. The resources listing is what is
// installed, whatever it came from. A module in both is one row; a module
// installed by hand that no index row offers is still a row, so nothing that
// runs on this server is missing from the page that removes it.
//
// Plain functions over plain data, so the screen derives them and a test
// computes them without rendering anything.
import type { AnnouncedPage, ResourceInstance } from '@prusa/connect-ui/data';

import {
  type ModuleOffer,
  type OfferState,
  stateOf,
} from '../stores/module-catalogue.svelte';
import { OFFICIAL } from './trust';

/**
 * Where a row comes from: this server's own index, the marketplace, or
 * neither. An installed module no list offers right now is `unlisted`: it was
 * installed by hand, or from a marketplace that is switched off or no longer
 * offers it, and this page does not claim to know which.
 */
export type ExtensionSource = 'local' | 'marketplace' | 'unlisted';

export const SOURCE_KEY = {
  local: 'app.settings.extensions.source.local',
  marketplace: 'app.settings.extensions.source.marketplace',
  unlisted: 'app.settings.extensions.source.unlisted',
} as const satisfies Record<ExtensionSource, string>;

/** The three dimensions a reader narrows the listing by. */
export type ExtensionFacet = 'source' | 'trust' | 'state';

/** What a reader chose in each dimension. Empty chooses everything. */
export type ExtensionSelection = Readonly<
  Record<ExtensionFacet, readonly string[]>
>;

export const NO_EXTENSIONS_SELECTED: ExtensionSelection = {
  source: [],
  trust: [],
  state: [],
};

/** A page a module adds, as a link. */
export interface ExtensionPage {
  readonly title: string;
  readonly href: string;
}

/** One module as the listing draws it. */
export interface ExtensionRow {
  /** Stable for the row's key: the module path, or the namespace when only that is known. */
  readonly key: string;
  readonly module: string;
  readonly namespace?: string | undefined;
  readonly title: string;
  readonly summary: string;
  readonly author: string;
  /** `official`, `verified` or `community`, for a marketplace row. */
  readonly trust?: string | undefined;
  readonly source: ExtensionSource;
  /** `beta` for a marketplace release from its beta channel. */
  readonly channel?: 'stable' | 'beta' | undefined;
  /** The resource kind it is declared under, when the server names it. */
  readonly kind?: string | undefined;
  readonly state: OfferState;
  /** The release on offer, or the installed one for a module no index offers. */
  readonly version: string;
  readonly installedVersion?: string | undefined;
  readonly platforms: readonly string[];
  readonly pages: readonly ExtensionPage[];
  /** Where the marketplace shows this module. */
  readonly pageUrl?: string | undefined;
  /** The index row, which an install takes. Absent for a module no index offers. */
  readonly offer?: ModuleOffer | undefined;
  /** The installed instance of this module, absent while it is not installed. */
  readonly instance?: ResourceInstance | undefined;
}

const firstOf = (...names: (string | undefined)[]): string =>
  names.find((name): name is string => name !== undefined && name !== '') ?? '';

const pagesOf = (
  pages: readonly AnnouncedPage[],
  namespace: string | undefined,
): ExtensionPage[] =>
  namespace === undefined || namespace === ''
    ? []
    : pages
        .filter((page) => page.namespace === namespace && !page.hidden)
        .map((page) => ({ title: page.title, href: page.href }));

const rowOfOffer = (
  offer: ModuleOffer,
  pages: readonly AnnouncedPage[],
  instance: ResourceInstance | undefined,
): ExtensionRow => ({
  key: offer.module,
  module: offer.module,
  namespace: offer.namespace,
  title: firstOf(offer.title, offer.name, offer.module),
  summary: offer.description ?? '',
  author: offer.author ?? '',
  trust: offer.trust,
  source: offer.origin === 'marketplace' ? 'marketplace' : 'local',
  channel: offer.channel,
  kind: offer.kind,
  state: stateOf(offer),
  version: offer.version,
  installedVersion: offer.installed_version,
  platforms: offer.platforms ?? [],
  pages: pagesOf(pages, offer.namespace),
  pageUrl: offer.page_url,
  offer,
  instance,
});

const rowOfInstalled = (
  item: ResourceInstance,
  pages: readonly AnnouncedPage[],
): ExtensionRow => {
  const namespace = item.namespace ?? '';

  return {
    key: namespace,
    module: namespace,
    namespace,
    title: firstOf(item.module?.title, item.name, namespace),
    summary: item.module?.summary ?? '',
    author: item.module?.author ?? '',
    source: 'unlisted',
    kind: item.kind,
    state: 'installed',
    version: item.module?.version ?? '',
    installedVersion: item.module?.version,
    platforms: [],
    pages: pagesOf(pages, namespace),
    instance: item,
  };
};

/**
 * Every row of the listing: the index rows in the order the server answered
 * them, then each installed module no index row offers.
 */
export const extensionRowsOf = (
  offers: readonly ModuleOffer[],
  installed: readonly ResourceInstance[],
  pages: readonly AnnouncedPage[],
): ExtensionRow[] => {
  const offered = new Set(
    offers
      .map((offer) => offer.namespace)
      .filter((namespace) => namespace !== undefined),
  );

  const byRef = new Map(
    installed.map((item) => [`${item.kind}:${item.name}`, item]),
  );

  return [
    ...offers.map((offer) =>
      rowOfOffer(
        offer,
        pages,
        offer.instance === undefined ? undefined : byRef.get(offer.instance),
      ),
    ),
    ...installed
      .filter(
        (item) =>
          item.module !== undefined &&
          (item.namespace ?? '') !== '' &&
          !offered.has(item.namespace ?? ''),
      )
      .map((item) => rowOfInstalled(item, pages)),
  ];
};

/**
 * Whether the listing leaves a row out while nobody chose to list community
 * extensions: every marketplace extension that is not official, trusted and
 * unverified alike. Only a row that is not installed is left out: what runs
 * on this server stays on the page that updates and removes it.
 */
export const hidesCommunity = (row: ExtensionRow, shown: boolean): boolean =>
  !shown &&
  row.source === 'marketplace' &&
  row.trust !== OFFICIAL &&
  row.state === 'available';

/** What a typed search matches, folded so case and spacing do not matter. */
export const searchTerm = (typed: string): string =>
  typed.trim().toLocaleLowerCase();

const VALUE: Record<ExtensionFacet, (row: ExtensionRow) => string> = {
  source: (row) => row.source,
  // A row no marketplace vouches for has no trust, and no trust value brings it.
  trust: (row) => row.trust ?? '',
  state: (row) => row.state,
};

const matchesSearch = (row: ExtensionRow, term: string): boolean =>
  term === '' ||
  [row.title, row.module, row.summary, row.author].some((field) =>
    field.toLocaleLowerCase().includes(term),
  );

// A row passes every dimension but `skip`, which is how a dimension counts
// the rows each of its own values would bring.
const matchesExcept = (
  row: ExtensionRow,
  selection: ExtensionSelection,
  term: string,
  skip: ExtensionFacet | null,
): boolean =>
  matchesSearch(row, term) &&
  (Object.keys(VALUE) as ExtensionFacet[]).every(
    (facet) =>
      facet === skip ||
      selection[facet].length === 0 ||
      selection[facet].includes(VALUE[facet](row)),
  );

/** The rows the search and every chosen value let through. */
export const filterExtensions = (
  rows: readonly ExtensionRow[],
  selection: ExtensionSelection,
  term: string,
): ExtensionRow[] =>
  rows.filter((row) => matchesExcept(row, selection, term, null));

/**
 * How many rows each value of `facet` would bring, with the other dimensions
 * and the search applied. Every value in `over` is counted, so a value that
 * matches nothing reads zero instead of vanishing.
 */
export const extensionFacetCounts = (
  rows: readonly ExtensionRow[],
  selection: ExtensionSelection,
  term: string,
  facet: ExtensionFacet,
  over: readonly string[],
): Map<string, number> => {
  const counts = new Map<string, number>(over.map((value) => [value, 0]));

  for (const row of rows) {
    if (!matchesExcept(row, selection, term, facet)) continue;
    const value = VALUE[facet](row);
    counts.set(value, (counts.get(value) ?? 0) + 1);
  }

  return counts;
};

/** The values each dimension offers, in the order the reader reads them. */
export const SOURCES: readonly ExtensionSource[] = [
  'local',
  'marketplace',
  'unlisted',
];
/** The marketplace's trust levels, the one it vouches for most first. */
export const TRUSTS: readonly string[] = ['official', 'verified', 'community'];
export const STATES: readonly OfferState[] = [
  'update',
  'installed',
  'available',
];

// An update is what a person came here to act on, so it leads; then what is
// installed, then what could be.
const RANK: Record<OfferState, number> = {
  update: 0,
  installed: 1,
  available: 2,
};

/** Updates first, then installed modules, then the rest, each by title. */
export const compareExtensions = (a: ExtensionRow, b: ExtensionRow): number =>
  RANK[a.state] - RANK[b.state] ||
  a.title.localeCompare(b.title, undefined, { sensitivity: 'base' });
