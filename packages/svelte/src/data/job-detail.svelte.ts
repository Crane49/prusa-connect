// One print, found by the id the address carries.
//
// The id is a UUID the core gives each print, so it names nothing else: the
// printer the print ran on is the print's own device_id. One read answers it,
// whether the print is running or ended long ago.
import { type DataFault, faultOf } from './fault';
import type { JobClient, PrintJob } from './services/jobs';
import { readJob } from './services/jobs';

/** What this store needs of the SDK, and no more. */
export type JobDetailClient = JobClient;

/**
 * The refusal that means the server holds no print the caller may see under
 * that id. It is an answer about the address and not a failure of the read.
 */
const MISSING = 'core.printer/JOB_NOT_FOUND';

/**
 * A shared store, so a class (svelte-5.md). The client comes in through the
 * constructor, which lets a test hand it a fake.
 */
export class JobDetail {
  readonly #client: JobDetailClient;
  readonly #jobId: string;

  #job = $state.raw<PrintJob | undefined>(undefined);
  #loaded = $state(false);
  #error = $state<DataFault | undefined>(undefined);
  #flight: Promise<void> | undefined;

  constructor(client: JobDetailClient, jobId: string) {
    this.#client = client;
    this.#jobId = jobId;
  }

  /** The print the address names. Undefined until the read has answered. */
  get job(): PrintJob | undefined {
    return this.#job;
  }

  /** True once the read has answered, which tells a missing print from an unread one. */
  get loaded(): boolean {
    return this.#loaded;
  }

  /** The refusal, as a code and never as a sentence. */
  get error(): DataFault | undefined {
    return this.#error;
  }

  /** A second call while one read is in flight is handed that read (svelte-data.md). */
  load(): Promise<void> {
    if (this.#flight) return this.#flight;

    const flight = this.#read().finally(() => {
      if (this.#flight === flight) this.#flight = undefined;
    });
    this.#flight = flight;

    return flight;
  }

  async #read(): Promise<void> {
    try {
      this.#job = await readJob(this.#client, this.#jobId);
      this.#loaded = true;
      this.#error = undefined;
    } catch (thrown) {
      const fault = faultOf(thrown);
      this.#job = undefined;

      if (fault.code === MISSING) {
        this.#loaded = true;
        this.#error = undefined;

        return;
      }

      this.#error = fault;
    }
  }
}
