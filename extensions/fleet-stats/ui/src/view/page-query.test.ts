import { describe, expect, it } from 'vitest';

import { pageViewOf, searchOf, WHOLE_PAGE } from './page-query';

describe('the page address', () => {
  it('reads what it wrote', () => {
    const view = { days: 30 as const, printer: 'mk4-01', tag: 'garage' };
    expect(searchOf(view)).toBe('?days=30&printer=mk4-01&tag=garage');
    expect(pageViewOf(searchOf(view))).toEqual(view);
  });

  it('writes nothing for the default view, and reads anything else as it', () => {
    expect(searchOf(WHOLE_PAGE)).toBe('');
    expect(pageViewOf('')).toEqual(WHOLE_PAGE);
    expect(pageViewOf('?days=365&other=1')).toEqual(WHOLE_PAGE);
  });
});
