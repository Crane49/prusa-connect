import { describe, expect, it } from 'vitest';

import { slicerLanguage } from './language';

describe('the language PrusaSlicer names', () => {
  it('is the language before the region', () => {
    expect(slicerLanguage('cs_CZ')).toBe('cs');
    expect(slicerLanguage('de_DE')).toBe('de');
    expect(slicerLanguage('pl')).toBe('pl');
  });

  it('reads Catalan as Spanish', () => {
    expect(slicerLanguage('ca_ES')).toBe('es');
  });

  it('is English for a language this page has no catalogue for', () => {
    expect(slicerLanguage('ja_JP')).toBe('en');
    expect(slicerLanguage('')).toBe('en');
  });
});
