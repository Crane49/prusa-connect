import type { Maintenance } from '../../data';
import type { PortalPairingSource } from '../portal-pairing/types';

export type MaintenanceSource = MaintenanceFields & {
  /** The Beta Portal pairing, absent when the server cannot deliver online updates. */
  readonly portal: PortalPairingSource | undefined;
};

type MaintenanceFields = Pick<
  Maintenance,
  | 'apply'
  | 'backupError'
  | 'backupLoading'
  | 'backups'
  | 'busy'
  | 'cancel'
  | 'createBackup'
  | 'deleteBackup'
  | 'deliveryAvailable'
  | 'error'
  | 'history'
  | 'loaded'
  | 'loading'
  | 'offlineBundle'
  | 'offlineUploadProgress'
  | 'prepare'
  | 'releaseError'
  | 'releaseLoading'
  | 'releaseRetryAt'
  | 'releases'
  | 'refreshBackups'
  | 'refreshReleases'
  | 'restoreBackup'
  | 'stage'
  | 'upgrade'
  | 'uploadOfflineBundle'
  | 'version'
>;

export interface SettingsMaintenanceScreenProps {
  source: MaintenanceSource;
}

export interface SettingsUpdatesScreenProps extends SettingsMaintenanceScreenProps {
  method?: 'online' | 'offline';
}

export type SettingsBackupsScreenProps = SettingsMaintenanceScreenProps;
