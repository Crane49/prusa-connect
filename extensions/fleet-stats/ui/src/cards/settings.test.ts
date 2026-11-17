import { describe, expect, it } from 'vitest';

import { cardViewOf, failureLimitOf, pageSearchOf } from './settings';

describe('a card of this module', () => {
  it('reads its period and scope, and falls back where a value is missing', () => {
    expect(
      cardViewOf({ period: '30', printers: ['a', 3], tag: 'garage' }),
    ).toEqual({
      days: 30,
      scope: { printers: ['a'], tag: 'garage' },
    });
    expect(cardViewOf({})).toEqual({
      days: 7,
      scope: { printers: [], tag: '' },
    });
  });

  it('keeps the failures it lists between one and twenty', () => {
    expect(failureLimitOf({ limit: 8 })).toBe(8);
    expect(failureLimitOf({ limit: 99 })).toBe(20);
    expect(failureLimitOf({})).toBe(5);
  });

  it('opens the page on one printer, and on all of them for several', () => {
    expect(
      pageSearchOf({ days: 30, scope: { printers: ['a'], tag: '' } }),
    ).toBe('?days=30&printer=a');
    expect(
      pageSearchOf({ days: 7, scope: { printers: ['a', 'b'], tag: 'x' } }),
    ).toBe('?tag=x');
  });
});
