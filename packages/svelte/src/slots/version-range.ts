// Whether a version is inside a range, for the check the loader makes before
// it imports a module's bundle (ADR-20260911-4p6 rule 24).
//
// # One grammar, two readers
//
// ADR-20260910-74m rule 5a: the browser reads a range the way the host reads one.
// `sdk/manifest/versionrange.go` is the other reader, and `module add` runs it
// over the same `ui.library` string this file is handed back in the resources
// listing. A range that means one thing at install and another in the page is
// a module an operator installs and then cannot see, so this file is a port of
// that one, form for form and refusal for refusal, and the tables in
// `version-range.test.ts` are the Go tables.
//
// # The grammar
//
// It is npm's range syntax, which is what the author of a JavaScript package
// already writes. A range is one or more alternatives separated by `||`, and a
// version is in the range when it is in any one of them. Each alternative is
// one or more comparators, and a version has to satisfy every comparator in
// it:
//
//	1.2.3            exactly that version
//	=1.2.3           the same thing written out
//	>1.2.3 <2.0.0    two comparators, both of which have to hold
//	>=1.2.3          at least that version
//	<=1.2.3          that version or below
//	^1.2.3           >=1.2.3 <2.0.0, and <0.3.0 for ^0.2.3, and <0.0.4 for ^0.0.3
//	~1.2.3           >=1.2.3 <1.3.0, and ~> is the same operator
//	1.2              any 1.2 patch, which is >=1.2.0 <1.3.0
//	1.x              any 1 minor, which is >=1.0.0 <2.0.0
//	*                any version, and so is an empty range
//	1.2.3 - 2.3.4    >=1.2.3 <=2.3.4, with a partial allowed on either side
//	^1 || ^2         either alternative
//
// # Prereleases
//
// A version carrying a prerelease, such as 1.2.3-rc.1, is in a range only when
// some comparator of the alternative that matched names the same three numbers
// and carries a prerelease of its own. That is npm's rule and it is the one
// that matters in practice: `>=1.0.0` must not quietly pick up `2.0.0-rc.1`,
// because a candidate for a major release is where a breaking change lands.
//
// # What is refused
//
// A range this grammar does not name is refused rather than read halfway, and
// a refused range holds nothing: "I could not read what you are compatible
// with" is not the claim "you are compatible", and treating it as one is how a
// gate quietly stops gating. A range no version can satisfy, such as
// `>=2.0.0 <1.0.0`, is refused as well, so an author who wrote two bounds the
// wrong way round reads that here rather than from a module that never loads.
//
// # No library
//
// The `semver` package brings a parser, a coercer, a sorter and a command line
// with it for the one predicate this needs, and the host is on the standard
// library alone for the same reason. It is one file, and a table covers every
// form and every refusal.

/**
 * One version: the three numbers, the prerelease identifiers and the build
 * metadata. Build takes no part in ordering, which is semver rule 10.
 */
export interface Version {
  readonly major: number;
  readonly minor: number;
  readonly patch: number;
  /** The dot-separated prerelease identifiers, and empty for a release. */
  readonly pre: readonly string[];
  /** The build metadata after the plus sign, and empty when there is none. */
  readonly build: string;
}

/** One bound: an operator and the version it is written against. */
interface Comparator {
  readonly operator: Operator;
  readonly version: Version;
}

/** A range, read into the comparators it means. */
export interface VersionRange {
  /** The `||` branches, each a list of comparators all of which have to hold. */
  readonly alternatives: readonly (readonly Comparator[])[];
  /** The range as the author wrote it. */
  readonly text: string;
}

type Operator = '<' | '<=' | '>' | '>=' | '=';

/** How many numbers a version is: MAJOR, MINOR and PATCH. */
const VERSION_FIELDS = 3;

/** The alternative a `*`, an `x` or an empty range reads as. */
const ANY_VERSION = '>=0.0.0';

/** The operators, longest first, so that a scan reads `>=` before `>`. */
const OPERATORS: readonly Operator[] = ['>=', '<=', '>', '<', '='];

/** A prerelease or build identifier: letters, digits and hyphens. */
const IDENTIFIER = /^[0-9A-Za-z-]+$/;

/** One version as written, with the fields left out or written as a wildcard. */
interface PartialVersion {
  readonly version: Version;
  /** 3 for `*`, 2 for `1`, 1 for `1.2` and 0 for a whole version. */
  readonly wild: number;
}

const makeVersion = (
  major = 0,
  minor = 0,
  patch = 0,
  pre: readonly string[] = [],
  build = '',
): Version => ({ major, minor, patch, pre, build });

/** Whether every character is a digit, which decides how two identifiers order. */
const numeric = (text: string): boolean => /^\d+$/.test(text);

/** Whether a field stands for every value it could have. */
const wildcard = (field: string): boolean =>
  field === '*' || field === 'x' || field === 'X';

/** Whether a prerelease or a build keeps to the alphabet semver allows. */
const identifiers = (text: string): boolean =>
  text !== '' && text.split('.').every((part) => IDENTIFIER.test(part));

/**
 * One of the three numbers, refusing a zero-padded one so that `1.02.0` and
 * `1.2.0` cannot both name one version.
 */
const versionNumber = (field: string): number | undefined => {
  if (field === '' || (field.length > 1 && field.startsWith('0'))) {
    return undefined;
  }

  return numeric(field) ? Number(field) : undefined;
};

/**
 * Reads one whole version. A leading `v`, a zero-padded field and an empty
 * identifier are all refused, because each makes two spellings of one version
 * compare unequal as text.
 */
export const parseVersion = (text: string): Version | undefined => {
  let rest = text.trim();
  if (rest === '') {
    return undefined;
  }

  let build = '';
  const plus = rest.indexOf('+');
  if (plus >= 0) {
    build = rest.slice(plus + 1);
    rest = rest.slice(0, plus);
    if (!identifiers(build)) {
      return undefined;
    }
  }

  let pre: readonly string[] = [];
  const dash = rest.indexOf('-');
  if (dash >= 0) {
    pre = rest.slice(dash + 1).split('.');
    rest = rest.slice(0, dash);
    if (!identifiers(pre.join('.'))) {
      return undefined;
    }

    const padded = pre.some(
      (part) => numeric(part) && part.length > 1 && part.startsWith('0'),
    );
    if (padded) {
      return undefined;
    }
  }

  const fields = rest.split('.');
  if (fields.length !== VERSION_FIELDS) {
    return undefined;
  }

  const major = versionNumber(fields[0] ?? '');
  const minor = versionNumber(fields[1] ?? '');
  const patch = versionNumber(fields[2] ?? '');
  if (major === undefined || minor === undefined || patch === undefined) {
    return undefined;
  }

  return makeVersion(major, minor, patch, pre, build);
};

/** Writes a version back in the form `parseVersion` reads. */
export const versionText = (held: Version): string => {
  const numbers = [held.major, held.minor, held.patch].join('.');
  const pre = held.pre.length > 0 ? `-${held.pre.join('.')}` : '';
  const build = held.build === '' ? '' : `+${held.build}`;

  return numbers + pre + build;
};

/** Whether this is a release rather than a candidate for one. */
const isRelease = (held: Version): boolean => held.pre.length === 0;

/** Orders one pair of prerelease identifiers, which is semver rule 11. */
const compareIdentifier = (left: string, right: string): number => {
  const leftNumber = numeric(left);
  const rightNumber = numeric(right);

  if (leftNumber && rightNumber) {
    return Number(left) - Number(right);
  }

  if (leftNumber) {
    return -1;
  }

  if (rightNumber) {
    return 1;
  }

  if (left === right) {
    return 0;
  }

  return left < right ? -1 : 1;
};

/**
 * Orders two prerelease lists identifier by identifier: numbers below text,
 * numbers compared as numbers, and a shorter list below a longer one that
 * starts the same way.
 */
const comparePre = (
  left: readonly string[],
  right: readonly string[],
): number => {
  const shared = Math.min(left.length, right.length);

  for (let index = 0; index < shared; index += 1) {
    const order = compareIdentifier(left[index] ?? '', right[index] ?? '');
    if (order !== 0) {
      return order;
    }
  }

  return left.length - right.length;
};

/**
 * Orders two versions, answering negative, zero or positive. Build metadata is
 * ignored, which is semver rule 10, and a version with a prerelease is below
 * the same three numbers without one, which is rule 9.
 */
export const compareVersions = (left: Version, right: Version): number => {
  const numbers =
    left.major - right.major ||
    left.minor - right.minor ||
    left.patch - right.patch;

  if (numbers !== 0) {
    return numbers;
  }

  if (left.pre.length === 0 && right.pre.length === 0) {
    return 0;
  }

  if (left.pre.length === 0) {
    return 1;
  }

  if (right.pre.length === 0) {
    return -1;
  }

  return comparePre(left.pre, right.pre);
};

/**
 * The first version above everything a partial could have named: `1.2` answers
 * `1.3.0`, and `1` answers `2.0.0`.
 */
const ceilingOf = (held: PartialVersion): Version => {
  if (held.wild === VERSION_FIELDS) {
    return makeVersion();
  }

  if (held.wild === VERSION_FIELDS - 1) {
    return makeVersion(held.version.major + 1);
  }

  return makeVersion(held.version.major, held.version.minor + 1);
};

/** What `partialFields` read out of one written version. */
interface WrittenFields {
  readonly numbers: readonly number[];
  /** The prerelease and build suffix, and empty when there is none. */
  readonly rest: string;
  readonly wild: number;
}

/**
 * Reads the three numbers, the prerelease and build suffix, and how many
 * fields were left to the wildcard.
 */
const partialFields = (text: string): WrittenFields | undefined => {
  const numbers = [0, 0, 0];

  let body = text;
  let rest = '';

  const cut = text.search(/[-+]/);
  if (cut > 0) {
    body = text.slice(0, cut);
    rest = text.slice(cut);
  }

  const fields = body.split('.');
  if (fields.length > VERSION_FIELDS) {
    return undefined;
  }

  const wild = VERSION_FIELDS - fields.length;

  for (let index = 0; index < fields.length; index += 1) {
    const field = fields[index] ?? '';

    if (wildcard(field)) {
      // Every field after a wildcard is a wildcard too. `1.x.3` names no set
      // of versions, and npm reads it as `1.x`, which is reading half of what
      // the author wrote. A wildcard also takes no prerelease.
      if (!fields.slice(index + 1).every(wildcard) || rest !== '') {
        return undefined;
      }

      return { numbers, rest: '', wild: VERSION_FIELDS - index };
    }

    const number = versionNumber(field);
    if (number === undefined) {
      return undefined;
    }

    numbers[index] = number;
  }

  return { numbers, rest, wild };
};

/** Reads a version that may leave fields out or write them as a wildcard. */
const parsePartial = (text: string): PartialVersion | undefined => {
  let trimmed = text.trim();
  if (trimmed.startsWith('v')) {
    trimmed = trimmed.slice(1);
  }

  if (trimmed === '' || wildcard(trimmed)) {
    return { version: makeVersion(), wild: VERSION_FIELDS };
  }

  const read = partialFields(trimmed);
  if (!read) {
    return undefined;
  }

  const { numbers, rest, wild } = read;
  const held = makeVersion(numbers[0], numbers[1], numbers[2]);

  if (rest === '') {
    return { version: held, wild };
  }

  if (wild > 0) {
    return undefined;
  }

  const full = parseVersion(versionText(held) + rest);

  return full ? { version: full, wild } : undefined;
};

/** The two comparators a range with both ends written out means. */
const between = (floor: Version, ceiling: Version): Comparator[] => [
  { operator: '>=', version: floor },
  { operator: '<', version: ceiling },
];

/**
 * Reads a bare version, whole or partial. A whole one is an exact pin, and a
 * partial one is every version the missing fields could have been.
 */
const parsePartialRange = (text: string): Comparator[] | undefined => {
  const held = parsePartial(text);
  if (!held) {
    return undefined;
  }

  if (held.wild === 0) {
    return [{ operator: '=', version: held.version }];
  }

  if (held.wild === VERSION_FIELDS) {
    return parseSimple(ANY_VERSION);
  }

  return between(held.version, ceilingOf(held));
};

/**
 * Reads a caret range: every version up to the next change that may break,
 * which is the next value of the leftmost field that is not zero.
 */
const parseCaret = (text: string): Comparator[] | undefined => {
  const held = parsePartial(text);
  if (!held) {
    return undefined;
  }

  if (held.wild === VERSION_FIELDS) {
    return parseSimple(ANY_VERSION);
  }

  const floor = held.version;

  if (floor.major > 0 || held.wild >= VERSION_FIELDS - 1) {
    return between(floor, makeVersion(floor.major + 1));
  }

  if (floor.minor > 0 || held.wild >= 1) {
    return between(floor, makeVersion(0, floor.minor + 1));
  }

  return between(floor, makeVersion(0, 0, floor.patch + 1));
};

/**
 * Reads a tilde range: patches of the minor that was named, and minors of the
 * major when no minor was.
 */
const parseTilde = (text: string): Comparator[] | undefined => {
  const held = parsePartial(text);
  if (!held) {
    return undefined;
  }

  if (held.wild === VERSION_FIELDS) {
    return parseSimple(ANY_VERSION);
  }

  const floor = held.version;
  const ceiling =
    held.wild >= VERSION_FIELDS - 1
      ? makeVersion(floor.major + 1)
      : makeVersion(floor.major, floor.minor + 1);

  return between(floor, ceiling);
};

/**
 * Reads one primitive comparator. A partial version after an operator is
 * filled in the way npm fills it: a lower bound takes zeros, and `>1.2` means
 * everything above the whole 1.2 line.
 */
const parseOperator = (
  operator: Operator,
  text: string,
): Comparator[] | undefined => {
  const held = parsePartial(text);
  if (!held) {
    return undefined;
  }

  // A wildcard on its own is the whole range, so an operator written against
  // one is an author who meant `*` and wrote something narrower-looking.
  if (held.wild === VERSION_FIELDS) {
    return undefined;
  }

  if (held.wild === 0) {
    return [{ operator, version: held.version }];
  }

  switch (operator) {
    case '>':
      return [{ operator: '>=', version: ceilingOf(held) }];
    case '<=':
      return [{ operator: '<', version: ceilingOf(held) }];
    case '=':
      return parsePartialRange(text);
    default:
      // `>=` and `<` both take the floor: `>=1.2` is `>=1.2.0` and `<1.2` is
      // `<1.2.0`.
      return [{ operator, version: held.version }];
  }
};

/**
 * Reads one comparator, one caret, one tilde or one partial version into the
 * comparators it means.
 */
const parseSimple = (text: string): Comparator[] | undefined => {
  if (text.startsWith('^')) {
    return parseCaret(text.slice(1));
  }

  if (text.startsWith('~>')) {
    return parseTilde(text.slice(2));
  }

  if (text.startsWith('~')) {
    return parseTilde(text.slice(1));
  }

  for (const operator of OPERATORS) {
    if (text.startsWith(operator)) {
      return parseOperator(operator, text.slice(operator.length));
    }
  }

  return parsePartialRange(text);
};

/** Reads `A - B`: at least A, and at most everything B could name. */
const parseHyphen = (left: string, right: string): Comparator[] | undefined => {
  const lower = parsePartial(left);
  const upper = parsePartial(right);
  if (!lower || !upper) {
    return undefined;
  }

  const floor: Comparator = { operator: '>=', version: lower.version };

  if (upper.wild === 0) {
    return [floor, { operator: '<=', version: upper.version }];
  }

  if (upper.wild === VERSION_FIELDS) {
    return [floor];
  }

  return [floor, { operator: '<', version: ceilingOf(upper) }];
};

/** Reads one `||` branch into the comparators it means. */
const parseAlternative = (text: string): Comparator[] | undefined => {
  const fields = text.trim().split(/\s+/).filter(Boolean);
  if (fields.length === 0) {
    return parseSimple('*');
  }

  // A hyphen range is three fields and is read whole, because its two sides
  // mean different things from the same spelling on its own.
  if (fields.length === VERSION_FIELDS && fields[1] === '-') {
    return parseHyphen(fields[0] ?? '', fields[2] ?? '');
  }

  const out: Comparator[] = [];

  for (const field of fields) {
    // A hyphen range is written `A - B`, with one version on each side.
    if (field === '-') {
      return undefined;
    }

    const parsed = parseSimple(field);
    if (!parsed) {
      return undefined;
    }

    out.push(...parsed);
  }

  return out;
};

/**
 * Whether an alternative is one no version can satisfy, which is an author who
 * wrote the two bounds the wrong way round or repeated one.
 *
 * It compares the highest floor against the lowest ceiling, which catches
 * every way of writing a range that holds nothing with two comparators.
 */
const holdsNothing = (alternative: readonly Comparator[]): boolean => {
  let floor: Comparator | undefined;
  let ceiling: Comparator | undefined;

  for (const held of alternative) {
    const isFloor =
      held.operator === '>' || held.operator === '>=' || held.operator === '=';
    if (
      isFloor &&
      (!floor || compareVersions(held.version, floor.version) > 0)
    ) {
      floor = held;
    }

    const isCeiling =
      held.operator === '<' || held.operator === '<=' || held.operator === '=';
    if (
      isCeiling &&
      (!ceiling || compareVersions(held.version, ceiling.version) < 0)
    ) {
      ceiling = held;
    }
  }

  if (!floor || !ceiling) {
    return false;
  }

  const order = compareVersions(floor.version, ceiling.version);
  const closed = floor.operator !== '>' && ceiling.operator !== '<';

  return order > 0 || (order === 0 && !closed);
};

/**
 * Reads one range, and answers nothing for a range this grammar does not name
 * or no version can satisfy.
 */
export const parseRange = (text: string): VersionRange | undefined => {
  const alternatives: Comparator[][] = [];

  for (const alternative of text.split('||')) {
    const parsed = parseAlternative(alternative);
    if (!parsed || holdsNothing(parsed)) {
      return undefined;
    }

    alternatives.push(parsed);
  }

  return { alternatives, text: text.trim() };
};

/**
 * Writes what the range was read as, in comparators, so a refusal can show an
 * author how their range was understood.
 */
export const describeRange = (range: VersionRange): string =>
  range.alternatives
    .map((alternative) =>
      alternative
        .map((held) => held.operator + versionText(held.version))
        .join(' '),
    )
    .join(' || ');

/** Whether a version satisfies one comparator. */
const meets = (held: Version, bound: Comparator): boolean => {
  const order = compareVersions(held, bound.version);

  switch (bound.operator) {
    case '>':
      return order > 0;
    case '>=':
      return order >= 0;
    case '<':
      return order < 0;
    case '<=':
      return order <= 0;
    case '=':
      return order === 0;
  }
};

/**
 * Whether a version meets every comparator of one alternative, under npm's
 * prerelease rule.
 */
const satisfies = (
  held: Version,
  alternative: readonly Comparator[],
): boolean => {
  if (!alternative.every((bound) => meets(held, bound))) {
    return false;
  }

  if (isRelease(held)) {
    return true;
  }

  // A candidate is in the range only where the author named a candidate of the
  // same three numbers. Otherwise `>=1.0.0` would pick up 2.0.0-rc.1, which is
  // where a breaking change lands.
  return alternative.some(
    (bound) =>
      !isRelease(bound.version) &&
      bound.version.major === held.major &&
      bound.version.minor === held.minor &&
      bound.version.patch === held.patch,
  );
};

/** Whether one parsed version is in the range. */
export const rangeHolds = (range: VersionRange, held: Version): boolean =>
  range.alternatives.some((alternative) => satisfies(held, alternative));

/**
 * True only when the range reads and the version is inside it. A range one
 * alternative of which will not parse fails whole rather than being skipped: a
 * range half of which is nonsense is a range nobody has checked.
 */
export const satisfiesRange = (version: string, range: string): boolean => {
  const held = parseVersion(version);
  if (!held) {
    return false;
  }

  const parsed = parseRange(range);

  return parsed ? rangeHolds(parsed, held) : false;
};
