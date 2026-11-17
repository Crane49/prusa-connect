// The cameras an operator declares from the app, and the printer each one
// watches.
import { type DataFault, faultOf } from './fault';
import {
  type CameraDeclaration,
  type CameraSettingsClient,
  mayConfigureCameras,
  PRINTER_KEY,
  readCameraDeclarations,
  readCameraTypes,
  type CameraTypeChoice,
  removeCamera,
  writeCamera,
} from './services/camera-settings';

/** What a person fills in to add or change one camera. */
export interface CameraDraft {
  readonly name: string;
  /** `generic`, or a type an installed module offers (ADR-20261106-km2). */
  readonly type: string;
  /** The printer it watches. */
  readonly printer: string;
  /** The endpoint that answers one JPEG. Empty for a camera that sends its own. */
  readonly snapshotUrl?: string | undefined;
  /** The stream: rtsp:// for H.264, or http:// for MJPEG. */
  readonly streamUrl?: string | undefined;
}

/** Each act this store sends is a save or a removal. */
export type CameraAct = 'save' | 'remove';

/** These options set how the store behaves, beyond its client. */
export interface CameraSettingsOptions {
  /**
   * Called after a write, so the host can read the camera listing again. The
   * store waits for it before it reports done.
   */
  readonly afterWrite?: () => Promise<void>;
}

/** The printer a declaration names, or undefined for one that names none. */
export const printerOf = (camera: CameraDeclaration): string | undefined => {
  const printer = camera.config[PRINTER_KEY];

  return typeof printer === 'string' ? printer : undefined;
};

/** The type without the `core/` namespace a declaration may carry. */
export const cameraTypeOf = (camera: CameraDeclaration): string =>
  camera.type.replace(/^core\//, '');

// The settings a draft writes for a camera of one type. A URL left empty is
// left out, so the driver declares no such endpoint rather than one it cannot
// fetch.
const configOf = (
  draft: CameraDraft,
  type: string,
): Record<string, unknown> => {
  const config: Record<string, unknown> = { [PRINTER_KEY]: draft.printer };
  if (type !== 'generic' && type !== 'mjpeg') return config;

  const snapshot = draft.snapshotUrl?.trim();
  const stream = draft.streamUrl?.trim();
  if (snapshot) config.snapshot_url = snapshot;
  if (stream) config.stream_url = stream;

  return config;
};

/**
 * A shared store for one printer's page, so a class (svelte-5.md).
 *
 * It holds every camera, because a camera moved to another printer leaves
 * this printer's list and its write still needs the version. The store reads
 * the list again after every write, so it never sends a version used up.
 */
export class CameraSettings {
  readonly #client: CameraSettingsClient;
  readonly #afterWrite: () => Promise<void>;

  #items = $state.raw<readonly CameraDeclaration[]>([]);
  #loading = $state(false);
  #loaded = $state(false);
  #error = $state<DataFault | undefined>(undefined);
  #failed = $state<CameraAct | undefined>(undefined);
  #pending = $state<CameraAct | undefined>(undefined);
  #allowed = $state<boolean | undefined>(undefined);
  #types = $state.raw<readonly CameraTypeChoice[]>([]);
  #flight: Promise<void> | undefined;

  constructor(
    client: CameraSettingsClient,
    options: CameraSettingsOptions = {},
  ) {
    this.#client = client;
    this.#afterWrite = options.afterWrite ?? (() => Promise.resolve());
  }

  /** Every declared camera. Read-only by design: the list is the server's. */
  get items(): readonly CameraDeclaration[] {
    return this.#items;
  }

  get loading(): boolean {
    return this.#loading;
  }

  /** True once a read has answered, which tells no cameras from an unread list. */
  get loaded(): boolean {
    return this.#loaded;
  }

  /** The last error, kept by its code. */
  get error(): DataFault | undefined {
    return this.#error;
  }

  /** Which act the last error stopped, or undefined for a refused read. */
  get failed(): CameraAct | undefined {
    return this.#failed;
  }

  /** The act waiting on the server. One at a time, because each uses the version. */
  get pending(): CameraAct | undefined {
    return this.#pending;
  }

  /** Whether the person may declare cameras, undefined until the server answered. */
  get allowed(): boolean | undefined {
    return this.#allowed;
  }

  /**
   * The types a new camera may be declared as: the core's generic camera and
   * the types installed modules offer. Empty until the server answered.
   */
  get types(): readonly CameraTypeChoice[] {
    return this.#types;
  }

  /** The cameras one printer declares, sorted by name as the listing sorts them. */
  of(printer: string): readonly CameraDeclaration[] {
    return this.#items.filter((camera) => printerOf(camera) === printer);
  }

  /** A second call while one read is in flight is given that read. */
  load(): Promise<void> {
    if (this.#flight) return this.#flight;

    const flight = this.#read().finally(() => {
      if (this.#flight === flight) this.#flight = undefined;
    });
    this.#flight = flight;

    return flight;
  }

  /**
   * Adds a camera, or changes the one the list holds under the draft's name.
   * A change keeps the settings the draft does not name, such as a snapshot
   * history, and keeps the type, which only a removal changes.
   */
  save(draft: CameraDraft): Promise<boolean> {
    const held = this.#items.find((camera) => camera.name === draft.name);

    return this.#act('save', () =>
      held === undefined
        ? writeCamera(
            this.#client,
            draft.name,
            draft.type,
            configOf(draft, draft.type),
          )
        : writeCamera(
            this.#client,
            held.name,
            held.type,
            { ...this.#keptOf(held), ...configOf(draft, cameraTypeOf(held)) },
            held.version,
          ),
    );
  }

  /** Removes one camera. */
  remove(camera: CameraDeclaration): Promise<boolean> {
    return this.#act('remove', () =>
      removeCamera(this.#client, camera.name, camera.version),
    );
  }

  // The settings a change keeps. The endpoints are the draft's to set, so an
  // endpoint the draft clears is cleared rather than kept.
  #keptOf(camera: CameraDeclaration): Record<string, unknown> {
    const kept = { ...camera.config };
    delete kept.snapshot_url;
    delete kept.stream_url;

    return kept;
  }

  async #act(act: CameraAct, send: () => Promise<unknown>): Promise<boolean> {
    if (this.#pending !== undefined) return false;

    this.#pending = act;
    this.#error = undefined;
    this.#failed = undefined;
    try {
      await send();
    } catch (thrown) {
      this.#error = faultOf(thrown);
      this.#failed = act;
    }
    // A refused write may be a version another person used up, so the list
    // is read again either way.
    try {
      // A read that started before the write holds the old list, so it is
      // let finish and a new one is started.
      await this.#flight;
      await this.load();
      if (this.#failed === undefined) await this.#afterWrite();
    } finally {
      this.#pending = undefined;
    }

    return this.#failed === undefined;
  }

  async #read(): Promise<void> {
    this.#loading = true;
    try {
      // The types change only with an install, so a list read once is kept.
      // They name each camera's module, which a person who may not read the
      // declarations still needs, so they do not wait on them.
      const types =
        this.#types.length === 0
          ? readCameraTypes(this.#client)
              .catch(() => [])
              .then((read) => {
                this.#types = read;
              })
          : undefined;
      const [items, allowed] = await Promise.all([
        readCameraDeclarations(this.#client),
        this.#allowed === undefined
          ? mayConfigureCameras(this.#client).catch(() => undefined)
          : Promise.resolve(this.#allowed),
        types,
      ]);
      this.#items = items;
      this.#allowed = allowed;
      this.#loaded = true;
      // A refused read is replaced by a read that worked. A refused write
      // stays on the card that sent it.
      if (this.#failed === undefined) this.#error = undefined;
    } catch (thrown) {
      if (this.#failed === undefined) this.#error = faultOf(thrown);
    } finally {
      this.#loading = false;
    }
  }
}
