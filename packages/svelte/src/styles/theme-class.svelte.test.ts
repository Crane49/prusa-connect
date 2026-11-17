// `tokens.css` matches the dark variant on a `dark` class on an ancestor, so
// the preference the store works out has to reach the root element.
//
// It runs in a browser rather than under Node because the class is read off a
// real document element.
import { afterEach, describe, expect, it } from 'vitest';

import { applyTheme } from './theme.svelte';

afterEach(() => {
  document.documentElement.classList.remove('dark');
});

describe('the class the dark variant matches', () => {
  it('adds the class for a dark theme', () => {
    applyTheme(true);

    expect(document.documentElement.classList.contains('dark')).toBe(true);
  });

  it('removes the class for a light theme', () => {
    document.documentElement.classList.add('dark');

    applyTheme(false);

    expect(document.documentElement.classList.contains('dark')).toBe(false);
  });

  it('is repeatable', () => {
    applyTheme(true);
    applyTheme(true);

    expect(document.documentElement.classList.contains('dark')).toBe(true);
  });
});
