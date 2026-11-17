// The dashboards a person sees, the one on screen, and every write to them.
//
// A class, because the home page, the switcher, the editor and a wall screen
// share it (svelte-5.md). The client comes in through the constructor, which
// is what lets a test drive it with no network.
//
// Two reads, each with its own loading, fault and answer, because a page that
// could not read one still draws the other (svelte-data.md):
//
//   the list       every dashboard the person sees, and which is the default
//   the dashboard  the one on screen, with every card
//
// Both are `$state.raw` and replaced whole on every read. A fault is kept as
// the value that was thrown, because its code is what a translation is keyed
// by (frontend-i18n.md rule 5).
import type { StreamEvent } from '@prusa/connect';

import { faultOf } from './fault';
import {
  createDashboard,
  type Dashboard,
  type DashboardDraftBody,
  type DashboardRow,
  type DashboardsClient,
  deleteDashboard,
  readDashboard,
  readDashboards,
  replaceDashboard,
  setDefaultDashboard,
} from './services/dashboards';

/** What a person may do with dashboards, as the core answered it. */
export interface DashboardRights {
  /** Make, change and delete a dashboard: operate_resource on the kind. */
  readonly write: boolean;
  /** Mark the default, and show a dashboard to administrators only. */
  readonly configure: boolean;
}

const NOTHING: DashboardRights = { write: false, configure: false };

/** The core's answer for a dashboard that is not there, or no longer is. */
const GONE = 'core.resource/DASHBOARD_NOT_FOUND';

/** The event the core publishes when a dashboard changes (ADR-20261008-sww rule 31). */
export const DASHBOARD_EVENTS = [
  'com.prusa.connect.dashboard.changed',
] as const;

/** What the store needs of the core: the dashboard routes and the access check. */
export type DashboardsStoreClient = DashboardsClient & {
  checkAccess: (
    request: {
      body: { object_type: 'resource_kind'; object_id: string; action: string };
    },
    signal?: AbortSignal,
  ) => Promise<{ allowed: boolean }>;
};

/** The resource kind dashboards are authorized through (ADR-20261008-sww rule 7). */
const KIND = 'dashboard';

export class Dashboards {
  readonly #client: DashboardsStoreClient;

  #rights = $state.raw<DashboardRights>(NOTHING);
  #rightsRead = $state(false);

  #rows = $state.raw<readonly DashboardRow[]>([]);
  #defaultId = $state<number | null>(null);
  #listed = $state(false);
  #listFault = $state.raw<unknown>(undefined);
  #listAbort: AbortController | undefined;

  #dashboard = $state.raw<Dashboard | undefined>(undefined);
  #dashboardId = $state<number | undefined>(undefined);
  #loading = $state(false);
  #fault = $state.raw<unknown>(undefined);
  #abort: AbortController | undefined;
  /** The poll on its way, dropped by every open, save, create and close. */
  #pollAbort: AbortController | undefined;

  #writing = $state(0);
  #writeFault = $state.raw<unknown>(undefined);

  /** The first read of the frame, so two readers asking make one request. */
  #framing: Promise<void> | undefined;
  /** Counts the resets. An answer to a request made before one is dropped. */
  #resets = 0;

  constructor(client: DashboardsStoreClient) {
    this.#client = client;
  }

  /** What the person may do. Nothing until the core has answered. */
  get rights(): DashboardRights {
    return this.#rights;
  }

  /** True once the core answered what the person may do. */
  get rightsRead(): boolean {
    return this.#rightsRead;
  }

  /** Every dashboard the person sees, by name. Read-only by design. */
  get rows(): readonly DashboardRow[] {
    return this.#rows;
  }

  /** The id of the default dashboard, when the person sees one. */
  get defaultId(): number | null {
    return this.#defaultId;
  }

  /** False until the first list answered, which is not an empty list. */
  get listed(): boolean {
    return this.#listed;
  }

  /** Why the list could not be read. */
  get listFault(): unknown {
    return this.#listFault;
  }

  /** The dashboard on screen, as the core last answered it. */
  get dashboard(): Dashboard | undefined {
    return this.#dashboard;
  }

  /** The id of the dashboard asked for, which may still be on its way. */
  get dashboardId(): number | undefined {
    return this.#dashboardId;
  }

  /** Why the dashboard asked for could not be read. */
  get fault(): unknown {
    return this.#fault;
  }

  /** A write is on its way, and the controls that start one wait for it. */
  get busy(): boolean {
    return this.#writing > 0;
  }

  /** What the last write was refused, until a person dismisses it. */
  get writeFault(): unknown {
    return this.#writeFault;
  }

  /** Forgets the last refusal. */
  dismiss(): void {
    this.#writeFault = undefined;
  }

  /**
   * Reads the list and what the person may do. A second call while the first
   * is on its way is given the first. A list that could not be read is asked
   * for again by the next call.
   */
  frame(): Promise<void> {
    this.#framing ??= Promise.all([this.#readRights(), this.refresh()]).then(
      () => {
        if (!this.#listed) this.#framing = undefined;
      },
    );

    return this.#framing;
  }

  /** Reads the list again. A read still on its way is dropped. */
  async refresh(): Promise<void> {
    this.#listAbort?.abort();
    const abort = new AbortController();
    this.#listAbort = abort;

    try {
      const page = await readDashboards(this.#client, abort.signal);
      if (abort.signal.aborted) return;
      this.#rows = page.items;
      this.#defaultId = page.default_id ?? null;
      this.#listed = true;
      this.#listFault = undefined;
    } catch (thrown) {
      if (abort.signal.aborted) return;
      this.#listFault = thrown;
    } finally {
      if (this.#listAbort === abort) this.#listAbort = undefined;
    }
  }

  /**
   * Opens one dashboard. A read of another one still on its way is dropped,
   * so the answer on screen is always the one asked for last.
   */
  async open(id: number): Promise<void> {
    if (
      this.#dashboardId === id &&
      (this.#dashboard?.id === id || this.#loading)
    ) {
      return;
    }

    this.#stopReads();
    const abort = new AbortController();
    this.#abort = abort;
    this.#dashboardId = id;
    this.#loading = true;
    this.#fault = undefined;
    if (this.#dashboard?.id !== id) this.#dashboard = undefined;

    try {
      const dashboard = await readDashboard(this.#client, id, abort.signal);
      if (abort.signal.aborted) return;
      this.#dashboard = dashboard;
    } catch (thrown) {
      if (abort.signal.aborted) return;
      this.#fault = thrown;
    } finally {
      if (this.#abort === abort) {
        this.#loading = false;
        this.#abort = undefined;
      }
    }
  }

  /** Forgets the dashboard on screen, for a page that draws none. */
  close(): void {
    this.#stopReads();
    this.#dashboardId = undefined;
    this.#dashboard = undefined;
    this.#loading = false;
    this.#fault = undefined;
  }

  /**
   * Reads the dashboard on screen again. The answer replaces it only when its
   * revision is higher than the one shown, so a wall that reads it redraws
   * nothing until there is something new, and an answer older than a save
   * never takes the saved dashboard's place. A poll still on its way is
   * dropped by the next poll and by every open, save, create and close. A
   * dashboard the core no longer has is taken off the screen and its fault
   * kept, so the page says it is gone. Any other failure keeps what is shown.
   */
  async poll(): Promise<void> {
    const id = this.#dashboardId;
    if (id === undefined || this.#loading) return;

    this.#pollAbort?.abort();
    const abort = new AbortController();
    this.#pollAbort = abort;

    try {
      const dashboard = await readDashboard(this.#client, id, abort.signal);
      if (abort.signal.aborted || this.#dashboardId !== id) return;
      const shown = this.#dashboard?.revision;
      if (shown === undefined || dashboard.revision > shown) {
        this.#dashboard = dashboard;
      }
    } catch (thrown) {
      if (abort.signal.aborted || this.#dashboardId !== id) return;
      if (faultOf(thrown).code === GONE) {
        this.#dashboard = undefined;
        this.#fault = thrown;
      } else if (this.#dashboard === undefined) {
        this.#fault = thrown;
      }
    } finally {
      if (this.#pollAbort === abort) this.#pollAbort = undefined;
    }
  }

  /**
   * Takes one frame of the event stream. A change of the dashboard on screen
   * reads it again, and any change reads the list again (ADR-20261008-sww rule 31).
   * A dashboard being edited is read again too: the editor draws its draft,
   * and its save is refused as a change made first, which the page offers
   * to read again.
   */
  hear(event: StreamEvent): void {
    if (!(DASHBOARD_EVENTS as readonly string[]).includes(event.type)) return;
    const payload = event.payload;
    const id =
      typeof payload === 'object' && payload !== null
        ? (payload as { id?: unknown }).id
        : undefined;

    void this.refresh();
    if (typeof id === 'number' && id === this.#dashboardId) {
      void this.poll();
    }
  }

  /** Makes a dashboard, opens it, and answers it, or undefined when refused. */
  async create(body: DashboardDraftBody): Promise<Dashboard | undefined> {
    const made = await this.#write(() => createDashboard(this.#client, body));
    if (made === undefined) return undefined;

    // A dashboard still being read would land under the new id.
    this.#stopReads();
    this.#dashboardId = made.id;
    this.#dashboard = made;
    this.#fault = undefined;
    void this.refresh();

    return made;
  }

  /**
   * Writes a dashboard back, and answers whether the core took it. A body made
   * against an older revision is refused, and the refusal stays in
   * `writeFault` for the page to offer a fresh read.
   */
  async save(
    id: number,
    body: DashboardDraftBody & { readonly revision: number },
  ): Promise<boolean> {
    const saved = await this.#write(() =>
      replaceDashboard(this.#client, id, body),
    );
    if (saved === undefined) return false;

    if (this.#dashboardId === id) {
      // A poll that left before the save would answer the older revision.
      this.#pollAbort?.abort();
      this.#pollAbort = undefined;
      this.#dashboard = saved;
    }
    void this.refresh();

    return true;
  }

  /** Deletes a dashboard, and answers whether it is gone. */
  async remove(id: number): Promise<boolean> {
    const done = await this.#write(async () => {
      await deleteDashboard(this.#client, id);

      return true;
    });
    if (done === undefined) return false;

    if (this.#dashboardId === id) this.close();
    this.#rows = this.#rows.filter((row) => row.id !== id);
    if (this.#defaultId === id) this.#defaultId = null;
    void this.refresh();

    return true;
  }

  /** Marks one dashboard the default, or none, and answers whether it took. */
  async makeDefault(id: number | null): Promise<boolean> {
    const answer = await this.#write(async () => ({
      id: await setDefaultDashboard(this.#client, id),
    }));
    if (answer === undefined) return false;

    this.#defaultId = answer.id;
    void this.refresh();

    return true;
  }

  /** Forgets everything read for the person who left (person-stores.ts). */
  reset(): void {
    this.close();
    this.#listAbort?.abort();
    this.#listAbort = undefined;
    this.#rights = NOTHING;
    this.#rightsRead = false;
    this.#rows = [];
    this.#defaultId = null;
    this.#listed = false;
    this.#listFault = undefined;
    this.#writeFault = undefined;
    this.#framing = undefined;
    this.#resets += 1;
  }

  /** Reads the dashboard on screen again, dropping what was refused. */
  async reload(): Promise<void> {
    const id = this.#dashboardId;
    this.#writeFault = undefined;
    if (id === undefined) return;

    this.#dashboard = undefined;
    this.#dashboardId = undefined;
    await this.open(id);
  }

  /** Drops the open and the poll on their way, so neither answer lands. */
  #stopReads(): void {
    this.#abort?.abort();
    this.#abort = undefined;
    this.#pollAbort?.abort();
    this.#pollAbort = undefined;
    this.#loading = false;
  }

  async #write<T>(run: () => Promise<T>): Promise<T | undefined> {
    const resets = this.#resets;
    this.#writing += 1;
    this.#writeFault = undefined;

    try {
      const answer = await run();

      return resets === this.#resets ? answer : undefined;
    } catch (thrown) {
      if (resets === this.#resets) this.#writeFault = thrown;

      return undefined;
    } finally {
      this.#writing -= 1;
    }
  }

  async #readRights(): Promise<void> {
    const resets = this.#resets;
    const may = async (action: string): Promise<boolean> =>
      (
        await this.#client.checkAccess({
          body: { object_type: 'resource_kind', object_id: KIND, action },
        })
      ).allowed;

    try {
      const [write, configure] = await Promise.all([
        may('operate_resource'),
        may('configure_resource'),
      ]);
      if (resets !== this.#resets) return;
      this.#rights = { write, configure };
      this.#rightsRead = true;
    } catch {
      if (resets !== this.#resets) return;
      // A core that could not answer leaves every control away. It still
      // decides every write, so this only hides what would be refused.
      this.#rights = NOTHING;
      this.#rightsRead = true;
    }
  }
}
