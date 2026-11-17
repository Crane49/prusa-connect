import { flushSync } from 'svelte';
import { expect, test, vi } from 'vitest';

import { Maintenance, retryAfterOf } from './maintenance.svelte';
import { upgradeDeliveryOf } from './services/maintenance';
import type {
  MaintenanceClient,
  OfflineBundleReceipt,
  UpgradeDeliveryService,
} from './services/maintenance';

const client = {} as MaintenanceClient;

test('keeps maintenance usable when the online catalogue fails', async () => {
  const catalogueFailure = Object.assign(new Error('catalogue unavailable'), {
    code: 'core.internal/UNEXPECTED',
    status: 500,
    traceId: 'catalogue-trace',
  });
  const loadingClient = {
    getServerVersion: vi.fn(() =>
      Promise.resolve({
        build_profile: 'server',
        build_variant: 'full',
        capabilities: [],
        contract: '1.0.0',
        goarch: 'arm64',
        goos: 'linux',
        host_schema_compat: 10,
        host_schema_version: 12,
        managed_externally: false,
        version: '1.2.0',
      }),
    ),
    getServerUpgrade: vi.fn(() =>
      Promise.reject(Object.assign(new Error('none'), { status: 404 })),
    ),
    listServerUpgradeReleases: vi.fn(() => Promise.reject(catalogueFailure)),
    listServerUpgradeHistory: vi.fn(() => Promise.resolve([])),
    listServerBackups: vi.fn(() => Promise.resolve([])),
  } as unknown as MaintenanceClient;
  const delivery = {
    getPortalPairingStatus: vi.fn(() =>
      Promise.resolve({ state: 'unpaired' as const }),
    ),
    startPortalPairing: vi.fn(),
    refreshPortalPairing: vi.fn(),
    disconnectPortal: vi.fn(),
    uploadOfflineBundle: vi.fn(),
  } as unknown as UpgradeDeliveryService;
  const maintenance = new Maintenance(loadingClient, delivery);

  await maintenance.load();

  expect(maintenance.version?.version).toBe('1.2.0');
  expect(maintenance.error).toBeUndefined();
  expect(maintenance.releases).toEqual([]);
  expect(maintenance.releaseError).toMatchObject({
    code: 'core.internal/UNEXPECTED',
    status: 500,
    traceId: 'catalogue-trace',
  });
});

test('keeps updates usable when the backup listing fails', async () => {
  const loadingClient = {
    getServerVersion: vi.fn(() =>
      Promise.resolve({
        build_profile: 'server',
        build_variant: 'full',
        capabilities: [],
        contract: '1.0.0',
        goarch: 'arm64',
        goos: 'linux',
        host_schema_compat: 10,
        host_schema_version: 12,
        managed_externally: false,
        version: '1.2.0',
      }),
    ),
    getServerUpgrade: vi.fn(() =>
      Promise.reject(Object.assign(new Error('none'), { status: 404 })),
    ),
    listServerUpgradeReleases: vi.fn(() => Promise.resolve([])),
    listServerUpgradeHistory: vi.fn(() => Promise.resolve([])),
    listServerBackups: vi.fn(() =>
      Promise.reject(
        Object.assign(new Error('backups unavailable'), {
          code: 'core.internal/UNEXPECTED',
          status: 500,
          traceId: 'backup-trace',
        }),
      ),
    ),
  } as unknown as MaintenanceClient;
  const maintenance = new Maintenance(loadingClient);

  await maintenance.load();

  expect(maintenance.version?.version).toBe('1.2.0');
  expect(maintenance.error).toBeUndefined();
  expect(maintenance.releaseError).toBeUndefined();
  expect(maintenance.backupError).toMatchObject({
    code: 'core.internal/UNEXPECTED',
    traceId: 'backup-trace',
  });
});

test('reads the version alone where the server prepares no upgrade', async () => {
  const upgradeReads = {
    getServerUpgrade: vi.fn(),
    listServerUpgradeReleases: vi.fn(),
    listServerUpgradeHistory: vi.fn(),
    listServerBackups: vi.fn(),
  };
  const loadingClient = {
    getServerVersion: vi.fn(() =>
      Promise.resolve({
        build_profile: 'printer',
        build_variant: 'full',
        capabilities: [],
        contract: '1.0.0',
        goarch: 'arm64',
        goos: 'linux',
        host_schema_compat: 10,
        host_schema_version: 12,
        managed_externally: true,
        version: '1.2.0',
      }),
    ),
    ...upgradeReads,
  } as unknown as MaintenanceClient;
  const pairingStatus = vi.fn();
  const delivery = {
    getPortalPairingStatus: pairingStatus,
    startPortalPairing: vi.fn(),
    refreshPortalPairing: vi.fn(),
    disconnectPortal: vi.fn(),
    uploadOfflineBundle: vi.fn(),
  } as unknown as UpgradeDeliveryService;
  const maintenance = new Maintenance(loadingClient, delivery, {
    upgrades: () => false,
  });

  await maintenance.load();

  expect(maintenance.loaded).toBe(true);
  expect(maintenance.version?.version).toBe('1.2.0');
  expect(maintenance.error).toBeUndefined();
  for (const read of Object.values(upgradeReads)) {
    expect(read).not.toHaveBeenCalled();
  }
  expect(pairingStatus).not.toHaveBeenCalled();
});

// The restart panel reads a refusal of this read as no permission to restart.
test('reports a refused version read on a server that prepares no upgrade', async () => {
  let refuse: ((reason: unknown) => void) | undefined;
  const refusing = {
    getServerVersion: vi.fn(
      () =>
        new Promise((_, reject) => {
          refuse = reject;
        }),
    ),
  } as unknown as MaintenanceClient;
  const maintenance = new Maintenance(refusing, undefined, {
    upgrades: () => false,
  });

  const loading = maintenance.load();
  expect(maintenance.loading).toBe(true);
  expect(maintenance.loaded).toBe(false);

  refuse?.(Object.assign(new Error('refused'), { status: 403 }));
  await loading;

  expect(maintenance.loading).toBe(false);
  expect(maintenance.loaded).toBe(true);
  expect(maintenance.error?.status).toBe(403);
  expect(maintenance.version).toBeUndefined();
});

test('publishes releases while backups are still loading', async () => {
  let finishBackups: ((value: readonly []) => void) | undefined;
  const backups = new Promise<readonly []>((resolve) => {
    finishBackups = resolve;
  });
  const loadingClient = {
    getServerVersion: vi.fn(() =>
      Promise.resolve({
        build_profile: 'server',
        build_variant: 'full',
        capabilities: [],
        contract: '1.0.0',
        goarch: 'arm64',
        goos: 'linux',
        host_schema_compat: 10,
        host_schema_version: 12,
        managed_externally: false,
        version: '1.2.0',
      }),
    ),
    getServerUpgrade: vi.fn(() =>
      Promise.reject(Object.assign(new Error('none'), { status: 404 })),
    ),
    listServerUpgradeReleases: vi.fn(() =>
      Promise.resolve([
        {
          binary: { name: 'core', size: 1, url: '/core' },
          name: 'Prusa Connect 1.3.0',
          prerelease: false,
          published_at: '2026-10-27T12:00:00Z',
          signature: { name: 'signature', size: 1, url: '/signature' },
          sums: { name: 'sums', size: 1, url: '/sums' },
          version: '1.3.0',
        },
      ]),
    ),
    listServerUpgradeHistory: vi.fn(() => Promise.resolve([])),
    listServerBackups: vi.fn(() => backups),
  } as unknown as MaintenanceClient;
  const maintenance = new Maintenance(loadingClient);

  const loaded = maintenance.load();
  await vi.waitFor(() => {
    expect(maintenance.releases).toHaveLength(1);
  });

  expect(maintenance.releaseLoading).toBe(false);
  expect(maintenance.backupLoading).toBe(true);
  expect(maintenance.loaded).toBe(true);

  expect(finishBackups).toBeDefined();
  finishBackups?.([]);
  await loaded;
  expect(maintenance.backupLoading).toBe(false);
});

test('keeps only the safe portal pairing status', async () => {
  const startPortalPairing = vi.fn(() =>
    Promise.resolve({
      state: 'pending' as const,
      user_code: 'PRUSA-7K2M',
      verification_uri: 'https://example.com/activate',
    }),
  );
  const delivery = {
    getPortalPairingStatus: vi.fn(),
    startPortalPairing,
    refreshPortalPairing: vi.fn(),
    disconnectPortal: vi.fn(),
    uploadOfflineBundle: vi.fn(),
  } as unknown as UpgradeDeliveryService;
  const maintenance = new Maintenance(client, delivery);

  await maintenance.portal?.start();

  expect(startPortalPairing).toHaveBeenCalledOnce();
  expect(maintenance.portal?.status).toEqual({
    state: 'pending',
    user_code: 'PRUSA-7K2M',
    verification_uri: 'https://example.com/activate',
  });
});

test('refreshes online releases when portal pairing completes', async () => {
  const release = {
    version: '1.3.0',
    name: 'Prusa Connect 1.3.0',
  };
  const pairedClient = {
    listServerUpgradeReleases: vi.fn(() => Promise.resolve([release])),
  } as unknown as MaintenanceClient;
  const delivery = {
    getPortalPairingStatus: vi.fn(),
    startPortalPairing: vi.fn(() =>
      Promise.resolve({ state: 'pending' as const, user_code: 'WDJB-MJHT' }),
    ),
    refreshPortalPairing: vi.fn(() =>
      Promise.resolve({ state: 'paired' as const }),
    ),
    disconnectPortal: vi.fn(),
    uploadOfflineBundle: vi.fn(),
  } as unknown as UpgradeDeliveryService;
  const maintenance = new Maintenance(pairedClient, delivery);

  await maintenance.portal?.start();
  await maintenance.portal?.refresh();

  expect(maintenance.portal?.status).toEqual({ state: 'paired' });
  await vi.waitFor(() => {
    expect(maintenance.releases).toEqual([release]);
  });
  expect(pairedClient.listServerUpgradeReleases).toHaveBeenCalledOnce();
});

test('records offline bundle progress and the inspected receipt', async () => {
  const receipt = {
    bundle: {
      name: 'Update 1.3.0',
      version: '1.3.0',
      artifacts: [],
      impacts: [],
    },
    upgrade: {
      id: 'offline-1',
      revision: 1,
      schema: 1,
      staged_binary: '/updates/candidate',
      state: 'staged',
      from_version: '1.2.0',
      to_version: '1.3.0',
      updated_at: '2026-10-26T12:00:00Z',
    },
  } as OfflineBundleReceipt;
  const uploadOfflineBundle = vi.fn(
    (_bundle: File, onProgress: (sent: number, total: number) => void) => {
      onProgress(5, 10);
      return Promise.resolve(receipt);
    },
  );
  const delivery = {
    getPortalPairingStatus: vi.fn(),
    startPortalPairing: vi.fn(),
    refreshPortalPairing: vi.fn(),
    disconnectPortal: vi.fn(),
    uploadOfflineBundle,
  } as unknown as UpgradeDeliveryService;
  const maintenance = new Maintenance(client, delivery);
  const file = new File(['signed'], 'update.bundle');

  await maintenance.uploadOfflineBundle(file);

  expect(uploadOfflineBundle).toHaveBeenCalledWith(file, expect.any(Function));
  expect(maintenance.offlineUploadProgress).toBe(100);
  expect(maintenance.offlineBundle).toBe(receipt.bundle);
  expect(maintenance.upgrade).toBe(receipt.upgrade);
});

test('uses generated pairing and offline bundle operations', async () => {
  const stageServerUpgrade = vi.fn(() =>
    Promise.resolve({
      id: 'offline-generated',
      revision: 1,
      schema: 1,
      staged_binary: '/updates/candidate',
      state: 'verified',
      from_version: '1.2.0',
      to_version: '1.3.0',
      updated_at: '2026-10-26T12:00:00Z',
      verified: {
        name: 'prusa-connect',
        sha256: 'a'.repeat(64),
        size: 6,
      },
      extensions: [],
    }),
  );
  const generated = {
    stageServerUpgradeBundle: stageServerUpgrade,
    getBetaPortalPairingStatus: vi.fn(() =>
      Promise.resolve({
        state: 'paired',
        identity: { name: 'updates@example.com' },
      }),
    ),
    startBetaPortalPairing: vi.fn(() =>
      Promise.resolve({
        state: 'pending',
        user_code: 'PRUSA-7K2M',
        verification_uri: 'https://example.com/activate',
        device_code: 'server-only',
      }),
    ),
    refreshBetaPortalPairing: vi.fn(() => Promise.resolve({ state: 'paired' })),
    disconnectBetaPortal: vi.fn(() => Promise.resolve()),
  } as unknown as MaintenanceClient;
  const delivery = upgradeDeliveryOf(generated);
  expect(delivery).toBeDefined();

  const pairing = await delivery!.startPortalPairing();
  expect(pairing).toEqual({
    state: 'pending',
    user_code: 'PRUSA-7K2M',
    verification_uri: 'https://example.com/activate',
  });
  expect(JSON.stringify(pairing)).not.toContain('server-only');

  const bundle = new File(['signed'], 'update.bundle');
  const onProgress = vi.fn();
  const receipt = await delivery!.uploadOfflineBundle(bundle, onProgress);

  // The file itself is the body, so the browser streams it off the disk.
  expect(stageServerUpgrade).toHaveBeenCalledWith({
    body: bundle,
    onProgress,
  });
  expect(receipt.bundle.version).toBe('1.3.0');
  expect(receipt.bundle.artifacts).toEqual([
    {
      kind: 'core',
      name: 'prusa-connect',
      version: '1.3.0',
      size: 6,
    },
  ]);
});

test('reads the upgrade again when applying is refused', async () => {
  const reviewed = {
    from_version: '1.2.0',
    id: 'offline-1',
    revision: 5,
    schema: 1,
    staged_binary: '/updates/candidate',
    state: 'rehearsed',
    to_version: '1.3.0',
    updated_at: '2026-10-26T12:00:00Z',
    extension_reviews: [
      { module: 'fleet-tools', version: '2.1.0', new_asks: ['read cameras'] },
    ],
  };
  const applyingClient = {
    applyServerUpgrade: vi.fn(() =>
      Promise.reject(
        Object.assign(new Error('approve the bundled extensions to apply'), {
          code: 'core.validation/REQUEST_INVALID',
          status: 400,
        }),
      ),
    ),
    getServerUpgrade: vi.fn(() => Promise.resolve(reviewed)),
  } as unknown as MaintenanceClient;
  const maintenance = new Maintenance(applyingClient, undefined);

  await maintenance.apply({ schedule: 'now', postgres_backup_ack: true });

  expect(maintenance.error?.code).toBe('core.validation/REQUEST_INVALID');
  expect(applyingClient.getServerUpgrade).toHaveBeenCalledOnce();
  expect(maintenance.upgrade?.extension_reviews).toHaveLength(1);
  expect(maintenance.busy).toBeUndefined();
});

test('reads the upgrade again when staging or preparing is refused', async () => {
  const failed = {
    failure: 'staged wire contract is 1.1.0, server contract is 1.0.0',
    from_version: '1.2.0',
    id: 'online-1',
    revision: 2,
    schema: 1,
    staged_binary: '/updates/candidate',
    state: 'failed-before-swap',
    to_version: '1.3.0',
    updated_at: '2026-11-07T12:00:00Z',
  };
  const refusal = () =>
    Promise.reject(
      Object.assign(new Error(failed.failure), {
        code: 'core.resource/UPGRADE_REFUSED',
        status: 422,
      }),
    );
  const refusingClient = {
    getServerUpgrade: vi.fn(() => Promise.resolve(failed)),
    prepareServerUpgrade: vi.fn(refusal),
    stageServerUpgrade: vi.fn(refusal),
  } as unknown as MaintenanceClient;

  for (const act of [
    (maintenance: Maintenance) =>
      maintenance.stage({ version: '1.3.0' } as never),
    (maintenance: Maintenance) => maintenance.prepare(),
  ]) {
    refusingClient.getServerUpgrade = vi.fn(() => Promise.resolve(failed));
    const maintenance = new Maintenance(refusingClient, undefined);

    await act(maintenance);

    expect(maintenance.error?.code).toBe('core.resource/UPGRADE_REFUSED');
    expect(refusingClient.getServerUpgrade).toHaveBeenCalledOnce();
    expect(maintenance.upgrade?.state).toBe('failed-before-swap');
    expect(maintenance.upgrade?.failure).toBe(failed.failure);
  }
});

test('waits as long as the update server asks before listing releases again', async () => {
  let now = 1_000_000;
  const limited = Object.assign(new Error('wait'), {
    code: 'core.resource/UPGRADE_SERVER_LIMITED',
    status: 429,
    params: { retry_after_seconds: 120 },
  });
  const listServerUpgradeReleases = vi
    .fn()
    .mockRejectedValueOnce(limited)
    .mockResolvedValueOnce([]);
  const listingClient = {
    listServerUpgradeReleases,
  } as unknown as MaintenanceClient;
  const maintenance = new Maintenance(listingClient, undefined, {
    now: () => now,
  });

  await maintenance.refreshReleases();
  expect(maintenance.releaseError?.code).toBe(
    'core.resource/UPGRADE_SERVER_LIMITED',
  );
  expect(maintenance.releaseRetryAt).toBe(1_000_000 + 120_000);

  now += 119_000;
  await maintenance.refreshReleases();
  expect(listServerUpgradeReleases).toHaveBeenCalledOnce();

  now += 1_000;
  await maintenance.refreshReleases();
  expect(listServerUpgradeReleases).toHaveBeenCalledTimes(2);
  expect(maintenance.releaseError).toBeUndefined();
  expect(maintenance.releaseRetryAt).toBeUndefined();
});

test('reads the wait only from a refusal for asking too often', () => {
  const fault = (code: string, params: Record<string, string | number>) => ({
    code,
    status: 429,
    params,
    message: undefined,
    traceId: undefined,
  });

  expect(
    retryAfterOf(
      fault('core.resource/UPGRADE_SERVER_LIMITED', {
        retry_after_seconds: 42,
      }),
    ),
  ).toBe(42);
  expect(
    retryAfterOf(
      fault('core.resource/UPGRADE_SERVER_LIMITED', {
        retry_after_seconds: '7',
      }),
    ),
  ).toBe(7);
  expect(retryAfterOf(fault('core.resource/UPGRADE_SERVER_LIMITED', {}))).toBe(
    1,
  );
  expect(
    retryAfterOf(fault('core.internal/UNEXPECTED', { retry_after_seconds: 9 })),
  ).toBeUndefined();
  expect(retryAfterOf(undefined)).toBeUndefined();
});

test('shows a pairing the portal revoked while the releases were read', async () => {
  const listingClient = {
    getServerVersion: vi.fn(() => Promise.resolve({ version: '1.2.0' })),
    getServerUpgrade: vi.fn(() =>
      Promise.reject(Object.assign(new Error('none'), { status: 404 })),
    ),
    listServerUpgradeHistory: vi.fn(() => Promise.resolve([])),
    listServerBackups: vi.fn(() => Promise.resolve([])),
    listServerUpgradeReleases: vi.fn(() =>
      Promise.reject(
        Object.assign(new Error('the portal refused the token'), {
          code: 'core.internal/UNEXPECTED',
          status: 502,
        }),
      ),
    ),
  } as unknown as MaintenanceClient;
  const getPortalPairingStatus = vi
    .fn()
    .mockResolvedValueOnce({ state: 'paired' })
    .mockResolvedValue({ state: 'revoked' });
  const delivery = {
    getPortalPairingStatus,
    startPortalPairing: vi.fn(),
    refreshPortalPairing: vi.fn(),
    disconnectPortal: vi.fn(),
    uploadOfflineBundle: vi.fn(),
  } as unknown as UpgradeDeliveryService;
  const maintenance = new Maintenance(listingClient, delivery);

  await maintenance.load();

  expect(getPortalPairingStatus).toHaveBeenCalledTimes(2);
  expect(maintenance.portal?.phase).toBe('revoked');
});

test('does not read the pairing again when the releases arrive', async () => {
  const listingClient = {
    getServerVersion: vi.fn(() => Promise.resolve({ version: '1.2.0' })),
    getServerUpgrade: vi.fn(() =>
      Promise.reject(Object.assign(new Error('none'), { status: 404 })),
    ),
    listServerUpgradeHistory: vi.fn(() => Promise.resolve([])),
    listServerBackups: vi.fn(() => Promise.resolve([])),
    listServerUpgradeReleases: vi.fn(() => Promise.resolve([])),
  } as unknown as MaintenanceClient;
  const getPortalPairingStatus = vi.fn(() =>
    Promise.resolve({ state: 'paired' as const }),
  );
  const delivery = {
    getPortalPairingStatus,
    startPortalPairing: vi.fn(),
    refreshPortalPairing: vi.fn(),
    disconnectPortal: vi.fn(),
    uploadOfflineBundle: vi.fn(),
  } as unknown as UpgradeDeliveryService;
  const maintenance = new Maintenance(listingClient, delivery);

  await maintenance.load();

  expect(getPortalPairingStatus).toHaveBeenCalledOnce();
  expect(maintenance.portal?.phase).toBe('paired');
});

test('keeps the version on a server that prepares no upgrade', async () => {
  const notFound = () =>
    Promise.reject(Object.assign(new Error('not served'), { status: 404 }));
  const switchedOff = {
    getServerVersion: vi.fn(() =>
      Promise.resolve({
        build_profile: 'server',
        build_variant: 'full',
        capabilities: [],
        contract: '1.0.0',
        goarch: 'arm64',
        goos: 'linux',
        host_schema_compat: 10,
        host_schema_version: 12,
        managed_externally: false,
        version: '1.2.0',
      }),
    ),
    getServerUpgrade: vi.fn(notFound),
    listServerUpgradeHistory: vi.fn(notFound),
    listServerUpgradeReleases: vi.fn(notFound),
    listServerBackups: vi.fn(notFound),
  } as unknown as MaintenanceClient;
  const delivery = {
    getPortalPairingStatus: vi.fn(notFound),
  } as unknown as UpgradeDeliveryService;
  const maintenance = new Maintenance(switchedOff, delivery);

  await maintenance.load();

  expect(maintenance.loaded).toBe(true);
  expect(maintenance.error).toBeUndefined();
  expect(maintenance.version?.version).toBe('1.2.0');
  expect(maintenance.history).toEqual([]);
});

// The Server room loads this store from an effect. A new read of the settings
// replaces them, and the load must not tie the effect to them, or every read
// would load the whole room again.
test('a load does not tie the effect that calls it to the settings', async () => {
  const getServerVersion = vi.fn(() => Promise.resolve({ version: '1.2.0' }));
  const settings = $state({ upgrades: false });
  const maintenance = new Maintenance(
    { getServerVersion } as unknown as MaintenanceClient,
    undefined,
    { upgrades: () => settings.upgrades },
  );

  let runs = 0;
  const cleanup = $effect.root(() => {
    $effect(() => {
      runs += 1;
      void maintenance.load();
    });
  });
  flushSync();
  await vi.waitFor(() => {
    expect(maintenance.loaded).toBe(true);
  });

  settings.upgrades = false;
  settings.upgrades = true;
  settings.upgrades = false;
  flushSync();

  expect(runs).toBe(1);
  expect(getServerVersion).toHaveBeenCalledTimes(1);
  cleanup();
});
