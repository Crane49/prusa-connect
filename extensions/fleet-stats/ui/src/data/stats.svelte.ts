import type { PrusaConnect } from '@prusa/connect';
import { type ReadonlyFleet, tagKey } from '@prusa/connect-ui/domain';
import type { ModuleHost } from '@prusa/connect-ui/slots';

import {
  type DeviceInfo,
  type JobStats,
  narrow,
  type Scope,
  type SettledJob,
  summarise,
  WHOLE_FLEET,
} from '../domain/job-stats';
import {
  CONTINUITY_LOST,
  type JobsReader,
  JOB_SETTLED,
  readSettledJobs,
} from './jobs';

const DAY_MS = 86_400_000;

/** The periods a person picks from. */
export const PERIODS = [7, 30, 90] as const;
export type Days = (typeof PERIODS)[number];

/** A period read from anywhere a person or a card wrote one, else seven days. */
export const daysOf = (value: unknown): Days =>
  PERIODS.find((days) => String(days) === String(value)) ?? 7;

export type StatsState =
  | { kind: 'loading' }
  | { kind: 'error'; thrown: unknown }
  | { kind: 'ready'; stats: JobStats };

export type StatsClient = JobsReader & Pick<PrusaConnect, 'events'>;

type Loaded =
  | { kind: 'loading' }
  | { kind: 'error'; thrown: unknown }
  | {
      kind: 'ready';
      jobs: SettledJob[];
      from: number;
      to: number;
    };

/** The jobs of one period, read once however many screens show it. */
class Period {
  #loaded = $state.raw<Loaded>({ kind: 'loading' });
  #attempt = 0;
  #inflight: AbortController | undefined;
  readonly #days: Days;
  readonly #client: StatsClient;
  readonly #now: () => number;

  constructor(days: Days, client: StatsClient, now: () => number) {
    this.#days = days;
    this.#client = client;
    this.#now = now;
  }

  get loaded(): Loaded {
    return this.#loaded;
  }

  /** Reads the period again. An answer to an earlier read is dropped. */
  reload(): void {
    this.#inflight?.abort();
    const inflight = new AbortController();
    this.#inflight = inflight;
    const attempt = ++this.#attempt;
    const to = this.#now();
    const from = to - this.#days * DAY_MS;
    if (this.#loaded.kind !== 'ready') this.#loaded = { kind: 'loading' };

    readSettledJobs(this.#client, { from, to }, inflight.signal).then(
      (jobs) => {
        if (attempt !== this.#attempt) return;
        this.#loaded = { kind: 'ready', jobs, from, to };
      },
      (thrown: unknown) => {
        if (attempt !== this.#attempt || inflight.signal.aborted) return;
        this.#loaded = { kind: 'error', thrown };
      },
    );
  }

  stop(): void {
    this.#inflight?.abort();
  }
}

/** One summary, kept while the jobs and the printers it was worked out from stay. */
interface Kept {
  loaded: Loaded;
  devices: readonly DeviceInfo[];
  state: StatsState;
}

/**
 * The fleet's statistics for this host's screens: the page, the dashboard
 * cards and the printer's card and section. Each period is read once and kept
 * fresh while anything shows it. A summary is worked out once per period and
 * scope, so twenty printer cards cost one.
 */
export class Stats {
  #days = $state<Days>(7);
  // Replaced whole when a period is added, which is what a screen waiting for
  // it reads. `svelte/reactivity` is not among what the host shares with a
  // bundle, so no SvelteMap.
  #periods = $state.raw<Partial<Record<Days, Period>>>({});
  // A memo the summaries are read through, written while a derived reads it.
  // Reactive, the write would be a mutation inside that derived.
  // eslint-disable-next-line svelte/prefer-svelte-reactivity
  readonly #kept = new Map<string, Kept>();
  readonly #client: StatsClient;
  readonly #fleet: ReadonlyFleet;
  readonly #now: () => number;

  // The fleet replaces its rows on every telemetry frame. Reading only a key
  // of the fields the statistics use keeps a summary from being worked out
  // again for a frame that changes none of them.
  readonly #deviceKey = $derived.by(() =>
    JSON.stringify(
      this.#fleet.devices.map(({ id, name, model, tags }) => [
        id,
        name,
        model,
        (tags ?? []).map(tagKey),
      ]),
    ),
  );
  readonly #devices = $derived.by(() =>
    (JSON.parse(this.#deviceKey) as [string, string, string, string[]][]).map(
      ([id, name, model, tags]): DeviceInfo => ({ id, name, model, tags }),
    ),
  );

  /** The page's period over the whole fleet, which a printer's card and section show. */
  readonly state: StatsState = $derived.by(() =>
    this.view(this.#days, WHOLE_FLEET),
  );

  constructor(
    client: StatsClient,
    fleet: ReadonlyFleet,
    signal: AbortSignal,
    now: () => number = Date.now,
  ) {
    this.#client = client;
    this.#fleet = fleet;
    this.#now = now;
    signal.addEventListener(
      'abort',
      () => {
        for (const period of Object.values(this.#periods)) period.stop();
      },
      { once: true },
    );
    this.want(this.#days);
    void this.#follow(signal);
  }

  /** The period the page shows, and a printer's card and section with it. */
  get days(): Days {
    return this.#days;
  }

  setDays(days: Days): void {
    this.#days = days;
    this.want(days);
  }

  /**
   * Starts reading a period a screen is about to show. A screen calls it when
   * it mounts and when a person picks another period; a period already read
   * is not read again.
   */
  want(days: Days): void {
    if (this.#periods[days] !== undefined) return;
    const period = new Period(days, this.#client, this.#now);
    this.#periods = { ...this.#periods, [days]: period };
    period.reload();
  }

  /** Reads every period on screen again. */
  reload(): void {
    for (const period of Object.values(this.#periods)) period.reload();
  }

  /** One period over the printers a scope counts, `loading` until `want` read it. */
  view(days: Days, scope: Scope): StatsState {
    const loaded = this.#periods[days]?.loaded ?? { kind: 'loading' };
    if (loaded.kind !== 'ready') return loaded;
    const devices = this.#devices;
    const tag = tagKey(scope.tag);
    const key = JSON.stringify([days, scope.printers, tag]);
    const kept = this.#kept.get(key);
    if (kept?.loaded === loaded && kept.devices === devices) return kept.state;

    const scoped = narrow(loaded.jobs, devices, {
      printers: scope.printers,
      tag,
    });
    const state: StatsState = {
      kind: 'ready',
      stats: summarise(scoped.jobs, scoped.devices, {
        from: loaded.from,
        to: loaded.to,
      }),
    };
    this.#kept.set(key, { loaded, devices, state });
    return state;
  }

  async #follow(signal: AbortSignal): Promise<void> {
    const subscription = this.#client.events({
      types: [JOB_SETTLED, CONTINUITY_LOST],
      signal,
    });
    try {
      // The subscription carries only the two types that call for a re-read.
      const events = subscription[Symbol.asyncIterator]();
      while (!(await events.next()).done) this.reload();
    } catch {
      // The SDK subscription reconnects by itself. A stream that throws anyway
      // leaves the last read on screen until the period is changed.
    }
  }
}

const shared = new WeakMap<ModuleHost, Stats>();

/** The one store this host's screens share. */
export const statsFor = (host: ModuleHost): Stats => {
  let stats = shared.get(host);
  if (stats === undefined) {
    // Built under its own root: the first screen to ask is not the owner, and
    // a derived owned by a screen goes inert when that screen is destroyed.
    let created!: Stats;
    const dispose = $effect.root(() => {
      created = new Stats(host.client, host.fleet, host.signal);
    });
    host.signal.addEventListener('abort', dispose, { once: true });
    stats = created;
    shared.set(host, stats);
  }
  return stats;
};
