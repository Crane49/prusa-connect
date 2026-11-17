import { describe, expect, it } from 'vitest';

import { byteLength, tagProblemOf } from '../presentation';
import { fitEdit } from './byte-limits';

// What one edit becomes in a field holding at most `limit` UTF-8 bytes. The
// page runs this on every input; these cases are the edges of it.

describe('an edit fitted to a byte limit', () => {
  it('lets an edit that fits through untouched', () => {
    expect(fitEdit('abc', 3, 3, 'abcd', 4)).toBeUndefined();
    // Exactly at the limit, counted in bytes: ř is two of them.
    expect(fitEdit('', 0, 0, 'řř', 4)).toBeUndefined();
  });

  it('keeps as many typed characters with diacritics as fit, counted in bytes', () => {
    const fitted = fitEdit('', 0, 0, 'ř'.repeat(70), 120);

    expect(fitted?.value).toBe('ř'.repeat(60));
    expect(byteLength(fitted?.value ?? '')).toBe(120);
    expect(fitted?.caret).toBe(60);
  });

  it('never cuts a character in half, an emoji included', () => {
    // Three bytes are left and the emoji takes four, so none of it lands.
    const fitted = fitEdit('a', 1, 1, 'a😀', 4);

    expect(fitted).toEqual({ value: 'a', caret: 1 });
    // One byte too many in the middle of a two-byte character.
    expect(fitEdit('', 0, 0, 'aaé', 3)?.value).toBe('aa');
  });

  it('keeps the text after the caret when the person types in the middle', () => {
    // "ab|cd" with room for one more byte: the typed "xyz" keeps only "x".
    const fitted = fitEdit('abcd', 2, 2, 'abxyzcd', 5);

    expect(fitted).toEqual({ value: 'abxcd', caret: 3 });
  });

  it('counts the room a replaced selection frees', () => {
    // "a[bc]d" replaced by "éé" is "aééd", six bytes; with five, one é fits.
    const fitted = fitEdit('abcd', 1, 3, 'aééd', 5);

    expect(fitted).toEqual({ value: 'aéd', caret: 2 });
  });

  it('clips a pasted text to the room left', () => {
    const fitted = fitEdit('Hook ', 5, 5, 'Hook žlutý držák', 10);

    expect(fitted?.value).toBe('Hook žlut');
    expect(byteLength(fitted?.value ?? '')).toBeLessThanOrEqual(10);
  });

  it('keeps the start of a value it cannot read as one insertion', () => {
    // The value changed in a way no single insertion explains.
    const fitted = fitEdit('xyz', 1, 1, 'ééé', 4);

    expect(fitted).toEqual({ value: 'éé', caret: 2 });
  });
});

describe('the tags a person typed', () => {
  it('is fine with what the module keeps', () => {
    expect(tagProblemOf(['bike', 'petg'])).toBeUndefined();
    expect(tagProblemOf(['ž'.repeat(16)])).toBeUndefined();
  });

  it('names a tag longer than 32 bytes, counted in bytes', () => {
    expect(tagProblemOf(['ok', 'ž'.repeat(17)])).toEqual({
      kind: 'long',
      tag: 'ž'.repeat(17),
      limit: 32,
    });
  });

  it('says when there are more tags than a part carries', () => {
    const many = Array.from({ length: 17 }, (_, index) => `t${String(index)}`);

    expect(tagProblemOf(many)).toEqual({ kind: 'many', limit: 16 });
    expect(tagProblemOf(many.slice(0, 16))).toBeUndefined();
  });
});
