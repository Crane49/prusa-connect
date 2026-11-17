import { describe, expect, it } from 'vitest';

import { type DiscoveryClient, DiscoveryVisibility } from '../../data';
import type { VisibilitySource } from './types';

const refuse = (): never => {
  throw new Error('this fake answers nothing');
};

const client: DiscoveryClient = {
  listNearbyPrinters: refuse,
  getDiscoveryVisibility: refuse,
  setDiscoveryVisibility: refuse,
};

describe('the network room port', () => {
  it('is satisfied by the store this product hands in', () => {
    // Structural typing makes the match silent in both directions, so it is
    // asserted rather than assumed.
    const source: VisibilitySource = new DiscoveryVisibility(client);

    expect(source.loaded).toBe(false);
    expect(source.hidden).toBe(false);
  });
});
