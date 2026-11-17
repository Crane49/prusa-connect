// What the Slicing page shows, read from the module's routes: the slicers and
// the jobs, the newest 200 of them, which the page filters and orders itself. It reads both again every few seconds while the page is open,
// because a job and an install move on the server without telling the page.
import { describeFault, type FaultText } from '@prusa/connect-ui/i18n';

import type { JobList, SlicerApi, Slicers, SubmitBody } from './api';

/** How often the page reads the slicers and the jobs again. */
export const REFRESH_MS = 3000;

/** How a read of the server ended. */
export type Load = 'loading' | 'ready' | 'failed' | 'refused';

/** What the page draws, and what a story hands it in place of the server. */
export interface SlicerView {
  readonly load: Load;
  readonly fault: FaultText | undefined;
  readonly slicers: Slicers | undefined;
  readonly jobs: JobList | undefined;
  /** An action that failed, shown above the lists until the next one. */
  readonly actionFault: FaultText | undefined;
  /** unverified confirms a release that publishes no checksum. */
  install(version: string, unverified?: boolean): Promise<void>;
  /** Reads the GitHub release list again. */
  checkReleases(): Promise<void>;
  remove(version: string): Promise<void>;
  pin(version: string): Promise<void>;
  cancel(id: string): Promise<void>;
  /**
   * Queues a job that slices a project and answers its id, or undefined when
   * the server refused it. The refusal is the action fault.
   */
  submit(body: SubmitBody): Promise<string | undefined>;
}

const refusedStatus = 403;

const statusOf = (thrown: unknown): number | undefined =>
  typeof thrown === 'object' && thrown !== null && 'status' in thrown
    ? Number(thrown.status)
    : undefined;

export class SlicerStore implements SlicerView {
  load = $state<Load>('loading');
  fault = $state<FaultText | undefined>(undefined);
  slicers = $state<Slicers | undefined>(undefined);
  jobs = $state<JobList | undefined>(undefined);
  actionFault = $state<FaultText | undefined>(undefined);

  readonly #api: SlicerApi;
  #timer: ReturnType<typeof setInterval> | undefined;
  #controller = new AbortController();

  constructor(api: SlicerApi) {
    this.#api = api;
  }

  /** Reads both lists, and again every REFRESH_MS until dispose. */
  start(): void {
    void this.refresh();
    this.#timer = setInterval(() => void this.refresh(), REFRESH_MS);
  }

  /** Stops the reading and every request still open. */
  dispose(): void {
    clearInterval(this.#timer);
    this.#controller.abort();
  }

  async refresh(): Promise<void> {
    const signal = this.#controller.signal;
    try {
      const [slicers, jobs] = await Promise.all([
        this.#api.listSlicers({}, signal),
        this.#api.listJobs({ query: { limit: 200 } }, signal),
      ]);
      this.slicers = slicers;
      this.jobs = jobs;
      this.load = 'ready';
      this.fault = undefined;
    } catch (thrown) {
      if (signal.aborted) return;
      this.load = statusOf(thrown) === refusedStatus ? 'refused' : 'failed';
      this.fault = describeFault(thrown);
    }
  }

  async #act(action: () => Promise<unknown>): Promise<void> {
    try {
      await action();
      this.actionFault = undefined;
    } catch (thrown) {
      this.actionFault = describeFault(thrown);
    }
    await this.refresh();
  }

  install(version: string, unverified = false): Promise<void> {
    return this.#act(() =>
      this.#api.installSlicer(
        { body: { version, unverified } },
        this.#controller.signal,
      ),
    );
  }

  checkReleases(): Promise<void> {
    return this.#act(() =>
      this.#api.refreshSlicers({}, this.#controller.signal),
    );
  }

  remove(version: string): Promise<void> {
    return this.#act(() =>
      this.#api.removeSlicer({ path: { version } }, this.#controller.signal),
    );
  }

  pin(version: string): Promise<void> {
    return this.#act(() =>
      this.#api.pinSlicer({ body: { version } }, this.#controller.signal),
    );
  }

  cancel(id: string): Promise<void> {
    return this.#act(() =>
      this.#api.cancelJob({ path: { id } }, this.#controller.signal),
    );
  }

  async submit(body: SubmitBody): Promise<string | undefined> {
    let id: string | undefined;
    await this.#act(async () => {
      const job = await this.#api.submitJob({ body }, this.#controller.signal);
      id = job.id;
    });

    return id;
  }
}
