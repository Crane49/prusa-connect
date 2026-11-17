import { describe, expect, it } from 'vitest';

import {
  compareVersions,
  describeRange,
  parseRange,
  parseVersion,
  rangeHolds,
  satisfiesRange,
} from './version-range';

// A range is what a module author writes to say which versions it runs on, and
// the grammar is npm's, because that is the one they already know. These
// tables are the tables `sdk/manifest/versionrange_test.go` and
// `sdk/manifest/ui_test.go` hold the host to, case for case. Two readers of
// one grammar drift apart the moment only one of them is covered, and the
// answer they disagree on is a module an operator installs and cannot see.

/** Every form of the grammar, and the comparators npm reads it as. */
const FORMS: [range: string, reads: string][] = [
  // Exact pins.
  ['1.2.3', '=1.2.3'],
  ['=1.2.3', '=1.2.3'],
  ['v1.2.3', '=1.2.3'],

  // The four comparators, written out.
  ['>=1.2.3', '>=1.2.3'],
  ['>1.2.3', '>1.2.3'],
  ['<=1.2.3', '<=1.2.3'],
  ['<1.2.3', '<1.2.3'],

  // Two comparators in one alternative, which is the form this library wrote
  // before the grammar widened.
  ['>=0.3.0 <1.0.0', '>=0.3.0 <1.0.0'],
  ['>1.2.3 <2.0.0', '>1.2.3 <2.0.0'],

  // Caret: up to the next change that may break.
  ['^1.2.3', '>=1.2.3 <2.0.0'],
  ['^0.2.3', '>=0.2.3 <0.3.0'],
  ['^0.0.3', '>=0.0.3 <0.0.4'],
  ['^1.2.x', '>=1.2.0 <2.0.0'],
  ['^0.0.x', '>=0.0.0 <0.1.0'],
  ['^1.x', '>=1.0.0 <2.0.0'],
  ['^0.x', '>=0.0.0 <1.0.0'],
  ['^1', '>=1.0.0 <2.0.0'],

  // Tilde: patches of the minor that was named.
  ['~1.2.3', '>=1.2.3 <1.3.0'],
  ['~1.2', '>=1.2.0 <1.3.0'],
  ['~1', '>=1.0.0 <2.0.0'],
  ['~0.2.3', '>=0.2.3 <0.3.0'],
  ['~>1.2.3', '>=1.2.3 <1.3.0'],

  // Partial versions and wildcards.
  ['1.2', '>=1.2.0 <1.3.0'],
  ['1.2.x', '>=1.2.0 <1.3.0'],
  ['1.x', '>=1.0.0 <2.0.0'],
  ['1', '>=1.0.0 <2.0.0'],
  ['*', '>=0.0.0'],
  ['x', '>=0.0.0'],
  ['', '>=0.0.0'],

  // Partial versions after a comparator, filled in the way npm fills them: a
  // floor takes zeros and an open upper bound takes the next line.
  ['>=1.2', '>=1.2.0'],
  ['<1.2', '<1.2.0'],
  ['>1.2', '>=1.3.0'],
  ['>1', '>=2.0.0'],
  ['<=1.2', '<1.3.0'],
  ['=1.2', '>=1.2.0 <1.3.0'],

  // Hyphen ranges, with a partial on either side.
  ['1.2.3 - 2.3.4', '>=1.2.3 <=2.3.4'],
  ['1.2.3 - 2.3', '>=1.2.3 <2.4.0'],
  ['1.2.3 - 2', '>=1.2.3 <3.0.0'],
  ['1.2 - 2.3.4', '>=1.2.0 <=2.3.4'],

  // Alternatives.
  ['^1 || ^2', '>=1.0.0 <2.0.0 || >=2.0.0 <3.0.0'],
  ['1.2.3 || >=2.0.0', '=1.2.3 || >=2.0.0'],
  ['<1.0.0 || >=2.0.0', '<1.0.0 || >=2.0.0'],

  // Prereleases are versions like any other, and they carry into the
  // comparator they were written on.
  ['>=1.0.0-rc.1', '>=1.0.0-rc.1'],
  ['1.0.0-rc.1', '=1.0.0-rc.1'],
  ['^1.0.0-rc.1', '>=1.0.0-rc.1 <2.0.0'],
  ['>=1.0.0-alpha.1+b7', '>=1.0.0-alpha.1+b7'],
];

/** Each of these is a shape an author writes by mistake. */
const REFUSALS: [what: string, range: string][] = [
  ['a version with four fields', '1.2.3.4'],
  ['a version with a letter in it', '1.2.beta'],
  ['a zero-padded field', '1.02.0'],
  ['an empty field', '1..3'],
  ['a comparator with no version', '>='],
  ['an operator on a wildcard', '>=*'],
  ['a wildcard before a number', '1.x.3'],
  ['a prerelease on a wildcard', '1.x-rc.1'],
  ['a hyphen with nothing after it', '1.2.3 -'],
  ['a hyphen with nothing before', '- 2.0.0'],
  ['an empty prerelease', '1.2.3-'],
  ['an empty build', '1.2.3+'],
  ['a prerelease with an empty part', '1.2.3-rc..1'],
  ['a prerelease outside the alphabet', '1.2.3-rc/1'],
  ['a zero-padded prerelease number', '1.2.3-01'],
  ['a comparator nobody writes', '=>1.2.3'],
  ['a negative field', '1.-2.3'],

  // A range no version can satisfy, refused when it is read rather than
  // matching nothing later.
  ['a range that holds one version and excludes it', '>=1.0.0 <1.0.0'],
  ['bounds the wrong way round', '>=2.0.0 <1.0.0'],
];

/** Is this version in this range: the answer the loader actually reads. */
const MEMBERS: [range: string, version: string, inside: boolean][] = [
  ['^1.2.3', '1.2.3', true],
  ['^1.2.3', '1.9.9', true],
  ['^1.2.3', '2.0.0', false],
  ['^1.2.3', '1.2.2', false],
  ['~1.2.3', '1.2.9', true],
  ['~1.2.3', '1.3.0', false],
  ['^0.2.3', '0.2.9', true],
  ['^0.2.3', '0.3.0', false],
  ['1.2.3', '1.2.3', true],
  ['1.2.3', '1.2.4', false],
  ['*', '0.0.1', true],
  ['*', '99.0.0', true],
  ['>=0.3.0 <1.0.0', '0.3.0', true],
  ['>=0.3.0 <1.0.0', '1.0.0', false],
  ['1.2.3 - 2.3.4', '2.3.4', true],
  ['1.2.3 - 2.3.4', '2.3.5', false],
  ['^1 || ^3', '2.0.0', false],
  ['^1 || ^3', '3.4.5', true],

  // The prerelease rule. A candidate is in a range only where the author named
  // a candidate of the same three numbers.
  ['>=1.0.0', '2.0.0-rc.1', false],
  ['>=1.0.0-rc.1', '1.0.0-rc.2', true],
  ['>=1.0.0-rc.1', '1.0.0-rc.0', false],
  ['>=1.0.0-rc.1 <2.0.0', '1.5.0', true],
  ['^1.0.0-rc.1', '2.0.0-rc.1', false],
  ['1.2.3-rc.1', '1.2.3-rc.1', true],
  ['1.2.3-rc.1', '1.2.3', false],

  // Semver's own ordering of prereleases, read through a range.
  ['>1.0.0-alpha', '1.0.0-alpha.1', true],
  ['>1.0.0-alpha.1', '1.0.0-alpha.beta', true],
  ['>1.0.0-beta.2', '1.0.0-beta.11', true],
  ['>1.0.0-rc.1', '1.0.0-beta.11', false],

  // Build metadata takes no part in ordering.
  ['1.2.3', '1.2.3+build.7', true],
];

/** Every pair is left below right, which is the example order in semver 11. */
const ORDER: [below: string, above: string][] = [
  ['1.0.0', '2.0.0'],
  ['2.0.0', '2.1.0'],
  ['2.1.0', '2.1.1'],
  ['1.0.0-alpha', '1.0.0'],
  ['1.0.0-alpha', '1.0.0-alpha.1'],
  ['1.0.0-alpha.1', '1.0.0-alpha.beta'],
  ['1.0.0-alpha.beta', '1.0.0-beta'],
  ['1.0.0-beta', '1.0.0-beta.2'],
  ['1.0.0-beta.2', '1.0.0-beta.11'],
  ['1.0.0-beta.11', '1.0.0-rc.1'],
  ['1.0.0-rc.1', '1.0.0'],
];

/** Reads a version the tables name, and fails the case when it will not read. */
const versionOf = (text: string) => {
  const held = parseVersion(text);
  expect(held, `${text} was refused`).toBeDefined();

  return held!;
};

describe('a range is read into the comparators it means', () => {
  it.each(FORMS)('%j reads as %j', (range, reads) => {
    const parsed = parseRange(range);
    expect(parsed, `${range} was refused`).toBeDefined();
    expect(describeRange(parsed!)).toBe(reads);
  });
});

describe('a range this grammar does not name is refused', () => {
  it.each(REFUSALS)('refuses %s', (_what, range) => {
    expect(parseRange(range)).toBeUndefined();
  });

  // Reading half of a range would let a bundle load against a library that
  // broke it, so a refused range holds nothing rather than everything.
  it.each(REFUSALS)('holds no version for %s', (_what, range) => {
    expect(satisfiesRange('1.2.3', range)).toBe(false);
  });
});

describe('which versions a range holds', () => {
  it.each(MEMBERS)('%j holds %j: %j', (range, version, inside) => {
    const parsed = parseRange(range);
    expect(parsed, `${range} was refused`).toBeDefined();
    expect(rangeHolds(parsed!, versionOf(version))).toBe(inside);
  });

  // The same answers through the one function the loader calls, because that
  // is the surface, and a range it reads differently is the defect this table
  // exists to catch.
  it.each(MEMBERS)('satisfiesRange(%j, %j) is %j', (range, version, inside) => {
    expect(satisfiesRange(version, range)).toBe(inside);
  });
});

describe('two versions are ordered as semver says they are', () => {
  it.each(ORDER)('%j is below %j', (below, above) => {
    expect(compareVersions(versionOf(below), versionOf(above))).toBeLessThan(0);
    expect(compareVersions(versionOf(above), versionOf(below))).toBeGreaterThan(
      0,
    );
  });

  it('ignores build metadata, which is semver rule 10', () => {
    expect(
      compareVersions(versionOf('1.0.0'), versionOf('1.0.0+build.7')),
    ).toBe(0);
  });
});

describe('parseVersion', () => {
  it('reads the three numbers, the prerelease and the build', () => {
    expect(parseVersion('0.3.1')).toEqual({
      major: 0,
      minor: 3,
      patch: 1,
      pre: [],
      build: '',
    });
    expect(parseVersion('1.2.3-rc.1+b7')).toEqual({
      major: 1,
      minor: 2,
      patch: 3,
      pre: ['rc', '1'],
      build: 'b7',
    });
  });

  // A version is not a range. The `v` and the missing fields a range accepts
  // are filled in while the range is read, and one version has one spelling.
  it('reads nothing from a partial version or a leading v', () => {
    expect(parseVersion('1.2')).toBeUndefined();
    expect(parseVersion('v1.2.3')).toBeUndefined();
    expect(parseVersion('latest')).toBeUndefined();
    expect(parseVersion('')).toBeUndefined();
  });
});

describe('satisfiesRange', () => {
  it('refuses a version it cannot read', () => {
    expect(satisfiesRange('latest', '*')).toBe(false);
    expect(satisfiesRange('1.2', '^1.0.0')).toBe(false);
  });

  // One alternative that will not parse fails the whole range. A range half of
  // which is nonsense is a range nobody has checked.
  it('fails a range one alternative of which will not read', () => {
    expect(satisfiesRange('1.2.3', '^1.0.0 || nonsense')).toBe(false);
    expect(satisfiesRange('1.2.3', 'whatever')).toBe(false);
  });

  it('answers the library check the loader makes', () => {
    expect(satisfiesRange('0.1.0', '>=0.1.0 <1.0.0')).toBe(true);
    expect(satisfiesRange('1.0.0', '>=0.1.0 <1.0.0')).toBe(false);
  });
});
