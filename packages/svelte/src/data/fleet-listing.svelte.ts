// The printers screen's page of the fleet, read from the server. The server
// owns the paging, filtering, search and order; this store holds the query and
// the page the server answered for it.
import type { components, PrusaConnect, StreamEvent } from '@prusa/connect';
import { untrack } from 'svelte';

import type { Device } from '../domain/command';
import {
  asError,
  deviceIdOf,
  FLEET_JOB_EVENTS,
  FLEET_TRANSFER_EVENTS,
  readingOf,
  withReading,
} from './fleet.svelte';
import { DEVICE_FILES_EVENTS } from './services/device';

type DeviceFilter = components['schemas']['DeviceFilter'];
type DeviceSort = components['schemas']['DeviceSort'];
type DeviceIssueSummary = components['schemas']['DeviceIssueSummary'];

export type ListingOrder = 'asc' | 'desc';

export interface FleetListingQuery {
  page: number;
  pageSize: number;
  /** The selected values per filter, keyed by the filter's query parameter. */
  filters: Readonly<Record<string, readonly string[]>>;
  search: string;
  sort: string | undefined;
  order: ListingOrder | undefined;
  /** Whether retired printers are left out (the default), added, or the only ones. */
  retired?: 'exclude' | 'include' | 'only';
}

export type FleetListingClient = Pick<PrusaConnect, 'listDevices'>;

type ListRequest = NonNullable<
  Parameters<FleetListingClient['listDevices']>[0]
>;
type ListQuery = NonNullable<ListRequest['query']>;

export interface FleetListingOptions {
  debounceMs?: number;
}

type SortField = NonNullable<ListQuery['sort']>;

// Keyed by the SDK's sort enum, so a field the server adds fails the build here.
const SORT_FIELDS: Record<SortField, true> = {
  urgency: true,
  name: true,
  model: true,
  state: true,
  last_seen: true,
  id: true,
};

const isSortField = (value: string | undefined): value is SortField =>
  value !== undefined && Object.hasOwn(SORT_FIELDS, value);

const DEFAULT_DEBOUNCE_MS = 300;
const MULTI_FILTERS = ['state', 'model', 'severity', 'tag'] as const;

const sameFilters = (
  a: FleetListingQuery['filters'],
  b: FleetListingQuery['filters'],
): boolean => JSON.stringify(a) === JSON.stringify(b);

export class FleetListing {
  readonly #client: FleetListingClient;
  readonly #debounceMs: number;
  #items = $state<readonly Device[]>([]);
  #total = $state(0);
  #filters = $state<readonly DeviceFilter[]>([]);
  #sorts = $state<readonly DeviceSort[]>([]);
  #sort = $state<string | undefined>(undefined);
  #order = $state<ListingOrder | undefined>(undefined);
  #issues = $state<DeviceIssueSummary | undefined>(undefined);
  #loading = $state(false);
  #loaded = $state(false);
  #error = $state<Error | undefined>(undefined);
  #query = $state<FleetListingQuery>({
    page: 0,
    pageSize: 50,
    filters: {},
    search: '',
    sort: undefined,
    order: undefined,
  });
  #abort: AbortController | undefined;
  #timer: ReturnType<typeof setTimeout> | undefined;
  #started = false;

  constructor(client: FleetListingClient, options: FleetListingOptions = {}) {
    this.#client = client;
    this.#debounceMs = options.debounceMs ?? DEFAULT_DEBOUNCE_MS;
  }

  get items(): readonly Device[] {
    return this.#items;
  }
  get total(): number {
    return this.#total;
  }
  get filters(): readonly DeviceFilter[] {
    return this.#filters;
  }
  get sorts(): readonly DeviceSort[] {
    return this.#sorts;
  }
  get sort(): string | undefined {
    return this.#sort;
  }
  get order(): ListingOrder | undefined {
    return this.#order;
  }
  get issues(): DeviceIssueSummary | undefined {
    return this.#issues;
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
  get query(): FleetListingQuery {
    return this.#query;
  }

  /** Merges `next` into the query and reads. A change of what is listed returns to the first page. */
  setQuery(next: Partial<FleetListingQuery>): void {
    // Untracked, so a caller's `$effect` does not subscribe to the state this writes.
    untrack(() => {
      this.#apply(next);
    });
  }

  #apply(next: Partial<FleetListingQuery>): void {
    const before = this.#query;
    const merged = { ...before, ...next };
    const narrowed =
      !sameFilters(before.filters, merged.filters) ||
      before.search !== merged.search ||
      before.sort !== merged.sort ||
      before.order !== merged.order ||
      before.retired !== merged.retired;
    this.#query = {
      ...merged,
      page: next.page === undefined && narrowed ? 0 : merged.page,
    };
    this.#cancelTimer();
    void this.#read();
  }

  /**
   * Applies one event the whole-fleet store applied. A telemetry frame patches
   * the row on the page, a transfer or a folder read changes nothing on it,
   * and any other event reads the page again once the burst is over. It never
   * throws: the caller is the loop that feeds the live stream.
   */
  hear(event: StreamEvent): void {
    try {
      if (!this.#started) return;
      if (FLEET_TRANSFER_EVENTS.includes(event.type)) return;
      if (DEVICE_FILES_EVENTS.includes(event.type)) return;
      if (FLEET_JOB_EVENTS.includes(event.type)) {
        this.#patch(event);

        return;
      }
      this.#cancelTimer();
      this.#timer = setTimeout(() => {
        this.#timer = undefined;
        void this.#read();
      }, this.#debounceMs);
    } catch (thrown) {
      this.#error = asError(thrown);
    }
  }

  /** Cancels a pending refetch and an in-flight read. */
  close(): void {
    this.#cancelTimer();
    this.#abort?.abort();
    this.#abort = undefined;
    this.#loading = false;
  }

  #cancelTimer(): void {
    clearTimeout(this.#timer);
    this.#timer = undefined;
  }

  #patch(event: StreamEvent): void {
    const id = deviceIdOf(event);
    const reading = readingOf(event.payload);
    if (id === undefined || reading === undefined) return;

    const at = this.#items.findIndex((row) => row.id === id);
    const row = this.#items[at];
    if (row === undefined) return;

    const next = [...this.#items];
    next[at] = withReading(row, reading);
    this.#items = next;
  }

  #requestOf(): ListQuery {
    const { page, pageSize, filters, search, sort, order, retired } =
      this.#query;
    const query: ListQuery = { limit: pageSize, offset: page * pageSize };
    for (const param of MULTI_FILTERS) {
      const values = filters[param];
      if (values !== undefined && values.length > 0) query[param] = [...values];
    }
    if (retired !== undefined && retired !== 'exclude') query.retired = retired;
    const text = search.trim();
    if (text !== '') query.search = text;

    // Before the first answer nothing is advertised, so only a field the SDK
    // knows goes out; once sorts are known, only an advertised one does.
    const advertised = this.#sorts.find((s) => s.field === sort)?.field;
    const known =
      this.#sorts.length === 0 && isSortField(sort) ? sort : advertised;
    if (known !== undefined) {
      query.sort = known;
      if (order !== undefined) query.order = order;
    }

    return query;
  }

  async #read(): Promise<void> {
    this.#started = true;
    this.#abort?.abort();
    const abort = new AbortController();
    this.#abort = abort;
    this.#loading = true;
    try {
      const page = await this.#client.listDevices(
        { query: this.#requestOf() },
        abort.signal,
      );
      if (abort.signal.aborted) return;

      const items = page.items ?? [];
      this.#items = items;
      this.#total = page.total;
      this.#filters = page.filters ?? [];
      this.#sorts = page.sorts ?? [];
      this.#sort = page.sort;
      this.#order = page.order === 'desc' ? 'desc' : 'asc';
      this.#issues = page.issues;
      this.#error = undefined;
      this.#loaded = true;
      this.#loading = false;

      const { page: at, pageSize } = this.#query;
      const last = Math.max(0, Math.ceil(page.total / pageSize) - 1);
      if (items.length === 0 && page.total > 0 && last < at) {
        this.#query = { ...this.#query, page: last };
        await this.#read();
      }
    } catch (thrown) {
      if (abort.signal.aborted) return;
      this.#error = asError(thrown);
      this.#loading = false;
    }
  }
}
