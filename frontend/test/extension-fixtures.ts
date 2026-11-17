// What the Extensions stories draw: rows, a catalogue that holds literal
// values, and the callbacks that do nothing.
import type { ResourceInstance } from '@prusa/connect-ui/data';

import { extensionRowsOf, type ExtensionRow } from '../src/lib/extensions/rows';
import type {
  InstallOutcome,
  ModuleCatalogue,
  ModuleMarketplace,
  ModuleOffer,
  PendingApproval,
} from '../src/lib/stores/module-catalogue.svelte';

export const offerOf = (over: Partial<ModuleOffer> = {}): ModuleOffer => ({
  module: 'github.com/prusa3d/spool-tracker',
  name: 'spool-tracker',
  title: 'Spool tracker',
  description: 'Counts what each print used and warns before a spool runs out.',
  author: 'Prusa Research',
  version: '1.4.0',
  source: 'https://marketplace.example/spool-tracker/1.4.0.tgz',
  installed: false,
  process: true,
  requests_known: true,
  namespace: 'com.github.prusa3d.spool-tracker',
  origin: 'marketplace',
  kind: 'driver',
  permissions: ['printers.read', 'jobs.read'],
  events: { 'spool.low': 'low' },
  records_mib: 64,
  ...over,
});

export const instanceOf = (
  over: Partial<ResourceInstance> = {},
): ResourceInstance => ({
  config: {},
  enabled: true,
  generation: 1,
  kind: 'driver',
  name: 'spool-tracker',
  namespace: 'com.github.prusa3d.spool-tracker',
  required: false,
  runtime: 'service',
  source: 'overlay',
  status: {
    attempt: 0,
    conditions: [],
    dropped: 0,
    observed_generation: 1,
    serving: true,
    state: 'ready',
  },
  type: 'module',
  uid: 'u1',
  version: '1.4.0',
  module: { asked: {}, title: 'Spool tracker', version: '1.4.0' },
  ...over,
});

export const rowOf = (
  offer: Partial<ModuleOffer> = {},
  instance?: Partial<ResourceInstance>,
): ExtensionRow => {
  const full = offerOf({
    ...(instance === undefined
      ? {}
      : {
          installed: true,
          installed_version: '1.4.0',
          instance: 'driver:spool-tracker',
        }),
    ...offer,
  });
  const [row] = extensionRowsOf(
    [full],
    instance === undefined ? [] : [instanceOf(instance)],
    [],
  );

  if (row === undefined) throw new Error('no row');

  return row;
};

export const MARKETPLACE: ModuleMarketplace = {
  browse_url: 'https://marketplace.example',
  enabled: true,
  community: false,
  reachable: true,
  stale: false,
  url: 'https://marketplace.example/index.json',
  read_at: '2026-10-22T08:30:00Z',
};

/** The marketplace before it has been read once. */
export const MARKETPLACE_UNREAD: ModuleMarketplace = {
  browse_url: MARKETPLACE.browse_url,
  enabled: true,
  community: false,
  reachable: false,
  stale: false,
  url: MARKETPLACE.url,
};

/** A catalogue that holds the values given and starts nothing. */
export const catalogueOf = (
  over: Partial<Record<keyof ModuleCatalogue, unknown>> = {},
): ModuleCatalogue =>
  ({
    acting: undefined,
    switching: false,
    writing: undefined,
    modulesOn: true,
    outcome: undefined,
    pending: undefined,
    marketplace: MARKETPLACE,
    switchError: undefined,
    mayAdministerOrg: true,
    mayConfigure: () => true,
    dismissOutcome: () => undefined,
    setMarketplace: () => Promise.resolve(true),
    setCommunity: () => Promise.resolve(true),
    writeInstance: () => Promise.resolve(true),
    // An install adds nothing and a removal leaves nothing unused, unless a
    // story says otherwise.
    planOf: () => NOTHING_PLANNED,
    readPlan: () => Promise.resolve(NOTHING_PLANNED),
    memoryPool: undefined,
    readMemory: () => Promise.resolve(),
    removalOf: () => NOTHING_LEFT,
    readRemoval: () => Promise.resolve(NOTHING_LEFT),
    ...over,
  }) as unknown as ModuleCatalogue;

const NOTHING_PLANNED = {
  state: 'ready',
  dependencies: [],
  conflicts: [],
} as const;
const NOTHING_LEFT = {
  state: 'ready',
  removal: { required_by: [], unused: [] },
} as const;

export const outcomeOf = (
  over: Partial<InstallOutcome['answer']> = {},
  update = false,
): InstallOutcome => ({
  offer: offerOf(),
  update,
  answer: {
    enabled: true,
    instance: 'driver:spool-tracker',
    kind: 'driver',
    namespace: 'com.github.prusa3d.spool-tracker',
    restart: false,
    root: '/var/lib/prusa-connect/modules',
    running: true,
    runtime: 'service',
    state: 'ready',
    type: 'module',
    verified: 1,
    version: '1.4.0',
    ...over,
  },
});

export const pendingOf = (
  cause: PendingApproval['cause'],
  found: readonly string[] = [],
): PendingApproval => ({
  cause,
  offer: offerOf({ requests_known: false }),
  version: '1.4.0',
  requests: {
    permissions: ['printers.read', 'jobs.read'],
    events: { 'spool.low': 'low' },
    records_mib: 64,
  },
  found,
  namespace: 'com.github.prusa3d.spool-tracker',
});

export const noop = (): void => undefined;

/** The instance override of a module the server holds in this state. */
export const inState = (
  state: ResourceInstance['status']['state'],
): Partial<ResourceInstance> => ({
  enabled: state !== 'disabled',
  status: {
    attempt: 0,
    conditions: [],
    dropped: 0,
    observed_generation: 1,
    serving: state === 'ready',
    state,
  },
});
