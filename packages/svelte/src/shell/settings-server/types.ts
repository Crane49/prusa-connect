import type {
  ServerHealth,
  ServerRestartPhase,
  ServerRestartRefusal,
} from '../../data';
import type { MaintenanceSource } from '../settings-maintenance';
import type { VisibilitySource } from '../settings-network';

/** What the Server room reads of the health store. */
export type HealthSource = Pick<
  ServerHealth,
  'readiness' | 'connectors' | 'readinessError' | 'connectorsError' | 'loaded'
>;

export interface SettingsServerScreenProps {
  source: HealthSource;
}

export type ServerSettingsTab =
  'overview' | 'health' | 'updates' | 'backups' | 'network';

/** What the Overview tab reads of the stores behind the other tabs. */
export interface ServerOverviewProps {
  health: HealthSource;
  maintenance: Pick<
    MaintenanceSource,
    | 'version'
    | 'releases'
    | 'releaseError'
    | 'releaseLoading'
    | 'upgrade'
    | 'backups'
    | 'loaded'
  >;
  network: Pick<
    VisibilitySource,
    'announcement' | 'described' | 'loaded' | 'error'
  >;
  healthHref: string;
  /**
   * Absent where core.upgrade.enabled is off: the version card then names the
   * version and is not a link.
   */
  updatesHref?: string | undefined;
  /** Absent where core.upgrade.enabled is off, and the backup card is not drawn. */
  backupsHref?: string | undefined;
  networkHref: string;
}

export interface ServerRestartPanelProps {
  /** The server runs in safe mode now. */
  safeMode: boolean;
  /** Whether the person may restart the server; undefined until known. */
  allowed: boolean | undefined;
  phase: ServerRestartPhase;
  /** Why the server refused the request, when it did. */
  refusal?: ServerRestartRefusal | undefined;
  /** What the failed request threw; read in the `failed` phase. */
  error?: unknown;
  /** What the failed read of the permission threw, when it did; undefined otherwise. */
  readError?: unknown;
  /** Reads the permission again; offered next to a failed read. */
  onRetryRead?: () => void;
  /** Whether the safe-mode confirmation is open. Bindable. */
  confirming?: boolean;
  onRestart: (mode: 'normal' | 'safe') => void;
}
