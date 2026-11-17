// What an install or an update says about the trust of what it adds.
//
// The marketplace marks every extension Official, Trusted or Unverified.
// An official one is Prusa Research's own and needs no word. Every other one
// is a community extension, from an author outside Prusa Research. A trusted
// one had its code reviewed by Prusa Research, and the person is told that a
// review is no guarantee. An unverified one passed only the marketplace's
// automatic checks, so the person ticks that they accept the risk before the
// install can start. The extensions a plan adds are counted with the one the
// person picked, so an official extension cannot bring in an unverified one
// unseen.

/** The marketplace's mark for an extension of Prusa Research. */
export const OFFICIAL = 'official';
/** The marketplace's mark for an extension whose code Prusa Research reviewed. */
export const TRUSTED = 'verified';
/** The marketplace's mark for an extension only its automatic checks read. */
export const UNVERIFIED = 'community';

/** One extension an install adds, as the dialog names it. */
export interface TrustedRelease {
  readonly title: string;
  readonly author?: string | undefined;
  readonly trust?: string | undefined;
}

/** The extensions of one install that need a word about their trust. */
export interface TrustNotice {
  /** The unverified extensions, by the name a person reads. */
  readonly unverified: readonly string[];
  /** Who publishes the unverified extensions, where the marketplace names somebody. */
  readonly authors: readonly string[];
  /** The trusted extensions, by the name a person reads. */
  readonly trusted: readonly string[];
}

const unique = (values: readonly string[]): string[] => [
  ...new Set(values.filter((value) => value !== '')),
];

/** What the dialog says about the trust of the extensions an install adds. */
export const trustNoticeOf = (
  releases: readonly TrustedRelease[],
): TrustNotice => {
  const unverified = releases.filter((one) => one.trust === UNVERIFIED);

  return {
    unverified: unique(unverified.map((one) => one.title)),
    authors: unique(unverified.map((one) => one.author ?? '')),
    trusted: unique(
      releases.filter((one) => one.trust === TRUSTED).map((one) => one.title),
    ),
  };
};

/** Whether the person must accept the risk before the install starts. */
export const needsConsent = (notice: TrustNotice): boolean =>
  notice.unverified.length > 0;
