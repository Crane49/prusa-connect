import { describe, expect, it } from 'vitest';

import { FleetListing } from '../../data';
import { fakeFleetClient } from '../../test/fakes';
import type { FleetListingSource } from './types';

describe('the listing port', () => {
  it('is satisfied by the store this product hands in', () => {
    const { client } = fakeFleetClient([]);
    // Structural typing makes the match silent in both directions, so it is
    // asserted rather than assumed.
    const source: FleetListingSource = new FleetListing(client);

    expect(source.loaded).toBe(false);
  });
});
