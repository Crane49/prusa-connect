// Stateless requests for the Updates & Backups room.
import type { PrusaConnect, components } from '@prusa/connect';

export type MaintenanceClient = Pick<
  PrusaConnect,
  | 'applyServerUpgrade'
  | 'cancelServerUpgrade'
  | 'createServerBackup'
  | 'deleteServerBackup'
  | 'getServerUpgrade'
  | 'getServerVersion'
  | 'listServerBackups'
  | 'listServerUpgradeHistory'
  | 'listServerUpgradeReleases'
  | 'prepareServerUpgrade'
  | 'restoreServerBackup'
  | 'stageServerUpgrade'
  | 'stageServerUpgradeBundle'
>;

export type ServerVersion = components['schemas']['ServerVersion'];
export type UpgradeJournal = components['schemas']['Journal'];
export type UpgradeRelease = components['schemas']['Release'];
export type BackupManifest = components['schemas']['BackupManifest'];
export type UpgradeSchedule = components['schemas']['ApplyInputBody'];
export type ExtensionReview = components['schemas']['ExtensionReview'];

/**
 * Whether applying this extension needs the operator's approval: it asks for
 * something the installed release did not, or it is older than that release.
 * The server refuses to apply such a bundle without `approve_extensions`.
 */
export const reviewNeedsApproval = (review: ExtensionReview): boolean =>
  (review.new_asks ?? []).length > 0 || review.downgrade === true;

/** The reviews of a prepared upgrade that need the operator's approval. */
export const reviewsNeedingApproval = (
  upgrade: UpgradeJournal | undefined,
): readonly ExtensionReview[] =>
  (upgrade?.extension_reviews ?? []).filter(reviewNeedsApproval);

/**
 * The only portal pairing fields the browser may receive. The device code,
 * access token and signing keys stay in the core.
 */
export type PortalPairingStatus =
  components['schemas']['BetaPortalPairingStatus'];
export type PortalConnectionState = PortalPairingStatus['state'];
export type PortalPairingStep = NonNullable<PortalPairingStatus['step']>;
export type PortalIdentity = components['schemas']['Identity'];

export interface UpdateArtifact {
  readonly kind: 'core' | 'extension';
  readonly name: string;
  readonly version: string;
  readonly size: number;
}

export interface UpdateCompatibilityImpact {
  readonly extension: string;
  readonly outcome: 'compatible' | 'disabled' | 'replaced' | 'required';
  readonly reason?: string;
}

export interface OfflineUpdateBundle {
  readonly name: string;
  readonly version: string;
  readonly artifacts: readonly UpdateArtifact[];
  readonly impacts: readonly UpdateCompatibilityImpact[];
}

export interface OfflineBundleReceipt {
  readonly bundle: OfflineUpdateBundle;
  readonly upgrade: UpgradeJournal;
}

export interface UpgradeDeliveryService {
  getPortalPairingStatus(): Promise<PortalPairingStatus>;
  startPortalPairing(): Promise<PortalPairingStatus>;
  refreshPortalPairing(): Promise<PortalPairingStatus>;
  disconnectPortal(): Promise<PortalPairingStatus>;
  uploadOfflineBundle(
    bundle: File,
    onProgress: (sent: number, total: number) => void,
  ): Promise<OfflineBundleReceipt>;
}

/**
 * Generated operation names are deliberately confined here. Regeneration may
 * replace this interface and the four calls in `upgradeDeliveryOf` without
 * changing the store or the screens.
 */
interface PortalPairingClient {
  getBetaPortalPairingStatus(): Promise<PortalPairingStatus>;
  startBetaPortalPairing(): Promise<PortalPairingStatus>;
  refreshBetaPortalPairing(): Promise<PortalPairingStatus>;
  disconnectBetaPortal(): Promise<void>;
}

type UpgradeDeliveryClient = MaintenanceClient & PortalPairingClient;

const receiptOf = (
  file: File,
  upgrade: UpgradeJournal,
): OfflineBundleReceipt => ({
  bundle: {
    name: file.name,
    version: upgrade.to_version,
    artifacts: [
      ...(upgrade.verified === undefined
        ? []
        : [
            {
              kind: 'core' as const,
              name: upgrade.verified.name,
              version: upgrade.to_version,
              size: upgrade.verified.size,
            },
          ]),
      ...(upgrade.extensions ?? []).map((extension) => ({
        kind: 'extension' as const,
        name: extension.name,
        version: extension.version,
        size: extension.size,
      })),
    ],
    impacts: (upgrade.extensions ?? []).map((extension) => ({
      extension: `${extension.kind}:${extension.name}`,
      outcome: 'compatible' as const,
    })),
  },
  upgrade,
});

// Keeps the documented fields alone, so an unexpected one the core may add
// later never reaches a screen by accident.
const safePairingStatus = (
  answer: PortalPairingStatus,
): PortalPairingStatus => {
  const kept: PortalPairingStatus = { state: answer.state };
  if (answer.step !== undefined) kept.step = answer.step;
  if (answer.user_code !== undefined) kept.user_code = answer.user_code;
  if (answer.verification_uri !== undefined)
    kept.verification_uri = answer.verification_uri;
  if (answer.verification_uri_complete !== undefined)
    kept.verification_uri_complete = answer.verification_uri_complete;
  if (answer.expires_at !== undefined) kept.expires_at = answer.expires_at;
  if (answer.interval_ms !== undefined) kept.interval_ms = answer.interval_ms;
  if (answer.identity !== undefined) kept.identity = answer.identity;
  if (answer.product !== undefined) kept.product = answer.product;
  if (answer.device !== undefined) kept.device = answer.device;

  return kept;
};

/** Builds delivery from generated operations and leaves older servers unsupported. */
export const upgradeDeliveryOf = (
  client: MaintenanceClient,
): UpgradeDeliveryService | undefined => {
  // The SDK generated by the backend branch gains these methods. This one
  // structural adapter is the only cast needed before that package is rebuilt.
  const candidate = client as Partial<UpgradeDeliveryClient>;
  if (
    typeof candidate.getBetaPortalPairingStatus !== 'function' ||
    typeof candidate.startBetaPortalPairing !== 'function' ||
    typeof candidate.refreshBetaPortalPairing !== 'function' ||
    typeof candidate.disconnectBetaPortal !== 'function'
  ) {
    return undefined;
  }
  const delivery = candidate as UpgradeDeliveryClient;

  return {
    getPortalPairingStatus: async () =>
      safePairingStatus(await delivery.getBetaPortalPairingStatus()),
    startPortalPairing: async () =>
      safePairingStatus(await delivery.startBetaPortalPairing()),
    refreshPortalPairing: async () =>
      safePairingStatus(await delivery.refreshBetaPortalPairing()),
    disconnectPortal: async () => {
      await delivery.disconnectBetaPortal();

      return { state: 'unpaired' };
    },
    // The file goes up as the request body, read off the disk by the
    // browser as it is sent, so a large bundle costs the page no memory.
    uploadOfflineBundle: async (bundle, onProgress) => {
      onProgress(0, bundle.size);
      const upgrade = await delivery.stageServerUpgradeBundle({
        body: bundle,
        onProgress,
      });
      onProgress(bundle.size, bundle.size);

      return receiptOf(bundle, upgrade);
    },
  };
};

export interface MaintenanceSnapshot {
  readonly version: ServerVersion;
  readonly upgrade: UpgradeJournal | undefined;
  readonly releases: readonly UpgradeRelease[];
  readonly releaseError?: unknown;
  readonly history: readonly UpgradeJournal[];
  readonly backups: readonly BackupManifest[];
  readonly backupError?: unknown;
}

export type MaintenanceCoreSnapshot = Pick<
  MaintenanceSnapshot,
  'version' | 'upgrade' | 'history'
>;

const isNotFound = (thrown: unknown): boolean =>
  typeof thrown === 'object' &&
  thrown !== null &&
  (thrown as { status?: unknown }).status === 404;

/** No transaction is represented by the operation's documented 404. */
export const readCurrentUpgrade = async (
  client: MaintenanceClient,
): Promise<UpgradeJournal | undefined> => {
  try {
    return await client.getServerUpgrade();
  } catch (thrown) {
    if (isNotFound(thrown)) return undefined;
    throw thrown;
  }
};

export const readMaintenance = async (
  client: MaintenanceClient,
): Promise<MaintenanceSnapshot> => {
  const [core, releaseResult, backupResult] = await Promise.all([
    readMaintenanceCore(client),
    readReleases(client).then(
      (releases) => ({ releases }),
      (releaseError: unknown) => ({ releaseError }),
    ),
    readBackups(client).then(
      (backups) => ({ backups }),
      (backupError: unknown) => ({ backupError }),
    ),
  ]);

  return {
    ...core,
    releases: 'releases' in releaseResult ? releaseResult.releases : [],
    ...('releaseError' in releaseResult
      ? { releaseError: releaseResult.releaseError }
      : {}),
    backups: 'backups' in backupResult ? backupResult.backups : [],
    ...('backupError' in backupResult
      ? { backupError: backupResult.backupError }
      : {}),
  };
};

/**
 * Reads the finished upgrades. A server that prepares no upgrade answers not
 * found, which is an empty history. The reads of the room load together and
 * fail together, so a not found here would otherwise lose the version and
 * the restart too.
 */
const readHistory = async (
  client: MaintenanceClient,
): Promise<readonly UpgradeJournal[]> => {
  try {
    return (await client.listServerUpgradeHistory()) ?? [];
  } catch (thrown) {
    if (isNotFound(thrown)) return [];
    throw thrown;
  }
};

export const readMaintenanceCore = async (
  client: MaintenanceClient,
): Promise<MaintenanceCoreSnapshot> => {
  const [version, upgrade, history] = await Promise.all([
    client.getServerVersion(),
    readCurrentUpgrade(client),
    readHistory(client),
  ]);

  return { version, upgrade, history };
};

export const readReleases = async (
  client: MaintenanceClient,
): Promise<readonly UpgradeRelease[]> =>
  (await client.listServerUpgradeReleases()) ?? [];

export const readBackups = async (
  client: MaintenanceClient,
): Promise<readonly BackupManifest[]> =>
  (await client.listServerBackups()) ?? [];

/** Asks the server to download and verify the selected signed release. */
export const stageRelease = async (
  client: MaintenanceClient,
  release: UpgradeRelease,
): Promise<UpgradeJournal> =>
  client.stageServerUpgrade({
    body: {
      version: release.version,
    },
  });

/** What the restart store needs of the SDK: the request, and the read that tells whether the server is back. */
export type ServerRestartClient = Pick<
  PrusaConnect,
  'getUiSettings' | 'restartServer'
>;

/** `normal` starts the server as configured; `safe` starts it with every module off. */
export type RestartMode = 'normal' | 'safe';

/**
 * Asks the server to restart. It answers 202 before it stops, so a resolved
 * call means the restart was accepted, not that it has happened.
 */
export const restartServer = async (
  client: ServerRestartClient,
  mode: RestartMode,
): Promise<void> => {
  await client.restartServer({ body: { mode } });
};
