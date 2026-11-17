// What the bell lists: the events a person wants to hear about while the app
// is open, read off the stream the fleet already consumes. Nothing is stored
// on the server or in the browser, so a reload starts an empty list.
import type { EventType, StreamEvent } from '@prusa/connect';

import type { Device } from '../domain/command';
import type { NotificationKind } from '../domain/notification';
import { deviceIdOf, type Fleet } from './fleet.svelte';
import {
  type RefusalReason,
  REGISTRATION_EVENTS,
  type RegistrationChange,
  registrationChangeOf,
} from './registration-events';

/**
 * The types the fleet does not subscribe to and the bell needs. A host adds
 * them to the one stream it opens.
 */
export const NOTICE_EVENTS: readonly EventType[] = [
  'com.prusa.connect.device.job.settled',
  'com.prusa.connect.resource.state.changed',
  ...REGISTRATION_EVENTS,
];

const JOB_SETTLED = 'com.prusa.connect.device.job.settled';
const PRESENCE_CHANGED = 'com.prusa.connect.device.presence.changed';
const ARRIVED = 'com.prusa.connect.device.arrived';
const RESOURCE_STATE_CHANGED = 'com.prusa.connect.resource.state.changed';
const SWITCHED_OFF_BY_HOST = 'switched-off-by-host';

/** One thing that happened, in data. A screen turns it into words. */
export interface Notice {
  readonly id: string;
  readonly kind: NotificationKind;
  /**
   * The device it happened to. Absent for a printer that has no device yet:
   * one waiting for approval, or one turned away at the door.
   */
  readonly deviceId?: string | undefined;
  /** The code a printer waiting for approval shows on its display. */
  readonly code?: string | undefined;
  /**
   * The printer's name when it happened, or its id when it had none. For a
   * printer with no device yet it is the model it reported, or empty.
   */
  readonly printer: string;
  /** Why the server turned a printer away. */
  readonly reason?: RefusalReason | undefined;
  /**
   * The instance name of a module the server switched off, which
   * `printer` stays empty for.
   */
  readonly module?: string | undefined;
  /** The file of a print. */
  readonly file?: string | undefined;
  /** The print's id, which its own page is addressed by. */
  readonly jobId?: string | undefined;
  /** The issue token of a printer that reported a new issue. */
  readonly issue?: string | undefined;
  /** Milliseconds since the epoch. */
  readonly at: number;
  readonly read: boolean;
}

export type NoticeDraft = Omit<Notice, 'id' | 'read' | 'at'>;

// The payload crosses into our code as `unknown`, and this narrows it to the
// one field being read (typescript.md).
const fieldOf = (payload: unknown, name: string): unknown =>
  typeof payload === 'object' && payload !== null
    ? (payload as Record<string, unknown>)[name]
    : undefined;

const textIn = (payload: unknown, name: string): string | undefined => {
  const value = fieldOf(payload, name);

  return typeof value === 'string' && value !== '' ? value : undefined;
};

const OUTCOME_KIND: Readonly<Record<string, NotificationKind>> = {
  finished: 'job-finished',
  failed: 'job-failed',
};

/** The notice a module the server switched off raises, and none for any other state change. */
export const moduleNoticesOf = (event: StreamEvent): NoticeDraft[] => {
  if (textIn(event.payload, 'result') !== SWITCHED_OFF_BY_HOST) return [];
  // The resource is `<kind>/<name>`; the instance name is what a person knows.
  const module = textIn(event.payload, 'resource')?.split('/')[1];

  return module === undefined || module === ''
    ? []
    : [{ kind: 'module-switched-off', printer: '', module }];
};

/**
 * The notices one applied event raises. `before` is the set of issue tokens
 * the printer carried before the event, or undefined when this page has no
 * earlier reading of it: an issue it already had then is not news.
 */
export const noticesOf = (
  event: StreamEvent,
  device: Device | undefined,
  before: ReadonlySet<string> | undefined,
): NoticeDraft[] => {
  const deviceId = deviceIdOf(event);
  if (deviceId === undefined) return [];

  const printer =
    device?.name === undefined || device.name === '' ? deviceId : device.name;

  if (event.type === JOB_SETTLED) {
    const kind = OUTCOME_KIND[textIn(event.payload, 'outcome') ?? ''];
    if (kind === undefined) return [];
    const file =
      textIn(event.payload, 'display_name') ?? textIn(event.payload, 'path');

    const jobId = textIn(event.payload, 'job_id');

    return [{ kind, deviceId, printer, file, jobId }];
  }

  if (event.type === PRESENCE_CHANGED) {
    return fieldOf(event.payload, 'connected') === false
      ? [{ kind: 'printer-offline', deviceId, printer }]
      : [];
  }

  if (before === undefined) return [];

  return (device?.issues ?? [])
    .filter((issue) => !before.has(issue.reason))
    .map((issue) => ({
      kind: issue.severity === 'error' ? 'printer-error' : 'printer-attention',
      deviceId,
      printer,
      issue: issue.reason,
    }));
};

const issuesOf = (device: Device | undefined): ReadonlySet<string> =>
  new Set((device?.issues ?? []).map((issue) => issue.reason));

/** What the feed reads of the fleet. */
export type NoticeFleet = Pick<Fleet, 'device' | 'devices' | 'subscribe'>;

export interface NoticesOptions {
  /** Called once per new notice, for a host that also raises a toast. */
  readonly onNotice?: ((notice: Notice) => void) | undefined;
  readonly now?: (() => number) | undefined;
  /**
   * Whether this server approves every printer on arrival. A printer then
   * waits for nobody, so its code is not news.
   */
  readonly admitsOnArrival?: (() => boolean) | undefined;
  /** The name a person reads for a model a printer reported. */
  readonly modelName?: ((model: string) => string | undefined) | undefined;
}

// ponytail: a fixed cap, newest kept. A list a person can page back through is
// the job of a stored history, not of this in-memory one.
const KEPT = 100;

/** The bell's list. A shared store, so a class (svelte-5.md). */
export class Notices {
  readonly #onNotice: ((notice: Notice) => void) | undefined;
  readonly #now: () => number;
  readonly #admitsOnArrival: () => boolean;
  readonly #modelName: (model: string) => string | undefined;

  // Newest first, replaced whole on each change (svelte-5.md).
  #items = $state.raw<readonly Notice[]>([]);
  // The issue tokens each printer carried at the last reading, so only a new
  // one is announced. Nothing renders from it.
  // eslint-disable-next-line svelte/prefer-svelte-reactivity
  #known = new Map<string, ReadonlySet<string>>();
  // The devices approved while the page was open and not arrived yet, with
  // the name to announce them by. Only their first arrival is news: a
  // printer that comes back after a restart is not a printer that joined.
  // eslint-disable-next-line svelte/prefer-svelte-reactivity
  #awaiting = new Map<string, string>();
  // The refused printers already announced, by the digest of what they
  // presented, so one printer calling again is one notice. Nothing renders
  // from it.
  // eslint-disable-next-line svelte/prefer-svelte-reactivity
  #refused = new Set<string>();
  #count = 0;

  constructor(options: NoticesOptions = {}) {
    this.#onNotice = options.onNotice;
    this.#now = options.now ?? Date.now;
    this.#admitsOnArrival = options.admitsOnArrival ?? (() => false);
    this.#modelName = options.modelName ?? (() => undefined);
  }

  /** Read-only by design: the stream is what adds to it. */
  get items(): readonly Notice[] {
    return this.#items;
  }

  /**
   * Listens to the events the fleet applies, after it has applied each one,
   * so a printer's row already carries what the event changed.
   */
  watch(fleet: NoticeFleet): () => void {
    return fleet.subscribe((event) => {
      if (event.type === RESOURCE_STATE_CHANGED) {
        // The server announces a switch-off once; a replayed frame names a
        // module whose notice is still unread and adds nothing.
        this.#add(
          moduleNoticesOf(event).filter(
            (draft) =>
              !this.#items.some(
                (item) =>
                  item.kind === draft.kind &&
                  item.module === draft.module &&
                  !item.read,
              ),
          ),
        );

        return;
      }

      const change = registrationChangeOf(event);
      if (change !== undefined) {
        this.#registration(change, fleet);

        return;
      }

      const id = deviceIdOf(event);
      const device = id === undefined ? undefined : fleet.device(id);
      const drafts = noticesOf(
        event,
        device,
        id === undefined ? undefined : this.#known.get(id),
      );
      if (id !== undefined && device !== undefined) {
        this.#known.set(id, issuesOf(device));
      }
      this.#add(drafts);
      if (event.type === ARRIVED && id !== undefined) this.#arrived(id, device);
    });
  }

  /**
   * Takes the printers' current issues as already seen. A host calls it once
   * the fleet is read: an issue a printer already had when the page opened is
   * not news.
   */
  seed(devices: readonly Device[]): void {
    for (const device of devices) this.#known.set(device.id, issuesOf(device));
  }

  toggleRead(id: string): void {
    this.#items = this.#items.map((item) =>
      item.id === id ? { ...item, read: !item.read } : item,
    );
  }

  markAllRead(): void {
    this.#items = this.#items.map((item) =>
      item.read ? item : { ...item, read: true },
    );
  }

  /** Forgets every notice, for a person who signed out. */
  reset(): void {
    this.#items = [];
    this.#known.clear();
    this.#awaiting.clear();
    this.#refused.clear();
  }

  #nameOf(model: string | undefined): string {
    if (model === undefined) return '';

    return this.#modelName(model) ?? model;
  }

  #registration(change: RegistrationChange, fleet: NoticeFleet): void {
    switch (change.kind) {
      case 'announced':
        if (this.#admitsOnArrival()) return;
        if (this.#items.some((item) => item.code === change.code)) return;
        this.#add([
          {
            kind: 'printer-pending',
            code: change.code,
            printer: this.#nameOf(change.model),
          },
        ]);

        return;
      case 'approved': {
        this.#decided(change.code);
        if (change.deviceId === undefined) return;
        this.#awaiting.set(change.deviceId, this.#nameOf(change.model));
        // The stream folds frames from different origins over a window, so
        // the printer's arrival can be delivered before its approval. A
        // printer already connected has arrived.
        const device = fleet.device(change.deviceId);
        if (device?.connected === true) this.#arrived(change.deviceId, device);

        return;
      }
      case 'rejected':
        this.#decided(change.code);

        return;
      case 'refused': {
        // A caller that presented no identity is not a printer: a browser
        // tab or a port scanner knocking on the wire.
        if (change.digest === undefined) return;
        // One notice per printer for as long as the page is open. A refused
        // printer calls again every minute, and the bell is for news.
        if (this.#refused.has(change.digest)) return;
        this.#refused.add(change.digest);
        // The digests are the caller's to choose, so the set is bounded the
        // way the list is.
        if (this.#refused.size > KEPT) {
          const oldest = this.#refused.values().next().value;
          if (oldest !== undefined) this.#refused.delete(oldest);
        }
        this.#add([
          {
            kind: 'printer-refused',
            printer: this.#nameOf(change.model),
            reason: change.reason,
          },
        ]);

        return;
      }
    }
  }

  /**
   * A code somebody decided is no longer waiting, so its notice is marked
   * read: it stays in the list as what happened, and stops asking.
   */
  #decided(code: string): void {
    if (!this.#items.some((item) => item.code === code && !item.read)) return;
    this.#items = this.#items.map((item) =>
      item.code === code && !item.read ? { ...item, read: true } : item,
    );
  }

  #arrived(deviceId: string, device: Device | undefined): void {
    const model = this.#awaiting.get(deviceId);
    if (model === undefined) return;
    this.#awaiting.delete(deviceId);
    const name = device?.name;
    this.#add([
      {
        kind: 'printer-connected',
        deviceId,
        printer: name === undefined || name === '' ? model : name,
      },
    ]);
  }

  #add(drafts: readonly NoticeDraft[]): void {
    if (drafts.length === 0) return;

    const at = this.#now();
    const fresh = drafts.map((draft) => {
      this.#count += 1;

      return { ...draft, id: String(this.#count), at, read: false };
    });
    const newestFirst = [...fresh].reverse();
    this.#items = [...newestFirst, ...this.#items].slice(0, KEPT);
    for (const notice of newestFirst) this.#onNotice?.(notice);
  }
}
