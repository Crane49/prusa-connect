// One printer's command record, newest first, a page at a time, and the
// lifecycle of one command read again when a person opens it.
//
// A listed row is the record as it stood when the page was read. A command
// still open then may have settled since, so opening a row reads it by id.
import { type DataFault, faultOf } from './fault';
import {
  type CommandLogClient,
  type CommandRecord,
  readCommand,
  readDeviceCommands,
} from './services/command-log';

/** One printer's stored commands. A shared store, so a class (svelte-5.md). */
export class DeviceCommandLog {
  readonly #client: CommandLogClient;
  readonly #deviceId: string;

  #rows = $state.raw<readonly CommandRecord[]>([]);
  #cursor = $state('');
  #loading = $state(false);
  #loaded = $state(false);
  #error = $state<DataFault | undefined>(undefined);
  // Replaced whole on each answer, keyed by command id.
  #opened = $state.raw<Readonly<Record<string, CommandRecord | DataFault>>>({});

  #flight: Promise<void> | undefined;
  #reading: AbortController | undefined;

  constructor(client: CommandLogClient, deviceId: string) {
    this.#client = client;
    this.#deviceId = deviceId;
  }

  /** Read-only by design: the collection is the server's (svelte-5.md). */
  get rows(): readonly CommandRecord[] {
    return this.#rows;
  }

  get loading(): boolean {
    return this.#loading;
  }

  /** Whether a read has finished, whether it answered rows or a refusal. */
  get loaded(): boolean {
    return this.#loaded;
  }

  get error(): DataFault | undefined {
    return this.#error;
  }

  /** Whether an older page waits behind the ones read. */
  get more(): boolean {
    return this.#cursor !== '';
  }

  /** The command as its own read answered, once that read has answered. */
  record(commandId: string): CommandRecord | undefined {
    const one = this.#opened[commandId];

    return one !== undefined && 'command_id' in one ? one : undefined;
  }

  /** Why the read of one command was refused. */
  recordError(commandId: string): DataFault | undefined {
    const one = this.#opened[commandId];

    return one !== undefined && !('command_id' in one) ? one : undefined;
  }

  /** Reads the newest page, dropping whatever was held. */
  load(): Promise<void> {
    return this.#read('', true);
  }

  /** Appends the older page the last cursor points at. */
  loadMore(): Promise<void> {
    if (this.#cursor === '') return Promise.resolve();

    return this.#read(this.#cursor, false);
  }

  /** Reads one command by id, for the lifecycle of a row a person opened. */
  async open(commandId: string): Promise<void> {
    let answer: CommandRecord | DataFault;
    try {
      answer = await readCommand(this.#client, commandId);
    } catch (thrown) {
      answer = faultOf(thrown);
    }
    this.#opened = { ...this.#opened, [commandId]: answer };
  }

  /** Abandons a read in flight, for a page moving to another printer. */
  abort(): void {
    this.#reading?.abort();
    this.#reading = undefined;
    this.#flight = undefined;
  }

  #read(cursor: string, fresh: boolean): Promise<void> {
    // A second call while one read is in flight is given that read.
    if (this.#flight) return this.#flight;

    const reading = new AbortController();
    this.#reading = reading;
    this.#loading = true;

    const flight = this.#fetch(cursor, fresh, reading.signal).finally(() => {
      if (this.#flight === flight) {
        this.#flight = undefined;
        this.#loading = false;
        this.#loaded = true;
      }
      if (this.#reading === reading) this.#reading = undefined;
    });
    this.#flight = flight;

    return flight;
  }

  async #fetch(
    cursor: string,
    fresh: boolean,
    signal: AbortSignal,
  ): Promise<void> {
    try {
      const answer = await readDeviceCommands(
        this.#client,
        this.#deviceId,
        cursor,
        signal,
      );
      const items = answer.items ?? [];

      this.#rows = fresh ? items : [...this.#rows, ...items];
      this.#cursor = answer.cursor;
      this.#error = undefined;
    } catch (thrown) {
      if (signal.aborted) return;
      this.#error = faultOf(thrown);
      if (fresh) this.#rows = [];
    }
  }
}
