// The skin the server names has to reach the root element, and a name the
// theme has no block for has to leave the page on the default.
//
// It runs in a browser rather than under Node because the attribute is read
// off a real document element.
import { afterEach, describe, expect, it } from 'vitest';

import { applySkin, DEFAULT_SKIN, SKIN_NAMES } from './skin';

afterEach(() => {
  delete document.documentElement.dataset.skin;
});

describe('the skin this library draws in', () => {
  it('offers the three skins the theme declares, the default first', () => {
    expect([...SKIN_NAMES]).toEqual(['prusa', 'slicer', 'prusapro']);
    expect(SKIN_NAMES[0]).toBe(DEFAULT_SKIN);
  });

  it('writes the name onto the root element', () => {
    for (const name of SKIN_NAMES) {
      applySkin(name);

      expect(document.documentElement.dataset.skin, name).toBe(name);
    }
  });

  // The server refuses an unknown name at startup, so this is a server of
  // another build.
  it('falls back to the default for a name it has no block for', () => {
    applySkin('candidate-chartreuse');

    expect(document.documentElement.dataset.skin).toBe(DEFAULT_SKIN);
  });
});
