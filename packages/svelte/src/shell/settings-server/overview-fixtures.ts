// The stores the Overview tab reads, as a story and a test hand them over.
import type { BackupManifest, UpgradeRelease } from '../../data';
import type { ServerOverviewProps } from './types';

export const overviewOf = (
  over: Partial<ServerOverviewProps> = {},
): ServerOverviewProps => ({
  health: {
    readiness: {
      app: 'served',
      blocked: null,
      declared: 11,
      instances: [],
      ready: true,
      serving: 8,
    },
    connectors: [
      {
        connector: 'xbuddy',
        counts: { connected: 2, disconnected: 1 },
        degraded: [],
        degraded_truncated: 0,
        status: 'degraded',
        total: 3,
      },
    ],
    readinessError: undefined,
    connectorsError: undefined,
    loaded: true,
  },
  maintenance: {
    version: {
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
    },
    releases: [{ version: '1.3.0' } as UpgradeRelease],
    releaseError: undefined,
    releaseLoading: false,
    upgrade: undefined,
    backups: [
      { id: 'b1', created_at: '2026-10-27T08:00:00Z' } as BackupManifest,
    ],
    loaded: true,
  },
  network: {
    announcement: 'announcing',
    described: {
      hidden: false,
      announcement: 'announcing',
      host: 'workshop-server.local',
      port: 8080,
      bridged: false,
    },
    loaded: true,
    error: undefined,
  },
  healthHref: '/settings/server/health',
  updatesHref: '/settings/server/updates',
  backupsHref: '/settings/server/backups',
  networkHref: '/settings/server/network',
  ...over,
});
