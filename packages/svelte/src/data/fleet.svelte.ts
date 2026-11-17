// The fleet, read once through the SDK and kept current from the event
// stream. The checklist of which event touches which row is earlier research
// section 8: the fleet adapter of pull request 185 stays behind, because it
// opens its own `fetch` and its own `EventSource`, and the map of event type
// to store action is what survives it.
import type {
  PrusaConnect,
  EventSubscription,
  EventType,
  StreamEvent,
} from '@prusa/connect';

import type { ReadonlyFleet } from '../domain/fleet';
import {
  DIALOG_PATHS,
  JOB_DISPLAY_PATH,
  JOB_PATH_PATH,
  JOB_PROGRESS_PATH,
  JOB_STATE_PATH,
  JOB_TIME_REMAINING_PATH,
  STATE_PATH,
} from '../domain/state-paths';
import type { Device } from '../domain/command';
import { REGISTRATION_EVENTS } from './registration-events';
import { DEVICE_FILES_EVENTS } from './services/device';
import { TRANSFER_EVENTS } from './services/transfers';

/**
 * What the fleet needs of the SDK. A `PrusaConnect` satisfies it, and naming
 * only the three operations is what lets a test hand the store a fake without
 * writing out every operation the core declares (svelte-data.md).
 */
export type FleetClient = Pick<
  PrusaConnect,
  'listDevices' | 'getDevice' | 'events'
>;

/**
 * The event types that send this store back to the API for a whole row. Each
 * of them means the record changed, and the record is what the listing
 * answers.
 */
export const FLEET_EVENTS: readonly EventType[] = [
  'com.prusa.connect.device.arrived',
  'com.prusa.connect.device.departed',
  'com.prusa.connect.device.presence.changed',
  'com.prusa.connect.device.link.changed',
  'com.prusa.connect.device.record.changed',
  'com.prusa.connect.stream.continuity.lost',
];

/**
 * The type that carries a print while it runs. There is no job event to
 * subscribe to: `sdk/event/vocabulary.go` declares none, and
 * `internal/app/devicerecords.go` says the job is deliberately not one of the
 * fields `device.record.changed` reports, because progress moves on every
 * telemetry frame and a durable frame per printing machine per flush is the
 * cost it refuses. So the print reaches a page on its state-tree paths and
 * nowhere else, and a fleet card that draws progress has to read them.
 */
export const FLEET_JOB_EVENTS: readonly EventType[] = [
  'com.prusa.connect.device.telemetry.sampled',
];

/**
 * The files on their way to any printer. The fleet changes nothing on them
 * and passes them to its listeners, so the header indicator reads every
 * printer's transfers on the stream the app already holds.
 */
export const FLEET_TRANSFER_EVENTS: readonly EventType[] = TRANSFER_EVENTS;

/** The type that says a camera was handed a new frame, so its still is new. */
export const CAMERA_FRAME_EVENT: EventType =
  'com.prusa.connect.camera.frame.received';

/**
 * What a host subscribes to for the fleet: the rows, the prints on them, and
 * the files on their way to them.
 */
export const FLEET_STREAM_EVENTS: readonly EventType[] = [
  ...FLEET_EVENTS,
  ...FLEET_JOB_EVENTS,
  ...FLEET_TRANSFER_EVENTS,
  CAMERA_FRAME_EVENT,
];

/**
 * How long the server folds frames over before it sends them. Without it a
 * fleet of four hundred machines sends one frame per path per sample, which
 * is the volume `live-telemetry.svelte.ts` is about. The SDK takes 100 ms to
 * 10 s, and a second is what a progress bar needs.
 */
export const FLEET_STREAM_INTERVAL_MS = 1000;

/**
 * The state-tree paths the fleet keeps current on its rows: the machine's own
 * state word, the print on its bed and the question it is waiting on. A frame
 * on any other path is dropped where it lands, because the rows have nowhere
 * to put a nozzle temperature and one printer's page reads those through
 * `LiveTelemetry`.
 */
const FLEET_PATHS: ReadonlySet<string> = new Set([
  STATE_PATH,
  JOB_DISPLAY_PATH,
  JOB_PATH_PATH,
  JOB_PROGRESS_PATH,
  JOB_STATE_PATH,
  JOB_TIME_REMAINING_PATH,
  ...DIALOG_PATHS,
]);

/** The page size the whole-fleet read asks for: the server's own cap. */
const WHOLE_FLEET_PAGE = 1000;

const SUBJECT_PREFIX = 'device:';

export const asError = (thrown: unknown): Error =>
  thrown instanceof Error ? thrown : new Error(String(thrown));

/**
 * Whether the server ended the stream because the credential that opened it
 * stopped being accepted. That end is a 401 with no code. A refusal when a
 * stream opens carries a code, and is not this.
 */
const endedForItsCredential = (thrown: unknown): boolean => {
  if (!(thrown instanceof Error)) return false;
  // The narrowing cast reads two fields off a value the SDK threw, which is a
  // trust boundary (typescript.md).
  const refused = thrown as { status?: unknown; code?: unknown };

  return refused.status === 401 && refused.code === undefined;
};

/**
 * Who is signed in, which the session store knows. The fleet asks it before it
 * opens a stream again, so the fleet of one person never carries on under the
 * cookie of another.
 */
export interface FleetIdentity {
  readonly principal: { readonly id: string } | undefined;
  /** Reads again who the browser's cookie belongs to. */
  read(): Promise<void>;
}

/** The subscription one call to `listen` holds, which a reopen replaces. */
interface Listening {
  current: EventSubscription | undefined;
  reopened: boolean;
  /**
   * Who was signed in when `listen` was called. A reopen compares with this
   * and not with the session as it reads when the stream ends, because
   * another reader may have moved the session to another person first.
   */
  readonly opener: string | undefined;
}

/**
 * Which printer an event is about. The envelope's subject is the origin in
 * its `kind:id` spelling, and the payload carries `device_id` on the frames
 * that have one. The core publishes no payload schema, so the field is read
 * behind a check rather than trusted.
 */
export const deviceIdOf = (event: StreamEvent): string | undefined => {
  const subject = event.envelope?.subject;
  if (subject?.startsWith(SUBJECT_PREFIX)) {
    return subject.slice(SUBJECT_PREFIX.length);
  }

  const payload = event.payload;
  if (typeof payload !== 'object' || payload === null) return undefined;
  // The payload crosses into our code as `unknown`, and this narrows it to the
  // one field being read (typescript.md).
  const named = (payload as { device_id?: unknown }).device_id;

  return typeof named === 'string' && named !== '' ? named : undefined;
};

export const ORIGIN_WITHDRAWN =
  'com.prusa.connect.stream.origin.withdrawn' as const;

/**
 * The origin a withdrawn frame names, split at the first `:` into its kind
 * and id. Any other frame names none.
 */
export const withdrawnOriginOf = (
  event: StreamEvent,
): { kind: string; id: string } | undefined => {
  if (event.type !== ORIGIN_WITHDRAWN) return undefined;
  const origin = event.payload.origin;
  const at = origin.indexOf(':');
  if (at < 1) return undefined;

  return { kind: origin.slice(0, at), id: origin.slice(at + 1) };
};

/** One reading: the path in the printer's state tree, and what it now reads. */
interface Reading {
  readonly path: string;
  /** `null` is the core removing the node, which is not a reading of zero. */
  readonly value: number | string | boolean | readonly string[] | null;
}

/**
 * Reads one telemetry frame. The core publishes no payload schema, so every
 * member is read behind a check rather than trusted (typescript.md). A frame
 * on a path this list does not draw answers undefined and costs nothing more.
 * A list is kept only as a list of words, which is what a dialog's buttons are.
 */
export const readingOf = (payload: unknown): Reading | undefined => {
  if (typeof payload !== 'object' || payload === null) return undefined;

  const named = payload as { path?: unknown; value?: unknown };
  if (typeof named.path !== 'string' || !FLEET_PATHS.has(named.path)) {
    return undefined;
  }

  const value = named.value;
  if (typeof value === 'number') {
    return Number.isFinite(value) ? { path: named.path, value } : undefined;
  }
  if (
    value === null ||
    typeof value === 'string' ||
    typeof value === 'boolean'
  ) {
    return { path: named.path, value };
  }
  if (
    Array.isArray(value) &&
    value.every((item): item is string => typeof item === 'string')
  ) {
    return { path: named.path, value };
  }

  return undefined;
};

/** One print of a `Device`, which is what the fleet card draws. */
type Print = NonNullable<Device['job']>;

/** Which word of the print each path carries. */
const JOB_TEXT_FIELD: Readonly<
  Record<string, 'display_name' | 'path' | 'state'>
> = {
  [JOB_DISPLAY_PATH]: 'display_name',
  [JOB_PATH_PATH]: 'path',
  [JOB_STATE_PATH]: 'state',
};

/**
 * Which number of the print each path carries, and what the state tree's own
 * reading is multiplied by to reach it. The machine reports whole seconds and
 * the print carries milliseconds, so the estimate is scaled and the percentage
 * is not.
 */
const JOB_NUMBER_FIELD: Readonly<
  Record<string, { field: 'progress' | 'time_remaining_ms'; scale: number }>
> = {
  [JOB_PROGRESS_PATH]: { field: 'progress', scale: 1 },
  [JOB_TIME_REMAINING_PATH]: { field: 'time_remaining_ms', scale: 1000 },
};

/**
 * The print a row carries before any frame has stated one. `running` is true
 * because a `/job/...` frame is the machine reporting a print it has on the
 * bed; the listing corrects the whole row on the next read.
 */
const NO_PRINT: Print = {
  device_job_id: '',
  display_name: '',
  id: '',
  path: '',
  progress: 0,
  running: true,
  state: '',
  time_remaining_ms: 0,
};

/**
 * One row with one reading applied, and no request made. That is why the
 * fleet may read these at all: the cost `live-telemetry.svelte.ts` refuses is
 * a refetch of the whole row per frame, and nothing here pays it.
 */
export const withReading = (row: Device, reading: Reading): Device => {
  const { path, value } = reading;
  if (value === null) {
    const state = Object.fromEntries(
      Object.entries(row.state).filter(([held]) => held !== path),
    );

    return { ...row, state };
  }

  const state = { ...row.state, [path]: value };
  if (path === STATE_PATH) return { ...row, state };

  const job = row.job ?? NO_PRINT;
  const word = JOB_TEXT_FIELD[path];
  if (word !== undefined && typeof value === 'string') {
    return { ...row, job: { ...job, [word]: value }, state };
  }

  const number = JOB_NUMBER_FIELD[path];
  if (number !== undefined && typeof value === 'number') {
    return {
      ...row,
      job: { ...job, [number.field]: value * number.scale },
      state,
    };
  }

  return { ...row, state };
};

const withRow = (
  devices: readonly Device[],
  device: Device,
): readonly Device[] => {
  const at = devices.findIndex((row) => row.id === device.id);
  if (at < 0) return [...devices, device];

  const next = [...devices];
  next[at] = device;

  return next;
};

/**
 * A shared store, so a class (svelte-5.md). The client comes in through the
 * constructor, which is what lets a test hand it a fake.
 *
 * The rows are `$state.raw`: a fleet runs to four hundred printers and every
 * row is replaced whole rather than edited in place, so deep proxying each
 * one would cost proxy creation for nothing (svelte-data.md).
 */
export class Fleet implements ReadonlyFleet {
  readonly #client: FleetClient;
  #devices = $state.raw<readonly Device[]>([]);
  // An index for every screen that finds a printer by its id, built again
  // only when the rows change. The first row with an id wins, as it does
  // for every write. Nothing draws it: a reader depends on the rows.
  // eslint-disable-next-line svelte/prefer-svelte-reactivity
  readonly #byId = new Map<string, Device>();
  #indexed: readonly Device[] | undefined;
  #loading = $state(false);
  #loaded = $state(false);
  #error = $state<Error | undefined>(undefined);
  #flight: Promise<void> | undefined;
  #listening: Listening | undefined;
  // Nothing renders from the listeners, so they need no reactivity.
  // eslint-disable-next-line svelte/prefer-svelte-reactivity
  readonly #listeners = new Set<(event: StreamEvent) => void>();
  // Counts the resets. A request that started before a reset records nothing.
  #resets = 0;

  readonly #identity: FleetIdentity | undefined;

  /**
   * The identity is optional. Without it the fleet cannot tell who a new
   * cookie belongs to, so it never opens a stream again and shows the error.
   */
  constructor(client: FleetClient, identity?: FleetIdentity) {
    this.#client = client;
    this.#identity = identity;
  }

  /** Read-only by design: the collection is the server's (svelte-5.md). */
  get devices(): readonly Device[] {
    return this.#devices;
  }

  get loading(): boolean {
    return this.#loading;
  }

  get loaded(): boolean {
    return this.#loaded;
  }

  get error(): Error | undefined {
    return this.#error;
  }

  device(id: string): Device | undefined {
    const rows = this.#devices;
    if (this.#indexed !== rows) {
      this.#byId.clear();
      for (const row of rows) {
        if (!this.#byId.has(row.id)) this.#byId.set(row.id, row);
      }
      this.#indexed = rows;
    }

    return this.#byId.get(id);
  }

  /**
   * Reads the fleet. A second call while one read is in flight is handed that
   * read, so two screens drawing the fleet cost one request (svelte-data.md).
   */
  load(): Promise<void> {
    if (this.#flight) return this.#flight;

    const flight = this.#read().finally(() => {
      if (this.#flight === flight) this.#flight = undefined;
    });
    this.#flight = flight;

    return flight;
  }

  /**
   * Opens the event stream and applies what it carries. The returned function
   * closes it, and an app calls that on teardown (svelte-testing.md).
   *
   * When the server ends the stream because its credential stopped being
   * accepted, the stream is opened once more from where it stopped, if the
   * cookie still belongs to the same person. A person who changed their own
   * password holds a new session by then, and sees no error. If the new
   * stream is refused or ended the same way, the error shows.
   */
  listen(): () => void {
    this.#listening?.current?.close();
    const listening: Listening = {
      current: undefined,
      reopened: false,
      opener: this.#identity?.principal?.id,
    };
    this.#listening = listening;
    this.#open(listening, undefined);

    return () => {
      listening.current?.close();
      listening.current = undefined;
      if (this.#listening === listening) this.#listening = undefined;
    };
  }

  #open(listening: Listening, since: string | undefined): void {
    // The SDK checks that the cookie still belongs to the opener before it
    // reconnects after a drop. Without that, a drop after another tab signed
    // in as someone else would carry on for that person.
    const subscription = this.#client.events({
      interval: FLEET_STREAM_INTERVAL_MS,
      types: FLEET_STREAM_EVENTS,
      ...(since === undefined ? {} : { since }),
      ...(listening.opener === undefined
        ? {}
        : { principal: listening.opener }),
    });
    listening.current = subscription;
    void this.#consume(listening, subscription);
  }

  /**
   * Calls `listener` after each event the store has applied. The returned
   * function removes it.
   */
  subscribe(listener: (event: StreamEvent) => void): () => void {
    this.#listeners.add(listener);

    return () => {
      this.#listeners.delete(listener);
    };
  }

  /**
   * Applies one event. It is public so a test can drive the store without a
   * socket, and so an app that already holds a subscription can feed it.
   */
  async apply(event: StreamEvent): Promise<void> {
    await this.#applyOne(event);
    for (const listener of this.#listeners) listener(event);
  }

  async #applyOne(event: StreamEvent): Promise<void> {
    if (event.type === 'com.prusa.connect.stream.continuity.lost') {
      // There is no partial recovery from a lost place in the log, so the
      // whole fleet is read again.
      await this.load();

      return;
    }

    // The server dropped this printer: what it held of it is gone, and there
    // is nothing to read.
    const withdrawn = withdrawnOriginOf(event);
    if (withdrawn !== undefined) {
      if (withdrawn.kind === 'device') {
        this.#devices = this.#devices.filter((row) => row.id !== withdrawn.id);
      }

      return;
    }

    // A registration names a device only once it is approved, and the device
    // may not exist until the machine dials in. The fleet learns of it from
    // device.arrived, so reading it here would ask for a record not yet there.
    if (REGISTRATION_EVENTS.includes(event.type)) return;

    // A transfer or a folder read changes nothing the printer listing
    // answers, so it is left to the listeners.
    if (FLEET_TRANSFER_EVENTS.includes(event.type)) return;
    if (DEVICE_FILES_EVENTS.includes(event.type)) return;

    // A frame names its camera in the device subject, not a printer, so it
    // is left to the cameras store.
    if (event.type === CAMERA_FRAME_EVENT) return;

    const id = deviceIdOf(event);
    if (id === undefined) return;

    // A print as it runs, applied to the row it names. A frame about a
    // printer this caller cannot see, or on a path the list does not draw,
    // changes nothing and asks for nothing.
    if (FLEET_JOB_EVENTS.includes(event.type)) {
      const reading = readingOf(event.payload);
      if (reading === undefined) return;

      const at = this.#devices.findIndex((row) => row.id === id);
      const row = this.#devices[at];
      if (row === undefined) return;

      const next = [...this.#devices];
      next[at] = withReading(row, reading);
      this.#devices = next;

      return;
    }

    await this.readDevice(id);
  }

  /**
   * Reads one printer again and replaces its row. A write calls this rather
   * than `load()`, because a list read already on its way may have started
   * before the write and would answer the old row.
   */
  async readDevice(id: string): Promise<void> {
    const resets = this.#resets;
    try {
      const device = await this.#client.getDevice({
        path: { device_id: id },
      });
      if (resets !== this.#resets) return;
      this.#devices = withRow(this.#devices, device);
      this.#error = undefined;
    } catch (thrown) {
      if (resets === this.#resets) this.#error = asError(thrown);
    }
  }

  async #read(): Promise<void> {
    const resets = this.#resets;
    this.#loading = true;
    try {
      const rows: Device[] = [];
      for (;;) {
        // Ordered by id so the order does not move with a printer's state.
        const page = await this.#client.listDevices({
          query: { limit: WHOLE_FLEET_PAGE, offset: rows.length, sort: 'id' },
        });
        if (resets !== this.#resets) return;
        const items = page.items ?? [];
        rows.push(...items);
        // A server that omits `total` is read until a short page.
        const complete =
          typeof page.total === 'number'
            ? rows.length >= page.total
            : items.length < WHOLE_FLEET_PAGE;
        if (items.length === 0 || complete) break;
      }
      this.#devices = rows;
      this.#loaded = true;
      this.#error = undefined;
    } catch (thrown) {
      if (resets === this.#resets) this.#error = asError(thrown);
    } finally {
      if (resets === this.#resets) this.#loading = false;
    }
  }

  async #consume(
    listening: Listening,
    subscription: EventSubscription,
  ): Promise<void> {
    try {
      for await (const event of subscription) {
        listening.reopened = false;
        await this.apply(event);
      }
    } catch (thrown) {
      // A stream closed or replaced here has nobody left to tell.
      if (listening.current !== subscription) return;

      if (
        !listening.reopened &&
        this.#identity !== undefined &&
        endedForItsCredential(thrown)
      ) {
        listening.reopened = true;
        await this.#reopenForTheSamePerson(listening, subscription);

        return;
      }

      // The server refused the stream, or ended the one opened again. That is
      // for a person to see rather than for a retry loop.
      this.#error = asError(thrown);
    }
  }

  /**
   * Opens the stream again if the cookie still belongs to the person who
   * opened it. A password change gives that person a new session, and their
   * fleet carries on. When another person signed in, or nobody is, the fleet
   * this store holds is theirs no longer, so it is dropped and nothing is
   * opened. The session store then shows what the app shows for that.
   */
  async #reopenForTheSamePerson(
    listening: Listening,
    ended: EventSubscription,
  ): Promise<void> {
    const identity = this.#identity;
    if (identity === undefined) return;

    await identity.read();
    const now = identity.principal?.id;

    // The stream may have been closed while the identity was read.
    if (listening.current !== ended) return;

    if (listening.opener !== undefined && listening.opener === now) {
      this.#open(listening, ended.lastEventId);

      return;
    }

    listening.current = undefined;
    this.reset();
  }

  /**
   * Forgets every printer and any error, and closes the stream this store
   * opened. An app calls it when the fleet no longer belongs to the person
   * signed in, so no screen draws another person's printers. An answer to a
   * request made before the reset is dropped when it arrives.
   *
   * A failed read keeps the rows of an earlier read. Those rows were read for
   * the same person, because a change of person resets the store first.
   */
  reset(): void {
    this.#resets += 1;
    this.#flight = undefined;
    const listening = this.#listening;
    this.#listening = undefined;
    listening?.current?.close();
    if (listening !== undefined) listening.current = undefined;
    this.#devices = [];
    this.#loading = false;
    this.#loaded = false;
    this.#error = undefined;
  }
}

/** One fleet per app, built where the app builds its `PrusaConnect`. */
export const createFleet = (
  client: FleetClient,
  identity?: FleetIdentity,
): Fleet => new Fleet(client, identity);
