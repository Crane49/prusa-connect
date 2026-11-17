import {
  ApiError,
  type PrusaConnect,
  type EventSubscription,
} from '@prusa/connect';
import {
  DASHBOARD_EVENTS,
  DEVICE_FILES_EVENTS,
  DEVICE_JOB_EVENTS,
  DEVICE_STORAGE_EVENTS,
  type Fleet,
  type FleetIdentity,
  FLEET_STREAM_EVENTS,
  FLEET_STREAM_INTERVAL_MS,
  moduleNoticesOf,
  NOTICE_EVENTS,
  ORIGIN_WITHDRAWN,
  REGISTRATION_EVENTS,
  withdrawnOriginOf,
} from '@prusa/connect-ui/data';

import { log } from '../log';

const streamLog = log.scope('live-updates');

/** A resource started, stopped, failed or was switched on or off. */
export const RESOURCE_STATE_CHANGED =
  'com.prusa.connect.resource.state.changed' as const;

/** A running module changed which entries of its own it hides (ADR-20261021-6h2). */
export const RESOURCE_UI_CHANGED =
  'com.prusa.connect.resource.ui.changed' as const;

/** A durable core upgrade transaction advanced. */
export const SERVER_UPGRADE_CHANGED =
  'com.prusa.connect.server.upgrade' as const;

/** The stream skipped events, as it does after the server restarted. */
const CONTINUITY_LOST = 'com.prusa.connect.stream.continuity.lost';

/** Whether the app is reading the event stream. */
export type ConnectionState = 'online' | 'offline';

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

/** The subscription one call to `open` holds, which a reopen replaces. */
interface Reading {
  current: EventSubscription | undefined;
  reopened: boolean;
  /**
   * Who was signed in when `open` was called. A reopen compares with this and
   * not with the session as it reads when the stream ends, because another
   * reader may have moved the session to another person first.
   */
  readonly opener: string | undefined;
}

/**
 * The app's one subscription to `GET /api/v1/events`, read through the SDK
 * (ADR-20260909-c7x rule 8). It feeds the component library's fleet store and reports
 * whether the app is still reading the stream, which is what the shell's
 * `ConnectionStatus` draws. It also asks for the settled prints the bell
 * lists, the print starts and ends a printer's page reads its jobs again
 * on, the changes of a dashboard, and the registration frames, which the
 * fleet itself does not subscribe to.
 *
 * The fleet store can open a subscription of its own. This app opens it here
 * instead, because the shell wants the state of that subscription and the
 * store publishes none, and because ADR-20260911-4p6 rule 23 asks that one stream
 * serve the whole page. The client shares that stream with the other tabs of
 * this browser (ADR-20261023-a52), and this store reads it the same way either way.
 *
 * `online` means the app is reading the stream, not that a socket is up this
 * instant. The SDK reconnects after a drop and resumes from the last id it
 * saw, so a drop it recovers from is invisible to every consumer by design.
 * What a reader can act on is the difference between a stream this app is
 * reading and one the server ended, and that is what these two values are.
 */
export class LiveUpdates {
  readonly #client: PrusaConnect;
  readonly #fleet: Fleet;
  readonly #identity: FleetIdentity | undefined;
  #connection = $state<ConnectionState>('offline');
  #error = $state<string | undefined>(undefined);
  readonly #onResource: () => void;
  readonly #onMaintenance: () => void;
  readonly #onDashboards: () => void;
  #reading: Reading | undefined;

  /**
   * The identity is the session store. Without it the store cannot tell who
   * a new cookie belongs to, so it never opens the stream again and shows
   * the error. `onResource` hears every resource state change, and every
   * change of what a module hides, which the fleet does not.
   */
  constructor(
    client: PrusaConnect,
    fleet: Fleet,
    identity?: FleetIdentity,
    onResource: () => void = () => undefined,
    onMaintenance: () => void = () => undefined,
    onDashboards: () => void = () => undefined,
  ) {
    this.#client = client;
    this.#fleet = fleet;
    this.#identity = identity;
    this.#onResource = onResource;
    this.#onMaintenance = onMaintenance;
    this.#onDashboards = onDashboards;
  }

  /** Read-only by design: `open()` and the stream ending are what move it. */
  get connection(): ConnectionState {
    return this.#connection;
  }

  /** Why the server ended the stream, when it did. Undefined otherwise. */
  get error(): string | undefined {
    return this.#error;
  }

  /**
   * Opens the stream and returns the call that closes it. A component calls
   * this from an `$effect` and returns the result, so the subscription dies
   * with the screen (svelte-testing.md).
   *
   * When the server ends the stream because its credential stopped being
   * accepted, the stream is opened once more from where it stopped, if the
   * cookie still belongs to the same person. A person who changed their own
   * password holds a new session by then, and the Printers list carries on.
   * When somebody else or nobody is signed in, the fleet is dropped and
   * nothing is opened, and the session store shows what the app shows then.
   */
  open(): () => void {
    this.#reading?.current?.close();
    const reading: Reading = {
      current: undefined,
      reopened: false,
      opener: this.#identity?.principal?.id,
    };
    this.#reading = reading;
    this.#error = undefined;
    this.#start(reading, undefined);

    return () => {
      reading.current?.close();
      reading.current = undefined;
      this.#connection = 'offline';
      if (this.#reading === reading) this.#reading = undefined;
    };
  }

  #start(reading: Reading, since: string | undefined): void {
    // The SDK checks that the cookie still belongs to the opener before it
    // reconnects after a drop. Without that, a drop after another tab signed
    // in as someone else would carry on for that person.
    const subscription = this.#client.events({
      interval: FLEET_STREAM_INTERVAL_MS,
      // Each type once: the lists overlap where two readers wait on one
      // frame, such as a registration the bell and the add-printer page both
      // read.
      types: [
        ...FLEET_STREAM_EVENTS,
        ...NOTICE_EVENTS,
        RESOURCE_STATE_CHANGED,
        RESOURCE_UI_CHANGED,
        ...DEVICE_JOB_EVENTS,
        // A USB drive put in or pulled out, which a printer's page and the
        // print picker read.
        ...DEVICE_STORAGE_EVENTS,
        // A printer folder whose read began or landed, which a printer's
        // Files tab waits on.
        ...DEVICE_FILES_EVENTS,
        // A dashboard changed on another screen (ADR-20261008-sww rule 31).
        ...DASHBOARD_EVENTS,
        // A printer asking to join, the decision on it, and a printer
        // turned away, which the add-printer page waits on.
        ...REGISTRATION_EVENTS,
        SERVER_UPGRADE_CHANGED,
        ORIGIN_WITHDRAWN,
      ].filter((type, at, all) => all.indexOf(type) === at),
      ...(since === undefined ? {} : { since }),
      ...(reading.opener === undefined ? {} : { principal: reading.opener }),
    });
    reading.current = subscription;
    this.#connection = 'online';
    void this.#consume(reading, subscription);
  }

  async #consume(
    reading: Reading,
    subscription: EventSubscription,
  ): Promise<void> {
    try {
      for await (const event of subscription) {
        reading.reopened = false;
        if (
          event.type === RESOURCE_STATE_CHANGED ||
          event.type === RESOURCE_UI_CHANGED
        ) {
          this.#onResource();
          // The bell reads a switch-off off the fleet's listeners. Any other
          // state change stays away from them: the printers listing re-reads
          // on every event it does not know.
          if (
            event.type === RESOURCE_STATE_CHANGED &&
            moduleNoticesOf(event).length > 0
          ) {
            await this.#fleet.apply(event);
          }
          continue;
        }
        const withdrawn = withdrawnOriginOf(event);
        if (withdrawn !== undefined && withdrawn.kind !== 'device') {
          // A dashboard is read again on its own; any other origin is a
          // module's resource, whose modules and catalogue are read again.
          if (withdrawn.kind === 'dashboard') this.#onDashboards();
          else this.#onResource();
          continue;
        }
        if (event.type === SERVER_UPGRADE_CHANGED) {
          this.#onMaintenance();
          continue;
        }
        // The skipped events may have been the ones the listeners wait on:
        // an applied update finishes in a process that published nothing.
        if (event.type === CONTINUITY_LOST) {
          this.#onResource();
          this.#onMaintenance();
          this.#onDashboards();
        }
        await this.#fleet.apply(event);
      }
    } catch (thrown) {
      // A stream closed or replaced here has nobody left to tell.
      if (reading.current !== subscription) return;

      if (
        !reading.reopened &&
        this.#identity !== undefined &&
        endedForItsCredential(thrown)
      ) {
        reading.reopened = true;
        await this.#reopenForTheSamePerson(reading, subscription);

        return;
      }

      // The server refused the stream, or ended the one opened again. That is
      // for a person to see rather than for a retry loop.
      this.#error = thrown instanceof Error ? thrown.message : String(thrown);
      // A refusal's message is server text, so only its coded parts are kept.
      streamLog.warn(
        'stream ended',
        thrown instanceof ApiError
          ? {
              status: thrown.status,
              code: thrown.code,
              traceId: thrown.traceId,
            }
          : { error: this.#error },
      );
    }

    if (reading.current === subscription) this.#connection = 'offline';
  }

  /**
   * Opens the stream again if the cookie still belongs to the person who
   * opened it. Otherwise the fleet is theirs no longer, so it is dropped.
   */
  async #reopenForTheSamePerson(
    reading: Reading,
    ended: EventSubscription,
  ): Promise<void> {
    const identity = this.#identity;
    if (identity === undefined) return;

    await identity.read();
    const now = identity.principal?.id;

    // The stream may have been closed while the identity was read.
    if (reading.current !== ended) return;

    if (reading.opener !== undefined && reading.opener === now) {
      this.#start(reading, ended.lastEventId);

      return;
    }

    reading.current = undefined;
    this.#fleet.reset();
    this.#connection = 'offline';
  }
}
