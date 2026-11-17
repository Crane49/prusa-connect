import { describe, expect, it } from 'vitest';

import { sameTag, tagKey } from './tags';

describe('tagKey', () => {
  it('folds case the way the server does', () => {
    expect(sameTag('PETG only', 'petg ONLY')).toBe(true);
    expect(sameTag('workshop', 'workshops')).toBe(false);
  });

  // A family emoji is three people joined by U+200D. Written without the
  // joiners it reads as three people side by side, and the server takes the
  // two as one tag.
  it('ignores the joiners in a family emoji', () => {
    const joined = '\u{1F468}\u200D\u{1F469}\u200D\u{1F467}';
    const apart = '\u{1F468}\u{1F469}\u{1F467}';

    expect(sameTag(joined, apart)).toBe(true);
  });

  it('ignores the non-joiner and both presentation selectors', () => {
    expect(sameTag('a\u200Cb', 'ab')).toBe(true);
    expect(sameTag('\u2764\uFE0F', '\u2764\uFE0E')).toBe(true);
    expect(sameTag('\u2764\uFE0F', '\u2764')).toBe(true);
  });

  // Greek has two lower-case sigmas, and a fold takes both to one letter.
  it('takes the final sigma and the other sigma as one letter', () => {
    expect(sameTag('οδος', 'ΟΔΟΣ')).toBe(true);
    expect(sameTag('οδος', 'οδοσ')).toBe(true);
  });

  it('keeps an accent, which a person can see', () => {
    expect(sameTag('café', 'cafe')).toBe(false);
  });

  // Go folds one letter to one letter, and the upper case of the sharp s is
  // two letters. So the two spellings stay two tags on the server.
  it('keeps the sharp s apart from a double s', () => {
    expect(sameTag('straße', 'strasse')).toBe(false);
    expect(sameTag('STRAẞE', 'straße')).toBe(true);
  });

  it('folds the Kelvin sign to the letter k', () => {
    expect(sameTag('K', 'k')).toBe(true);
  });

  // The server trims a tag and makes a run of spaces one space.
  it('collapses a run of spaces and trims the ends', () => {
    expect(sameTag('  night   shift ', 'night shift')).toBe(true);
    expect(sameTag('night	shift', 'night shift')).toBe(true);
  });

  it('answers the same key for the same tag', () => {
    expect(tagKey('Night Shift')).toBe(tagKey('night shift'));
  });
});
