import { describe, expect, it } from 'vitest';

import { type DiscoveryClient, NearbyPrinters } from '../../data';
import type { NearbySource } from './types';

const refuse = (): never => {
  throw new Error('this fake answers nothing');
};

const client: DiscoveryClient = {
  listNearbyPrinters: refuse,
  getDiscoveryVisibility: refuse,
  setDiscoveryVisibility: refuse,
};

describe('the nearby printers port', () => {
  it('is satisfied by the store this product hands in', () => {
    // Structural typing makes the match silent in both directions, so it is
    // asserted rather than assumed.
    const source: NearbySource = new NearbyPrinters(client);

    expect(source.loaded).toBe(false);
    expect(source.items).toEqual([]);
  });
});
