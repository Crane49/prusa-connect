import { pairingSource } from '../portal-pairing/story-source';
import type { ExtensionReview, UpgradeJournal } from '../../data';
import type { MaintenanceSource } from './types';

export const maintenanceSource = (
  over: Partial<MaintenanceSource> = {},
): MaintenanceSource => ({
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
  upgrade: undefined,
  releases: [],
  releaseError: undefined,
  releaseRetryAt: undefined,
  releaseLoading: false,
  history: [],
  backups: [],
  backupError: undefined,
  backupLoading: false,
  deliveryAvailable: true,
  portal: pairingSource(),
  offlineBundle: undefined,
  offlineUploadProgress: undefined,
  loaded: true,
  loading: false,
  busy: undefined,
  error: undefined,
  refreshReleases: () => Promise.resolve(),
  refreshBackups: () => Promise.resolve(),
  uploadOfflineBundle: () => Promise.resolve(),
  stage: () => Promise.resolve(),
  prepare: () => Promise.resolve(),
  apply: () => Promise.resolve(),
  cancel: () => Promise.resolve(),
  createBackup: () => Promise.resolve(),
  restoreBackup: () => Promise.resolve(),
  deleteBackup: () => Promise.resolve(),
  ...over,
});

/** An offline bundle that preparation rehearsed, ready to apply. */
export const preparedUpgrade = (
  over: Partial<UpgradeJournal> = {},
): UpgradeJournal => ({
  from_version: '1.2.0',
  id: 'offline-1',
  revision: 4,
  schema: 1,
  staged_binary: '/updates/candidate',
  state: 'rehearsed',
  to_version: '1.3.0',
  updated_at: '2026-10-26T12:00:00Z',
  rehearsal: {
    checked_at: '2026-10-26T12:00:00Z',
    duration_ms: 1800,
  },
  ...over,
});

/** Two bundled extensions, one asking for more and one going back a version. */
export const reviewsNeedingApproval: ExtensionReview[] = [
  {
    module: 'github.com/example/fleet-tools',
    version: '2.1.0',
    installed_version: '2.0.0',
    asks: ['read printers', 'send commands to printers', 'read cameras'],
    new_asks: ['send commands to printers', 'read cameras'],
  },
  {
    module: 'github.com/example/spool-log',
    version: '1.4.0',
    installed_version: '1.5.2',
    asks: ['read jobs'],
    downgrade: true,
  },
];
