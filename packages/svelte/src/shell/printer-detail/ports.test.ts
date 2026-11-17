import { describe, expect, it } from 'vitest';

import { LiveTelemetry } from '../../data';
import type { TelemetryClient } from '../../data/services/telemetry';
import type { TelemetrySource } from './types';

const client: TelemetryClient = {
  events: () => {
    throw new Error('this fake opens no stream');
  },
};

describe('the telemetry port', () => {
  it('is satisfied by the store this product hands in', () => {
    // Structural typing makes the match silent in both directions, so it is
    // asserted rather than assumed.
    const source: TelemetrySource = new LiveTelemetry(client, 'printer-1');

    expect(source.value('/state')).toBeUndefined();
    expect(source.points('/tools/0/nozzle_temp')).toEqual([]);
  });
});
