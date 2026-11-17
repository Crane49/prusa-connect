// The sample grants the access stories and tests read. The people are the
// People screen's: two administrators, a shared operator account and a viewer.
import type { Account } from '../../data';
import type { Device, DirectRelation } from '../../domain';
import { fakeDevice } from '../../test/fakes';
import { people } from '../settings-people/people.fixtures';
import type {
  AccessGrantsSource,
  AccessPeopleSource,
  AccessPrintersSource,
} from './types';

export { people, SUBJECT } from '../settings-people/people.fixtures';

export const PRINTER = 'p-core-one';

/** The workshop account is blocked here; the intern was raised to operator. */
export const grantsOnPrinter: Record<string, DirectRelation[]> = {
  'user:workshop': ['blocked'],
  'user:intern': ['operator'],
};

export interface FakeGrants extends AccessGrantsSource {
  readonly asked: string[];
}

/** A grants port that records what it was asked and changes nothing. */
export const fakeGrants = (
  held: Record<string, DirectRelation[]>,
  overrides: Partial<AccessGrantsSource> = {},
): FakeGrants => {
  const asked: string[] = [];

  return {
    asked,
    busy: () => false,
    direct: (_device, subject) => held[subject] ?? [],
    error: undefined,
    load: () => Promise.resolve(),
    loaded: true,
    loading: false,
    setChoice: (device, subject, choice) => {
      asked.push(`${device} ${subject} ${choice}`);

      return Promise.resolve();
    },
    setOn: (device, subject, _role, on) => {
      asked.push(`${device} ${subject} ${on ? 'on' : 'off'}`);

      return Promise.resolve();
    },
    ...overrides,
  };
};

export const fakePeople = (
  items: readonly Account[] = people,
): AccessPeopleSource => ({
  items,
  loaded: true,
  loading: false,
  error: undefined,
  load: () => Promise.resolve(),
});

const printer = (id: string, name: string, model: string): Device => ({
  ...fakeDevice(id, name),
  model,
  actions: ['observe', 'operate', 'configure', 'administer'],
});

/** One page of a fleet of 400. */
export const fakePrinters = (
  overrides: Partial<AccessPrintersSource> = {},
): AccessPrintersSource => ({
  items: [
    printer(PRINTER, 'CoreOne DEMO-01', 'CoreOne'),
    printer('p-mk4s', 'MK4S DEMO-02', 'MK4S'),
    printer('p-xl', 'XL DEMO-03', 'XL'),
  ],
  loaded: true,
  total: 400,
  query: {
    page: 0,
    pageSize: 25,
    filters: {},
    search: '',
    sort: undefined,
    order: undefined,
  },
  setQuery: () => undefined,
  ...overrides,
});
