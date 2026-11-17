import { describe, expect, it } from 'vitest';

import fixtures from '../test/ux/fixtures/api.json';
import { devicesAnswer, fleetOf } from './fleet';

const recorded = (
  JSON.parse(fixtures['GET /api/v1/devices'].body) as { items: unknown[] }
).items;

describe('the fleet', () => {
  it('holds one, two or twelve printers', () => {
    expect(fleetOf('one')).toHaveLength(1);
    expect(fleetOf('two')).toHaveLength(2);
    expect(fleetOf('many')).toHaveLength(12);
  });

  it('gives every printer its own id and name', () => {
    const many = fleetOf('many');
    expect(new Set(many.map((d) => d.id)).size).toBe(12);
    expect(new Set(many.map((d) => d.name)).size).toBe(12);
  });

  it('starts with the recorded printers, so the rest of the table still points at them', () => {
    expect(fleetOf('many').slice(0, 5)).toEqual(recorded);
  });

  it('answers a listing whose total is its length', async () => {
    const body = (await devicesAnswer('two').json()) as {
      items: unknown[];
      total: number;
    };
    expect(body.items).toHaveLength(2);
    expect(body.total).toBe(2);
  });
});
