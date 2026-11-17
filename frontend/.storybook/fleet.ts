// The printers every app story sees, in three fleet sizes. The first five are
// the devices of the UX fixture table, in its order, so the jobs, files and
// history that table answers for them still belong to a printer on screen. A
// farm adds seven copies of them with their own ids, names and states.
import { getContext } from 'svelte';

import fixtures from '../test/ux/fixtures/api.json';

export type FleetSize = 'one' | 'two' | 'many';

/** One item of `GET /api/v1/devices`, as the core writes it. */
export interface DeviceJson {
  readonly id: string;
  readonly name: string;
  readonly model: string;
  readonly serial: string;
  readonly state: Readonly<Record<string, unknown>>;
  readonly [field: string]: unknown;
}

interface Listing {
  readonly items: DeviceJson[];
  readonly [field: string]: unknown;
}

const listing = JSON.parse(fixtures['GET /api/v1/devices'].body) as Listing;

const recorded = listing.items;

const STATES = ['PRINTING', 'IDLE', 'ATTENTION', 'ERROR', 'FINISHED'] as const;

const FARM = 12;

// A copy keeps the model's capabilities and actions. Its job is left out, as
// the recorded job ids belong to the recorded printers; a printing copy shows
// progress from its telemetry alone.
const copy = (number: number): DeviceJson => {
  const index = (number - recorded.length - 1) % recorded.length;
  const source = recorded[index];
  if (source === undefined) throw new Error('the fixture has no devices');
  const serial = `DEMO-${String(number).padStart(2, '0')}`;
  const telemetry = Object.fromEntries(
    Object.entries(source.state).filter(([key]) => !key.startsWith('/job/')),
  );
  const state = STATES[index] ?? 'IDLE';

  return {
    ...source,
    job: undefined,
    id: `00000000-0000-4000-8000-${String(number).padStart(12, '0')}`,
    name: `${source.model} ${serial}`,
    serial,
    unit_uid: `DEMO${String(number).padStart(11, '0')}`,
    state:
      state === 'PRINTING'
        ? { ...telemetry, '/state': state, '/job/progress': 42 }
        : { ...telemetry, '/state': state },
  };
};

const copies = Array.from({ length: FARM - recorded.length }, (_, i) =>
  copy(recorded.length + 1 + i),
);

const farm: readonly DeviceJson[] = [...recorded, ...copies];

/** The recorded printer each copy was made from, by the copy's id. */
export const copiedFrom: ReadonlyMap<string, string> = new Map(
  copies.map((device, i) => [
    device.id,
    recorded[i % recorded.length]?.id ?? '',
  ]),
);

const COUNT: Record<FleetSize, number> = { one: 1, two: 2, many: FARM };

export const fleetOf = (size: FleetSize): DeviceJson[] =>
  farm.slice(0, COUNT[size]);

/** The whole `GET /api/v1/devices` answer for a fleet of this size. */
export const devicesAnswer = (size: FleetSize): Response => {
  const items = fleetOf(size);

  return new Response(
    JSON.stringify({ ...listing, items, total: items.length }),
    { status: 200, headers: { 'content-type': 'application/json' } },
  );
};

/** Where `fleet-frame.svelte` puts the toolbar's `printers` global. */
export const SHOWN_FLEET = Symbol('printers');

/** The fleet size the toolbar shows, read while a component initialises. */
export const shownFleet = (): (() => FleetSize) =>
  getContext<(() => FleetSize) | undefined>(SHOWN_FLEET) ?? (() => 'many');
