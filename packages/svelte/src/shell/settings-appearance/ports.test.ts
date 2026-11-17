import { describe, expect, it } from 'vitest';

import { Preferences } from '../../data';
import type { HomeSource } from './types';

describe('the home port', () => {
  it('is satisfied by the store this product hands in', () => {
    // Structural typing makes the match silent in both directions.
    const home: HomeSource = new Preferences(null);

    expect(home.home).toBe('dashboard');

    home.setHome('my-printer');

    expect(home.home).toBe('my-printer');
  });
});
