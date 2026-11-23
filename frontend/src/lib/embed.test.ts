import { describe, expect, it } from 'vitest';

import { isEmbedded } from './embed';
import { PATHS } from './routes';

describe('the embedded page', () => {
  it('is a module page with embed=1', () => {
    expect(isEmbedded(PATHS.modulePage, '?embed=1')).toBe(true);
    expect(isEmbedded(PATHS.modulePage, '?printer=7&embed=1')).toBe(true);
  });

  it('is not a module page without it, or with another value', () => {
    expect(isEmbedded(PATHS.modulePage, '')).toBe(false);
    expect(isEmbedded(PATHS.modulePage, '?embed=0')).toBe(false);
    expect(isEmbedded(PATHS.modulePage, '?embed=true')).toBe(false);
  });

  // The core's own screens keep their frame, so the parameter cannot hide
  // the sign-out or the navigation of a core screen.
  it('is never a core screen', () => {
    expect(isEmbedded(PATHS.printers, '?embed=1')).toBe(false);
    expect(isEmbedded(PATHS.home, '?embed=1')).toBe(false);
    expect(isEmbedded(undefined, '?embed=1')).toBe(false);
  });
});
