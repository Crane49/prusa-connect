import { untrack } from 'svelte';
import type { ErrorCode } from '@prusa/connect';

import { type DataFault, faultOf } from './fault';
import { PortalPairing } from './portal-pairing.svelte';
import {
  type BackupManifest,
  type MaintenanceClient,
  type OfflineUpdateBundle,
  readBackups,
  readCurrentUpgrade,
  readMaintenanceCore,
  readReleases,
  type ServerVersion,
  stageRelease,
  upgradeDeliveryOf,
  type UpgradeDeliveryService,
  type UpgradeJournal,
  type UpgradeRelease,
  type UpgradeSchedule,
} from './services/maintenance';

export type MaintenanceAction =
  | 'apply'
  | 'backup'
  | 'cancel'
  | 'delete-backup'
  | 'offline-upload'
  | 'prepare'
  | 'releases'
  | 'restore'
  | 'stage';

/** The update server refused the release list because this server asked too often. */
export const UPGRADE_SERVER_LIMITED_FAULT =
  'core.resource/UPGRADE_SERVER_LIMITED' satisfies ErrorCode;

/** The update server refused the release list because this server is not paired. */
export const PAIRING_NEEDED_FAULT =
  'core.resource/BETA_PORTAL_PAIRING_NEEDED' satisfies ErrorCode;

/**
 * The seconds the update server asks this server to wait, read from a
 * `UPGRADE_SERVER_LIMITED` refusal. The core copies the same number into
 * `Retry-After`. Undefined for any other fault.
 */
export const retryAfterOf = (
  fault: DataFault | undefined,
): number | undefined => {
  if (fault?.code !== UPGRADE_SERVER_LIMITED_FAULT) return undefined;
  const given = Number(fault.params.retry_after_seconds);

  return Number.isFinite(given) && given > 0 ? Math.ceil(given) : 1;
};

export interface MaintenanceOptions {
  /** The clock, in milliseconds, replaceable in a test. */
  readonly now?: () => number;
  /**
   * Whether the server prepares upgrades and keeps backups, as
   * `GET /api/v1/ui` says. Off, a load reads the server version alone,
   * because every upgrade and backup read answers 404 there. Absent is on.
   */
  readonly upgrades?: () => boolean;
}

/** Server maintenance data shared by the Updates & Backups screen. */
export class Maintenance {
  readonly #client: MaintenanceClient;
  readonly #delivery: UpgradeDeliveryService | undefined;
  #version = $state.raw<ServerVersion | undefined>(undefined);
  #upgrade = $state.raw<UpgradeJournal | undefined>(undefined);
  #releases = $state.raw<readonly UpgradeRelease[]>([]);
  #releaseError = $state<DataFault | undefined>(undefined);
  #releaseRetryAt = $state<number | undefined>(undefined);
  #history = $state.raw<readonly UpgradeJournal[]>([]);
  #backups = $state.raw<readonly BackupManifest[]>([]);
  #backupError = $state<DataFault | undefined>(undefined);
  #offlineBundle = $state.raw<OfflineUpdateBundle | undefined>(undefined);
  #offlineUploadProgress = $state<number | undefined>(undefined);
  #releaseLoading = $state(false);
  #backupLoading = $state(false);
  #loaded = $state(false);
  #loading = $state(false);
  #busy = $state<MaintenanceAction | undefined>(undefined);
  #error = $state<DataFault | undefined>(undefined);
  #flight: Promise<void> | undefined;
  // The value of the upgrades switch the load in flight started for.
  #flightFor: boolean | undefined;
  #resets = 0;

  readonly #portal: PortalPairing | undefined;
  readonly #now: () => number;
  readonly #upgrades: () => boolean;

  constructor(
    client: MaintenanceClient,
    delivery?: UpgradeDeliveryService,
    options: MaintenanceOptions = {},
  ) {
    this.#client = client;
    this.#now = options.now ?? (() => Date.now());
    this.#upgrades = options.upgrades ?? (() => true);
    this.#delivery = delivery ?? upgradeDeliveryOf(client);
    this.#portal =
      this.#delivery === undefined
        ? undefined
        : new PortalPairing(this.#delivery, {
            onPaired: () => void this.#reloadReleases(),
          });
  }

  /** The Beta Portal pairing, absent when the server cannot deliver online updates. */
  get portal(): PortalPairing | undefined {
    return this.#portal;
  }
  get version(): ServerVersion | undefined {
    return this.#version;
  }
  get upgrade(): UpgradeJournal | undefined {
    return this.#upgrade;
  }
  get releases(): readonly UpgradeRelease[] {
    return this.#releases;
  }
  get releaseError(): DataFault | undefined {
    return this.#releaseError;
  }
  /**
   * When the update server allows the next release list, in milliseconds
   * since the epoch. Until then the store sends no release request.
   */
  get releaseRetryAt(): number | undefined {
    return this.#releaseRetryAt;
  }
  get history(): readonly UpgradeJournal[] {
    return this.#history;
  }
  get backups(): readonly BackupManifest[] {
    return this.#backups;
  }
  get backupError(): DataFault | undefined {
    return this.#backupError;
  }
  get deliveryAvailable(): boolean {
    return this.#delivery !== undefined;
  }
  get offlineBundle(): OfflineUpdateBundle | undefined {
    return this.#offlineBundle;
  }
  get offlineUploadProgress(): number | undefined {
    return this.#offlineUploadProgress;
  }
  get releaseLoading(): boolean {
    return this.#releaseLoading;
  }
  get backupLoading(): boolean {
    return this.#backupLoading;
  }
  get loaded(): boolean {
    return this.#loaded;
  }
  get loading(): boolean {
    return this.#loading;
  }
  get busy(): MaintenanceAction | undefined {
    return this.#busy;
  }
  get error(): DataFault | undefined {
    return this.#error;
  }

  load(): Promise<void> {
    // An effect that loads must not track the settings, or every new read of
    // them would load the room again.
    const upgrades = untrack(this.#upgrades);
    if (this.#flight && this.#flightFor === upgrades) return this.#flight;
    // A load for the other value of the switch reads the wrong paths, so it
    // is not reused, and what it still answers is not kept.
    if (this.#flight) this.#resets += 1;
    this.#flightFor = upgrades;
    if (!upgrades) return this.#loadVersion();
    const reset = this.#resets;
    this.#loading = true;
    this.#releaseLoading = true;
    this.#backupLoading = true;

    const core = readMaintenanceCore(this.#client)
      .then((snapshot) => {
        if (reset !== this.#resets) return;
        this.#version = snapshot.version;
        this.#upgrade = snapshot.upgrade;
        this.#history = snapshot.history;
        this.#error = undefined;
      })
      .catch((thrown: unknown) => {
        if (reset === this.#resets) this.#error = faultOf(thrown);
      })
      .finally(() => {
        if (reset === this.#resets) {
          this.#loading = false;
          this.#loaded = true;
        }
      });

    const releases = readReleases(this.#client)
      .then((answer) => {
        if (reset !== this.#resets) return;
        this.#releasesRead(answer);
      })
      .catch((thrown: unknown) => {
        if (reset === this.#resets) this.#releasesRefused(thrown);
      })
      .finally(() => {
        if (reset === this.#resets) this.#releaseLoading = false;
      });

    const backups = readBackups(this.#client)
      .then((answer) => {
        if (reset !== this.#resets) return;
        this.#backups = answer;
        this.#backupError = undefined;
      })
      .catch((thrown: unknown) => {
        if (reset === this.#resets) this.#backupError = faultOf(thrown);
      })
      .finally(() => {
        if (reset === this.#resets) this.#backupLoading = false;
      });

    const portal = this.#portal?.load() ?? Promise.resolve();
    const pairing = Promise.all([releases, portal]).then(async () => {
      if (reset === this.#resets) await this.#recheckPortal();
    });

    const flight = Promise.all([core, pairing, backups])
      .then(() => undefined)
      .finally(() => {
        if (this.#flight === flight) this.#flight = undefined;
      });
    this.#flight = flight;

    return flight;
  }

  /**
   * The load of a server that prepares no upgrade: the version, which the
   * Server room and the restart still read, and nothing else.
   */
  #loadVersion(): Promise<void> {
    const reset = this.#resets;
    this.#loading = true;

    const flight = this.#client
      .getServerVersion()
      .then((version) => {
        if (reset !== this.#resets) return;
        this.#version = version;
        this.#error = undefined;
      })
      .catch((thrown: unknown) => {
        if (reset === this.#resets) this.#error = faultOf(thrown);
      })
      .finally(() => {
        if (reset === this.#resets) {
          this.#loading = false;
          this.#loaded = true;
        }
        if (this.#flight === flight) this.#flight = undefined;
      });
    this.#flight = flight;

    return flight;
  }

  /** Reads the release list again, unless the update server asked to wait. */
  async refreshReleases(): Promise<void> {
    if (this.#busy !== undefined || this.#releasesWaiting()) return;
    this.#busy = 'releases';
    this.#releaseLoading = true;
    try {
      this.#releasesRead(await readReleases(this.#client));
    } catch (thrown) {
      this.#releasesRefused(thrown);
    } finally {
      this.#releaseLoading = false;
      this.#busy = undefined;
    }
    await this.#recheckPortal();
  }

  async refreshBackups(): Promise<void> {
    if (this.#busy !== undefined) return;
    this.#backupLoading = true;
    try {
      this.#backups = await readBackups(this.#client);
      this.#backupError = undefined;
    } catch (thrown) {
      this.#backupError = faultOf(thrown);
    } finally {
      this.#backupLoading = false;
    }
  }

  async uploadOfflineBundle(bundle: File): Promise<void> {
    const delivery = this.#delivery;
    if (delivery === undefined) return;
    this.#offlineUploadProgress = 0;
    await this.#act('offline-upload', async () => {
      const receipt = await delivery.uploadOfflineBundle(
        bundle,
        (sent, total) => {
          this.#offlineUploadProgress =
            total > 0 ? Math.min(100, Math.round((sent / total) * 100)) : 0;
        },
      );
      this.#offlineBundle = receipt.bundle;
      this.#upgrade = receipt.upgrade;
      this.#offlineUploadProgress = 100;
    });
  }

  // A refused stage or prepare can end the update on the server, so the
  // store reads it again and the card shows where it is now.
  async stage(release: UpgradeRelease): Promise<void> {
    await this.#act(
      'stage',
      async () => {
        this.#upgrade = await stageRelease(this.#client, release);
      },
      () => this.#rereadUpgrade(),
    );
  }

  async prepare(): Promise<void> {
    await this.#act(
      'prepare',
      async () => {
        this.#upgrade = await this.#client.prepareServerUpgrade();
      },
      () => this.#rereadUpgrade(),
    );
  }

  /**
   * Authorizes the prepared upgrade. After a refusal the store reads the
   * upgrade again, so a review that changed since the page loaded is shown
   * before the operator approves it.
   */
  async apply(schedule: UpgradeSchedule): Promise<void> {
    await this.#act(
      'apply',
      async () => {
        this.#upgrade = await this.#client.applyServerUpgrade({
          body: schedule,
        });
      },
      () => this.#rereadUpgrade(),
    );
  }

  async #rereadUpgrade(): Promise<void> {
    const reset = this.#resets;
    try {
      const upgrade = await readCurrentUpgrade(this.#client);
      if (reset === this.#resets) this.#upgrade = upgrade;
    } catch {
      // The refusal is already shown. A failed reread keeps the record.
    }
  }

  async cancel(): Promise<void> {
    await this.#act('cancel', async () => {
      await this.#client.cancelServerUpgrade();
      this.#upgrade = undefined;
    });
  }

  async createBackup(
    toVersion?: string,
    postgresBackupAck = false,
  ): Promise<void> {
    await this.#act('backup', async () => {
      const made = await this.#client.createServerBackup({
        query: {
          ...(toVersion === undefined ? {} : { to_version: toVersion }),
          postgres_backup_ack: postgresBackupAck,
        },
      });
      this.#backups = [
        made,
        ...this.#backups.filter((one) => one.id !== made.id),
      ];
      this.#backupError = undefined;
    });
  }

  async restoreBackup(id: string): Promise<void> {
    await this.#act('restore', async () => {
      await this.#client.restoreServerBackup({ path: { id } });
    });
  }

  async deleteBackup(id: string): Promise<void> {
    await this.#act('delete-backup', async () => {
      await this.#client.deleteServerBackup({ path: { id } });
      this.#backups = this.#backups.filter((one) => one.id !== id);
    });
  }

  reset(): void {
    this.#resets += 1;
    this.#flight = undefined;
    this.#version = undefined;
    this.#upgrade = undefined;
    this.#releases = [];
    this.#releaseError = undefined;
    this.#releaseRetryAt = undefined;
    this.#history = [];
    this.#backups = [];
    this.#backupError = undefined;
    this.#portal?.reset();
    this.#offlineBundle = undefined;
    this.#offlineUploadProgress = undefined;
    this.#releaseLoading = false;
    this.#backupLoading = false;
    this.#loaded = false;
    this.#loading = false;
    this.#busy = undefined;
    this.#error = undefined;
  }

  async #act(
    action: MaintenanceAction,
    work: () => Promise<void>,
    failed?: () => Promise<void>,
  ): Promise<void> {
    if (this.#busy !== undefined) return;
    this.#busy = action;
    try {
      await work();
      this.#error = undefined;
    } catch (thrown) {
      this.#error = faultOf(thrown);
      await failed?.();
    } finally {
      this.#busy = undefined;
    }
  }

  // A pairing that finished shows the releases it unlocked. A refusal stays
  // in releaseError and does not touch the pairing.
  async #reloadReleases(): Promise<void> {
    if (this.#releasesWaiting()) return;
    try {
      this.#releasesRead(await readReleases(this.#client));
    } catch (thrown) {
      this.#releasesRefused(thrown);
    }
    await this.#recheckPortal();
  }

  // A portal that refuses the server's token answers the release list with an
  // error, and the server marks the pairing revoked at that moment. The status
  // this page read before may still say paired, so it is read once more.
  async #recheckPortal(): Promise<void> {
    if (this.#releaseError === undefined) return;
    if (this.#portal?.status?.state !== 'paired') return;
    await this.#portal.load();
  }

  #releasesWaiting(): boolean {
    return (
      this.#releaseRetryAt !== undefined && this.#now() < this.#releaseRetryAt
    );
  }

  #releasesRead(answer: readonly UpgradeRelease[]): void {
    this.#releases = answer;
    this.#releaseError = undefined;
    this.#releaseRetryAt = undefined;
  }

  // A refusal for asking too often keeps the wait the update server named,
  // so a retry does not ask it again before then.
  #releasesRefused(thrown: unknown): void {
    const fault = faultOf(thrown);
    const wait = retryAfterOf(fault);
    this.#releases = [];
    this.#releaseError = fault;
    this.#releaseRetryAt =
      wait === undefined ? undefined : this.#now() + wait * 1000;
  }
}
