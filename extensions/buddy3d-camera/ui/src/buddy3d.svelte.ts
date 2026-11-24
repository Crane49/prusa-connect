// The store the views read: where cameras send, the cameras this module
// paired, and the core's camera declarations, which say which cameras are of
// this module's type. It talks to this module's routes
// and, through the library's CameraSettings, to the core's camera resources.
import type { ModuleHost } from '@prusa/connect-ui/slots';
import { CameraSettings, printerOf } from '@prusa/connect-ui/data';

import {
  apiOn,
  said,
  type Buddy3dApi,
  type Control,
  type Live,
  type PairedCamera,
  type Settings,
  type Status,
} from './api';

/** How often the list is read again while a view is open. */
const REFRESH_MS = 10_000;

export class Buddy3d {
  status = $state<Status | undefined>(undefined);
  cameras = $state<readonly PairedCamera[]>([]);
  loaded = $state(false);
  busy = $state(false);
  /** Why the last act failed, as a sentence, and which camera it was for. */
  problem = $state('');
  problemFor = $state<string | undefined>(undefined);

  readonly #api: Buddy3dApi;
  readonly #host: ModuleHost;
  readonly #settings: CameraSettings;
  // Whether the person may operate each camera, as the core answered once.
  // svelte/reactivity is not in the app's import map, so a record it is.
  #operable = $state<Record<string, boolean>>({});
  readonly #asked: string[] = [];
  #timer: ReturnType<typeof setInterval> | undefined;
  #watchers = 0;

  constructor(host: ModuleHost) {
    this.#api = apiOn(host);
    this.#host = host;
    this.#settings = new CameraSettings(host.client);
    host.signal.addEventListener('abort', () => {
      clearInterval(this.#timer);
    });
  }

  /** The paired camera feeding the core camera of that name. */
  pairedAs(camera: string): PairedCamera | undefined {
    return this.cameras.find((row) => row.name === camera);
  }

  /** Whether the core declares camera as a Buddy3D camera, the type this module offers. */
  isMine(camera: string): boolean {
    const declared = this.#settings.items.find((item) => item.name === camera);

    return declared?.type === this.#api.cameraType;
  }

  /** Whether the core has answered for camera. A camera added a moment ago is read again. */
  declares(camera: string): boolean {
    return this.#settings.items.some((item) => item.name === camera);
  }

  /** Whether the person may declare cameras, which pairing one takes. */
  get mayConfigure(): boolean {
    return this.#settings.allowed === true;
  }

  /**
   * Whether the person may operate the camera, which changing its settings
   * takes. False until askOperate's question is answered, and after a refusal.
   */
  mayOperate(camera: string): boolean {
    return this.#operable[camera] === true;
  }

  /** Asks the core once whether the person may operate the camera. */
  askOperate(camera: string): void {
    if (this.#asked.includes(camera)) return;
    this.#asked.push(camera);
    void this.#host.client
      .checkAccess({ body: { action: 'operate', object_type: 'device', object_id: camera } })
      .then((decision) => {
        this.#operable = { ...this.#operable, [camera]: decision.allowed };
      })
      .catch(() => undefined);
  }

  /** Keeps what a person chose for the camera, over what it had. */
  async saveSettings(camera: string, change: Partial<Settings>): Promise<boolean> {
    const settings = { ...this.pairedAs(camera)?.settings, ...change };

    return this.#act(camera, () =>
      this.#api.routes.updateCameraSettings({
        path: { name: camera },
        body: {
          snapshot_interval: settings.snapshot_interval ?? 0,
          ...(settings.stream === undefined ? {} : { stream: settings.stream }),
        },
      }),
    );
  }

  /** Sends the camera one command: a turn, a restart or a setting it keeps itself. */
  async control(camera: string, body: Control): Promise<boolean> {
    return this.#act(camera, () => this.#api.routes.controlCamera({ path: { name: camera }, body }));
  }

  /** Starts reading while a view is open, and answers how to stop. */
  watch(): () => void {
    this.#watchers += 1;
    if (this.#watchers === 1) {
      void this.load();
      void this.loadDeclared();
      this.#timer = setInterval(() => void this.load(), REFRESH_MS);
    }

    return () => {
      this.#watchers -= 1;
      if (this.#watchers === 0) clearInterval(this.#timer);
    };
  }

  async load(): Promise<void> {
    try {
      const [status, cameras] = await Promise.all([
        this.#api.routes.readStatus({}),
        this.#api.routes.listCameras({}),
      ]);
      this.status = status;
      this.cameras = cameras.items;
    } catch (error) {
      this.#fail(undefined, error);
    } finally {
      this.loaded = true;
    }
  }

  /** Reads the core's camera declarations again. A read in flight is shared. */
  loadDeclared(): Promise<void> {
    return this.#settings.load();
  }

  /**
   * Pairs a Buddy3D camera the core already declares under a new token. The core lets this module feed the camera by
   * its type, so no grant is written here.
   */
  async setUp(camera: string): Promise<boolean> {
    return this.#act(camera, async () => {
      await this.#settings.load();
      const declared = this.#settings.items.find((item) => item.name === camera);
      const printer = declared === undefined ? undefined : printerOf(declared);
      if (printer === undefined) throw new Error('the camera watches no printer');

      await this.#api.routes.pairCamera({
        body: { name: camera, printer },
      });
    });
  }

  /** Forgets the pairing. The core camera stays, for its own card to remove. */
  async forget(camera: string): Promise<boolean> {
    return this.#act(camera, () => this.#api.routes.removeCamera({ path: { name: camera } }));
  }

  async snapshot(camera: string): Promise<boolean> {
    return this.#act(camera, () => this.#api.routes.requestSnapshot({ path: { name: camera } }));
  }

  /** Asks the camera for a live view: its offer, under a session. */
  startLive(camera: string): Promise<Live> {
    return this.#api.routes.startLive({ path: { name: camera } });
  }

  /** Hands the camera the browser's answer, with every candidate in it. */
  async answerLive(camera: string, session: string, answer: string): Promise<void> {
    await this.#api.routes.answerLive({ path: { name: camera, session }, body: { answer } });
  }

  /** Ends the live view. A failure is left alone: the camera ends it on the next start. */
  async stopLive(camera: string, session: string): Promise<void> {
    await this.#api.routes.stopLive({ path: { name: camera, session } }).catch(() => undefined);
  }

  /** The certificate a camera trusts, as text. */
  async certificate(): Promise<string> {
    const answer = await this.#api.routes.readCertificate({});

    return answer.text();
  }

  async #act(camera: string, work: () => Promise<unknown>): Promise<boolean> {
    if (this.busy) return false;
    this.busy = true;
    this.problem = '';
    try {
      await work();

      return true;
    } catch (error) {
      this.#fail(camera, error);

      return false;
    } finally {
      this.busy = false;
      await this.load();
    }
  }

  #fail(camera: string | undefined, error: unknown): void {
    this.problem = said(error) ?? (error instanceof Error ? error.message : String(error));
    this.problemFor = camera;
  }
}

