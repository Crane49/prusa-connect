// The Slicers tab as one listing: one row per PrusaSlicer version, made from
// the releases the module offers, the slicers it has installed and the
// installs that run. Plain functions, so a test reads them without a browser.
import type { InstalledSlicer, InstallProgress, Offer, Slicers } from './api';
import { channelOf, type ChannelWord } from './views/channel';

/** Where a version stands on this server. */
export type ReleaseState =
  'installed' | 'installing' | 'failed' | 'available' | 'unavailable';

export const RELEASE_STATES: readonly ReleaseState[] = [
  'installed',
  'installing',
  'failed',
  'available',
  'unavailable',
];

/** Whether a download is checked against a published SHA-256. */
export type DigestState = 'verified' | 'unverified' | 'none';

export interface ReleaseRow {
  version: string;
  channel: ChannelWord;
  /** The major and minor version, such as 2.9. */
  line: string;
  released: string | undefined;
  offer: Offer | undefined;
  /** The installed copies: a download, a local binary, or both. */
  installed: InstalledSlicer[];
  progress: InstallProgress | undefined;
  state: ReleaseState;
  isDefault: boolean;
  /** An administrator chose this version as the default. */
  isPinned: boolean;
  /** Bytes on disk when installed, else the size of the download. */
  size: number | undefined;
  digest: DigestState;
  /** A copy this module downloaded, which an administrator may remove. */
  removable: boolean;
}

const prereleaseRank: Record<string, number> = { alpha: 0, beta: 1, rc: 2 };

interface Parsed {
  parts: [number, number, number];
  /** Undefined for a final release, which comes after every prerelease. */
  label: [number, number] | undefined;
}

const parse = (version: string): Parsed => {
  const match = /^(\d+)\.(\d+)\.(\d+)(?:-([a-z]+)(\d*))?/u.exec(version);
  if (!match) return { parts: [0, 0, 0], label: [-1, 0] };

  const [, major, minor, patch, word, number] = match;
  return {
    parts: [Number(major), Number(minor), Number(patch)],
    label:
      word === undefined
        ? undefined
        : [prereleaseRank[word] ?? -1, Number(number)],
  };
};

/** Orders two versions the way PrusaSlicer numbers them, oldest first. */
export const compareVersions = (a: string, b: string): number => {
  const left = parse(a);
  const right = parse(b);

  const [leftMajor, leftMinor, leftPatch] = left.parts;
  const [rightMajor, rightMinor, rightPatch] = right.parts;
  const step =
    leftMajor - rightMajor || leftMinor - rightMinor || leftPatch - rightPatch;
  if (step !== 0) return step;

  if (left.label === undefined) return right.label === undefined ? 0 : 1;
  if (right.label === undefined) return -1;

  return left.label[0] - right.label[0] || left.label[1] - right.label[1];
};

/** The major and minor version, such as 2.9 for 2.9.6-rc1. */
export const lineOf = (version: string): string =>
  parse(version).parts.slice(0, 2).join('.');

const stateOf = (
  offer: Offer | undefined,
  installed: readonly InstalledSlicer[],
  progress: InstallProgress | undefined,
): ReleaseState => {
  if (progress?.phase === 'failed') return 'failed';
  if (progress) return 'installing';
  if (installed.length > 0) return 'installed';
  if (offer && !offer.compatible) return 'unavailable';

  return 'available';
};

const digestOf = (
  offer: Offer | undefined,
  installed: readonly InstalledSlicer[],
): DigestState => {
  const downloaded = installed.find((held) => held.source !== 'local');
  if (downloaded) {
    // A copy installed before the module recorded a check is still checked
    // when its digest is the one the source publishes now.
    const matches =
      downloaded.sha256 !== undefined && downloaded.sha256 === offer?.sha256;

    return downloaded.verified || matches ? 'verified' : 'unverified';
  }
  if (installed.length > 0) return 'none';
  if (!offer?.compatible) return 'none';

  return offer.verified ? 'verified' : 'unverified';
};

/** Every version the module knows about, one row each, newest first. */
export const releaseRowsOf = (slicers: Slicers): ReleaseRow[] => {
  const versions = new Set<string>([
    ...slicers.available.map((offer) => offer.version),
    ...slicers.installed.map((held) => held.version),
    ...slicers.installing.map((held) => held.version),
  ]);

  return [...versions]
    .map((version): ReleaseRow => {
      const offer = slicers.available.find((row) => row.version === version);
      const installed = slicers.installed.filter(
        (row) => row.version === version,
      );
      const progress = slicers.installing.find(
        (row) => row.version === version,
      );
      const channel = offer?.channel ?? installed[0]?.channel ?? 'prerelease';
      const state = stateOf(offer, installed, progress);
      const onDisk = installed.reduce(
        (sum, held) => sum + (held.bytes ?? 0),
        0,
      );

      return {
        version,
        channel: channelOf(version, channel),
        line: lineOf(version),
        released: offer?.released ?? installed[0]?.released,
        offer,
        installed,
        progress,
        state,
        isDefault: installed.length > 0 && slicers.default === version,
        isPinned: slicers.pinned === version,
        size: onDisk > 0 ? onDisk : offer?.size,
        digest: digestOf(offer, installed),
        removable: installed.some((held) => held.source !== 'local'),
      };
    })
    .sort((a, b) => compareVersions(b.version, a.version));
};

/** The column a reader orders the releases by. */
export type ReleaseSortColumn = 'version' | 'released';

export interface ReleaseSort {
  column: ReleaseSortColumn;
  ascending: boolean;
}

export const DEFAULT_RELEASE_SORT: ReleaseSort = {
  column: 'version',
  ascending: false,
};

export const compareReleases = (
  a: ReleaseRow,
  b: ReleaseRow,
  sort: ReleaseSort,
): number => {
  if (
    sort.column === 'released' &&
    (a.released === undefined) !== (b.released === undefined)
  ) {
    // A version with no known date goes last, whichever way the list runs.
    return a.released === undefined ? 1 : -1;
  }

  const step =
    sort.column === 'released'
      ? (a.released ?? '').localeCompare(b.released ?? '') ||
        compareVersions(a.version, b.version)
      : compareVersions(a.version, b.version);

  return sort.ascending ? step : -step;
};

/** What the reader narrowed the releases to. Empty means every value. */
export interface ReleaseFilter {
  q: string;
  channel: readonly string[];
  state: readonly string[];
  line: readonly string[];
}

export const NO_RELEASE_FILTER: ReleaseFilter = {
  q: '',
  channel: [],
  state: [],
  line: [],
};

export type ReleaseFacet = 'channel' | 'state' | 'line';

const valueOf = (row: ReleaseRow, facet: ReleaseFacet): string => {
  if (facet === 'channel') return row.channel;
  if (facet === 'state') return row.state;

  return row.line;
};

const matches = (
  row: ReleaseRow,
  filter: ReleaseFilter,
  skip?: ReleaseFacet,
): boolean => {
  for (const facet of ['channel', 'state', 'line'] as const) {
    if (facet === skip) continue;
    const chosen = filter[facet];
    if (chosen.length > 0 && !chosen.includes(valueOf(row, facet))) {
      return false;
    }
  }

  const term = filter.q.trim().toLowerCase();
  if (term === '') return true;

  return [row.version, row.offer?.asset, row.offer?.reason]
    .filter((text): text is string => text !== undefined)
    .some((text) => text.toLowerCase().includes(term));
};

export const filterReleases = (
  rows: readonly ReleaseRow[],
  filter: ReleaseFilter,
): ReleaseRow[] => rows.filter((row) => matches(row, filter));

/** The values a facet offers, in the order a reader expects them. */
export const releaseFacetValues = (
  rows: readonly ReleaseRow[],
  facet: ReleaseFacet,
): string[] => {
  if (facet === 'state') {
    return RELEASE_STATES.filter((state) =>
      rows.some((row) => row.state === state),
    );
  }
  if (facet === 'channel') {
    return (['stable', 'rc', 'beta', 'alpha', 'prerelease'] as const).filter(
      (channel) => rows.some((row) => row.channel === channel),
    );
  }

  return [...new Set(rows.map((row) => row.line))].sort(
    (a, b) => -compareVersions(`${a}.0`, `${b}.0`),
  );
};

/** How many rows each value would leave, with the other filters applied. */
export const releaseFacetCounts = (
  rows: readonly ReleaseRow[],
  filter: ReleaseFilter,
  facet: ReleaseFacet,
): Map<string, number> => {
  const counts = new Map<string, number>();
  for (const row of rows) {
    if (!matches(row, filter, facet)) continue;
    const value = valueOf(row, facet);
    counts.set(value, (counts.get(value) ?? 0) + 1);
  }

  return counts;
};
