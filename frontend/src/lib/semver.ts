// Ordering two release versions, for the one question the Extensions page
// asks: is the release on offer newer than the one installed.
//
// Semantic versioning precedence: the three numbers in order, then a release
// beats any prerelease of the same numbers, then prerelease identifiers left to
// right, numeric ones by value and below alphanumeric ones. Build metadata
// after `+` is ignored. A version this cannot read is treated as unknown, and
// unknown is never newer: the page offers no update it cannot justify.

interface Parsed {
  readonly core: readonly [number, number, number];
  readonly pre: readonly string[];
}

const SEMVER =
  /^v?(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-([0-9A-Za-z.-]+))?(?:\+[0-9A-Za-z.-]+)?$/u;

const NUMERIC = /^\d+$/u;

const parse = (version: string): Parsed | undefined => {
  const match = SEMVER.exec(version.trim());
  if (!match) return undefined;

  return {
    core: [Number(match[1]), Number(match[2]), Number(match[3])],
    pre: match[4] === undefined ? [] : match[4].split('.'),
  };
};

const compareIdentifier = (a: string, b: string): number => {
  const aNumeric = NUMERIC.test(a);
  const bNumeric = NUMERIC.test(b);

  if (aNumeric && bNumeric) return Number(a) - Number(b);
  if (aNumeric) return -1;
  if (bNumeric) return 1;

  if (a === b) return 0;

  return a < b ? -1 : 1;
};

const comparePre = (a: readonly string[], b: readonly string[]): number => {
  // A release outranks every prerelease of the same three numbers.
  if (a.length === 0 || b.length === 0) return b.length - a.length;

  for (let at = 0; at < Math.min(a.length, b.length); at += 1) {
    const order = compareIdentifier(a[at] ?? '', b[at] ?? '');
    if (order !== 0) return order;
  }

  return a.length - b.length;
};

/**
 * Below zero when `a` is older than `b`, above zero when newer, zero when they
 * are the same release, and undefined when either cannot be read.
 */
export const compareVersions = (a: string, b: string): number | undefined => {
  const left = parse(a);
  const right = parse(b);
  if (!left || !right) return undefined;

  for (let at = 0; at < left.core.length; at += 1) {
    const order = (left.core[at] ?? 0) - (right.core[at] ?? 0);
    if (order !== 0) return order;
  }

  return comparePre(left.pre, right.pre);
};

/** True when `offered` is a newer release than `installed`. */
export const isNewer = (offered: string, installed: string): boolean =>
  (compareVersions(offered, installed) ?? 0) > 0;
