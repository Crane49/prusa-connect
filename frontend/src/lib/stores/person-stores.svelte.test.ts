// What the app shows after one person signs out and another signs in, in the
// same tab and without the page reloading. The server answers each person
// with their own rows, and the stores must show nothing of the first person.
import { flushSync } from 'svelte';
import { describe, expect, it } from 'vitest';

import { Router } from '@prusa/connect-ui/router';
import {
  createModuleLoader,
  defineModule,
  SlotRegistry,
} from '@prusa/connect-ui/slots';

import PlainPage from '../../test/plain-page.svelte';
import { json, stubClient } from '../../test/stub';
import { PATHS, PATTERNS } from '../routes';
import { forgetThePerson, personStores } from './person-stores';
import { forgetEachPerson } from './signed-in.svelte';

type Person = 'ada' | 'grace';

// The rows each person may read. Both people see printer p1, and each one
// has their own history, credential, transfer and waiting device on it.
const rowsOf = (who: Person) => ({
  devices: [
    {
      id: 'p1',
      name: 'Printer 1',
      kind: 'printer',
      labels: {},
      state: { status: 'IDLE' },
    },
  ],
  jobs: [
    {
      id: `p1:${who}`,
      device_id: 'p1',
      display_name: `${who}.bgcode`,
      path: `/usb/${who}.bgcode`,
      progress: 100,
      started: '2026-09-01T08:00:00Z',
      ended: '2026-09-01T09:00:00Z',
      state: 'FINISHED',
      time_remaining_ms: 0,
    },
  ],
  credentials: [
    {
      created_at: '2026-09-01T08:00:00Z',
      expires_at: '2027-09-01T08:00:00Z',
      id: `pk-${who}`,
      name: `${who} laptop`,
      subject: `user:${who}`,
    },
  ],
  transfers: [
    {
      bytes_sent: 512,
      bytes_total: 1024,
      path: `usb/${who}.bgcode`,
      source: `${who}.bgcode`,
      started_at: '2026-09-01T08:00:00Z',
      state: 'sending',
      storage: 'local',
      transfer_id: `t-${who}`,
    },
  ],
  nearby: [
    {
      address: '192.168.1.20',
      name: `${who} desk`,
      port: 80,
      services: ['_prusalink._tcp'],
      tls: false,
      url: 'http://192.168.1.20:80/',
    },
  ],
  visibility: { hidden: who === 'ada', announcement: 'announcing' },
  registrations: [
    {
      code: `code-${who}`,
      fingerprint: `fp-${who}`,
      model: 'MK4',
      firmware: '6.0.0',
      serial: `serial-${who}`,
    },
  ],
});

// A module that adds a page, and remembers who it was admitted for.
const SPOOLS = 'com.github.aux.acme.spools';
const spoolsModule = {
  config: {},
  enabled: true,
  generation: 1,
  kind: 'module',
  name: SPOOLS,
  namespace: SPOOLS,
  required: false,
  runtime: 'asset',
  source: 'file',
  status: {
    attempt: 0,
    conditions: [],
    dropped: 0,
    observed_generation: 1,
    serving: true,
    state: 'ready',
  },
  type: SPOOLS,
  ui: {
    entry_url: `/api/v1/modules/${SPOOLS}/ui/index.js`,
    library: '>=0.0.0',
    contributes: {
      pages: [{ path: 'spools', title: 'Spools', hidden: false }],
    },
  },
  uid: `uid-${SPOOLS}`,
  version: '1',
};

// The app as it is wired: the person's stores, the module loader, the router,
// a session, and the reset that runs when the person changes. The server
// answers whoever `signed.who` is.
const signedIn = () => {
  const signed: { who: Person } = { who: 'ada' };
  const admittedFor: string[] = [];
  const { client } = stubClient({
    'GET /api/v1/resources': json(200, { items: [spoolsModule] }),
    'GET /api/v1/principal': () =>
      json(200, { principal: `user:${signed.who}` }),
    'GET /api/v1/devices': () =>
      json(200, { items: rowsOf(signed.who).devices }),
    'GET /api/v1/devices/p1/jobs/history': () =>
      json(200, { items: rowsOf(signed.who).jobs }),
    'GET /api/v1/jobs': (call) =>
      json(200, {
        items: call.query.includes('phase=running')
          ? []
          : rowsOf(signed.who).jobs,
        cursor: '',
      }),
    'GET /api/v1/auth/credentials': () =>
      json(200, { items: rowsOf(signed.who).credentials }),
    'GET /api/v1/devices/p1/transfers': () =>
      json(200, { items: rowsOf(signed.who).transfers }),
    'GET /api/v1/registrations': () =>
      json(200, { items: rowsOf(signed.who).registrations }),
    'GET /api/v1/discovery/nearby': () =>
      json(200, { items: rowsOf(signed.who).nearby }),
    'GET /api/v1/discovery/visibility': () =>
      json(200, rowsOf(signed.who).visibility),
  });
  const session = $state<{
    status: string;
    principal: { id: string } | undefined;
  }>({ status: 'signed-in', principal: { id: 'user:ada' } });
  const stores = personStores(client, () => session.principal?.id);
  const registry = new SlotRegistry();
  const loader = createModuleLoader({
    client,
    registry,
    fleet: stores.fleet,
    importModule: () =>
      Promise.resolve({
        default: defineModule((host) => {
          admittedFor.push(host.principal.id);
          host.register.page('spools', PlainPage);
        }),
      }),
    storage: null,
    // The module carries no catalogue, so no request leaves the test for one.
    readCatalogue: () => Promise.resolve(undefined),
  });
  const router = new Router({ known: PATTERNS, navigation: null });
  const cleanup = $effect.root(() => {
    forgetEachPerson(session, () => {
      forgetThePerson({ stores, loader, router, home: PATHS.home });
    });
  });
  flushSync();

  return {
    signed,
    session,
    stores,
    loader,
    registry,
    router,
    admittedFor,
    cleanup,
  };
};

// Everything a person reads on the pages this test covers.
const readAll = async (stores: ReturnType<typeof personStores>) => {
  await stores.fleet.load();
  await Promise.all([
    stores.jobs.load(),
    stores.credentials.load(),
    stores.transfers('p1').load(),
    stores.registrations.load(),
    stores.nearby.load(),
    stores.discovery.load(),
  ]);
};

// What the pages would draw, as the names of the rows.
const drawn = (stores: ReturnType<typeof personStores>) => ({
  jobs: stores.jobs.printers.flatMap((row) =>
    (row.jobs ?? []).map((job) => job.id),
  ),
  credentials: stores.credentials.items.map((row) => row.id),
  transfers: stores.transfers('p1').rows.map((row) => row.transferId),
  registrations: stores.registrations.pending.map((row) => row.code),
  nearby: stores.nearby.items.map((row) => row.name),
  hidden: stores.discovery.loaded ? stores.discovery.hidden : undefined,
});

describe('the stores after the person signed in changes', () => {
  it('show none of the first person once another person signs in', async () => {
    const {
      signed,
      session,
      stores,
      loader,
      registry,
      router,
      admittedFor,
      cleanup,
    } = signedIn();
    await readAll(stores);
    await loader.activate();
    router.path = PATHS.settingsAccount;
    expect(registry.pages()).toHaveLength(1);
    expect(drawn(stores)).toEqual({
      jobs: ['p1:ada'],
      credentials: ['pk-ada'],
      transfers: ['t-ada'],
      registrations: ['code-ada'],
      nearby: ['ada desk'],
      hidden: true,
    });
    const heldTransfers = stores.transfers('p1');

    signed.who = 'grace';
    session.principal = { id: 'user:grace' };
    flushSync();

    // Nothing is drawn for Grace before her own reads answer.
    expect(drawn(stores)).toEqual({
      jobs: [],
      credentials: [],
      transfers: [],
      registrations: [],
      nearby: [],
      hidden: undefined,
    });
    expect(stores.jobs.loaded).toBe(false);
    expect(stores.credentials.loaded).toBe(false);
    // A page still holding the old transfers store sees it empty.
    expect(heldTransfers.rows).toEqual([]);
    // The page admitted for Ada is gone, and the app is back home.
    expect(registry.pages()).toEqual([]);
    expect(router.path).toBe(PATHS.home);

    await readAll(stores);
    await loader.activate();
    expect(admittedFor).toEqual(['user:ada', 'user:grace']);
    expect(drawn(stores)).toEqual({
      jobs: ['p1:grace'],
      credentials: ['pk-grace'],
      transfers: ['t-grace'],
      registrations: ['code-grace'],
      nearby: ['grace desk'],
      hidden: false,
    });

    cleanup();
  });

  it('show none of the person who signed out', async () => {
    const { session, stores, loader, registry, router, cleanup } = signedIn();
    await readAll(stores);
    await loader.activate();
    router.path = PATHS.settingsAccount;

    session.status = 'signed-out';
    session.principal = undefined;
    flushSync();

    expect(drawn(stores)).toEqual({
      jobs: [],
      credentials: [],
      transfers: [],
      registrations: [],
      nearby: [],
      hidden: undefined,
    });
    expect(stores.fleet.devices).toEqual([]);
    expect(registry.pages()).toEqual([]);
    expect(router.path).toBe(PATHS.home);

    cleanup();
  });

  it('keep what they read when the same person is read again', async () => {
    const { session, stores, cleanup } = signedIn();
    await readAll(stores);

    // A password change reads the session again and hands over a new object
    // for the same person.
    session.principal = { id: 'user:ada' };
    flushSync();

    expect(drawn(stores)).toEqual({
      jobs: ['p1:ada'],
      credentials: ['pk-ada'],
      transfers: ['t-ada'],
      registrations: ['code-ada'],
      nearby: ['ada desk'],
      hidden: true,
    });

    cleanup();
  });
});
