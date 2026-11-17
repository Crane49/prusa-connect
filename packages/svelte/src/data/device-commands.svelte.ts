// What a person sent to one printer and what it answered.
import { type DataFault, faultOf, NETWORK_FAULT, UNKNOWN_FAULT } from './fault';
import {
  type CommandLogClient,
  type CommandRecord,
  readCommand,
} from './services/command-log';
import {
  type CommandArgs,
  type CommandClient,
  type CommandResult,
  sendCommand,
} from './services/device';
import {
  type GcodeClient,
  gcodeLines,
  type GcodeStep,
  readGcodePresets,
  runGcodePreset,
  sendGcode,
} from './services/gcode';
import type { GcodePreset } from './services/gcode';
import { systemTimers, type Timers } from './timers';

/** What the command log needs of the SDK, and no more. */
export type DeviceCommandsClient = CommandClient &
  GcodeClient &
  Pick<CommandLogClient, 'getCommand'>;

/** One line of the log: what was sent, and what came back. */
export interface CommandEntry {
  /** A number this store gives each entry, so a list keys on it. */
  readonly id: number;
  /** `command` for a capability verb, `gcode` for one line of G-code. */
  readonly kind: 'command' | 'gcode';
  /** The verb, or the G-code line. */
  readonly sent: string;
  readonly ok: boolean;
  /** What the printer said back, when it said anything. */
  readonly answer: string | undefined;
  /** The core's code for a refusal, or the step's status for a G-code line. */
  readonly code: string | undefined;
  /** Why it was refused, in the words of the printer or the server. */
  readonly reason: string | undefined;
  /** The arguments the printer ran the command without. */
  readonly ignored: readonly string[];
  /** The arguments the printer replaced, and the value it used instead. */
  readonly substituted?: Readonly<Record<string, string>>;
  /**
   * How the command ended, in the words the command record uses: `succeeded`,
   * `refused`, `timed_out` and the rest. Absent when nothing said.
   */
  readonly status?: string | undefined;
  /** Nobody saw how the command ended, so the printer may have run it. */
  readonly unknown?: boolean;
  /** How long the command took, in milliseconds, when that is known. */
  readonly elapsedMs?: number | undefined;
  /** The sender reports the outcome itself, so no announcer repeats it. */
  readonly quiet?: boolean;
  /** When it was sent, in milliseconds since the epoch. */
  readonly at: number;
}

/**
 * What became of one verb. The printer accepted it or refused it, or it was
 * never sent because another command was already on its way. A refusal
 * carries its fault, whether the printer or the server gave it.
 */
export interface SendOutcome {
  readonly kind: 'accepted' | 'refused' | 'unsent';
  readonly fault?: DataFault | undefined;
}

/** These options change how one verb is sent. */
export interface SendOptions {
  /** The caller reports the outcome itself, so the entry is marked quiet. */
  readonly quiet?: boolean;
}

/** How the store is built beyond its client. */
export interface DeviceCommandsOptions {
  /** How many entries the log keeps. The oldest are dropped past it. */
  readonly limit?: number;
  /** The clock the store waits on between reads of an open command. */
  readonly timers?: Timers;
}

const DEFAULT_LIMIT = 100;

/**
 * The core writes a command record at most two seconds after the command ends,
 * so ten seconds of reads every half second reach it with room to spare.
 */
const POLL_MS = 500;
const POLL_FOR_MS = 10_000;

/** How long an accepted verb stays pending when the state never changes. */
const HOLD_MS = 10_000;

/** What the core answers for a command record it has not written yet. */
const NOT_FOUND = 404;

const textOf = (value: unknown): string | undefined =>
  typeof value === 'string' && value !== '' ? value : undefined;

type Outcome = Omit<CommandEntry, 'id' | 'at' | 'kind' | 'sent' | 'quiet'>;

/** The entry for a command whose send answered how it ended. */
const answered = (result: CommandResult, elapsedMs: number): Outcome => ({
  ok: result.ok,
  answer: textOf(result.answer),
  code: textOf(result.fault_code),
  reason: textOf(result.machine_reason),
  ignored: result.ignored ?? [],
  substituted: result.substituted ?? {},
  status: result.ok ? 'succeeded' : 'refused',
  elapsedMs,
});

/**
 * The entry for a command read back from its record. A record still open
 * when the reading stopped says as much as one whose outcome is unknown.
 */
const recorded = (record: CommandRecord): Outcome => ({
  ok: record.outcome_unknown !== true && record.ok,
  answer: undefined,
  code: textOf(record.fault_code),
  reason: textOf(record.machine_reason),
  ignored: [],
  status: record.status,
  unknown: record.outcome_unknown === true || record.status === undefined,
  elapsedMs: record.status === undefined ? undefined : record.elapsed_ms,
});

/**
 * A shared store, so a class (svelte-5.md). The client comes in through the
 * constructor, which lets a test pass a fake.
 *
 * The log is bounded. A console left open for an hour would otherwise keep
 * every line a person sent, and the page draws the newest few.
 */
export class DeviceCommands {
  readonly #client: DeviceCommandsClient;
  readonly #deviceId: string;
  readonly #limit: number;
  readonly #timers: Timers;
  #watching: AbortController | undefined;

  #log = $state.raw<readonly CommandEntry[]>([]);
  #presets = $state.raw<readonly GcodePreset[]>([]);
  #sending = $state(false);
  #pending = $state<string | undefined>(undefined);
  #state: string | undefined;
  #held:
    { readonly from: string | undefined; readonly timer: number } | undefined;
  #error = $state<DataFault | undefined>(undefined);
  #next = 1;

  constructor(
    client: DeviceCommandsClient,
    deviceId: string,
    options: DeviceCommandsOptions = {},
  ) {
    this.#client = client;
    this.#deviceId = deviceId;
    this.#limit = options.limit ?? DEFAULT_LIMIT;
    this.#timers = options.timers ?? systemTimers;
  }

  /** The printer this store is about. Read-only by design. */
  get deviceId(): string {
    return this.#deviceId;
  }

  /** The log, oldest first. Read-only by design (svelte-5.md). */
  get log(): readonly CommandEntry[] {
    return this.#log;
  }

  /** The stored presets a person runs by name. */
  get presets(): readonly GcodePreset[] {
    return this.#presets;
  }

  get sending(): boolean {
    return this.#sending;
  }

  /**
   * The verb under way, for a button to show a spinner on itself. It is set
   * while the send runs and, once the printer accepted the verb, until its
   * state changes (`observe`) or `HOLD_MS` passes, whichever comes first.
   */
  get pending(): string | undefined {
    return this.#pending;
  }

  /**
   * Tells the store the state the printer reports now. A verb the printer
   * accepted stops being pending once the state differs from the one it was
   * accepted in.
   */
  observe(state: string | undefined): void {
    if (this.#held !== undefined && state !== this.#held.from) this.#release();
    this.#state = state;
  }

  get error(): DataFault | undefined {
    return this.#error;
  }

  /** How many entries the log keeps. */
  get limit(): number {
    return this.#limit;
  }

  /** The last entry, which is the answer a console shows under the field. */
  readonly last: CommandEntry | undefined = $derived(
    this.#log[this.#log.length - 1],
  );

  /** What the printer said back to the last thing sent. */
  readonly answer: string | undefined = $derived(this.last?.answer);

  /** How many entries were refused. */
  readonly refused: number = $derived(
    this.#log.filter((entry) => !entry.ok).length,
  );

  /** Reads the stored presets. */
  async loadPresets(): Promise<void> {
    try {
      const page = await readGcodePresets(this.#client);
      this.#presets = page.items ?? [];
      this.#error = undefined;
    } catch (thrown) {
      this.#presets = [];
      this.#error = faultOf(thrown);
    }
  }

  /**
   * Sends one capability verb and writes how it ended into the log. A verb sent
   * while another is on its way is not sent, and answers unsent.
   *
   * The answer to the send is the outcome, except in two cases where the
   * command is still open on the server: the answer says another request
   * under the same key is running, or the send failed with the id of the
   * command it started. Then the command record is read until it settles, and
   * the store stays sending until then, because the command may still run.
   */
  async send(
    verb: string,
    args?: CommandArgs,
    options: SendOptions = {},
  ): Promise<SendOutcome> {
    if (this.#sending) return { kind: 'unsent' };

    const quiet = options.quiet === true;
    this.#release();
    this.#sending = true;
    this.#pending = verb;
    this.#error = undefined;
    const started = performance.now();
    try {
      const result = await sendCommand(
        this.#client,
        this.#deviceId,
        verb,
        args,
      );
      const open =
        result.duplicate === true ? textOf(result.command_id) : undefined;
      const record = open === undefined ? undefined : await this.#settle(open);
      const entry =
        record === undefined
          ? answered(result, performance.now() - started)
          : recorded(record);
      this.#append({ kind: 'command', sent: verb, quiet, ...entry });
      if (entry.ok) this.#hold();

      return entry.ok
        ? { kind: 'accepted' }
        : {
            kind: 'refused',
            fault: {
              code: entry.code ?? UNKNOWN_FAULT,
              traceId: undefined,
              status: undefined,
              params: {},
              message: entry.reason,
            },
          };
    } catch (thrown) {
      const fault = faultOf(thrown);
      this.#error = fault;
      const opened = fault.params.command_id;
      const record =
        typeof opened === 'string' && opened !== ''
          ? await this.#settle(opened)
          : undefined;
      this.#append({
        kind: 'command',
        sent: verb,
        quiet,
        ...(record === undefined
          ? {
              ok: false,
              answer: undefined,
              code: fault.code,
              reason: fault.message,
              ignored: [],
              // No answer arrived, so the request may have reached the printer.
              unknown: fault.code === NETWORK_FAULT,
            }
          : recorded(record)),
      });

      return { kind: 'refused', fault };
    } finally {
      this.#sending = false;
      if (this.#held === undefined) this.#pending = undefined;
    }
  }

  #hold(): void {
    this.#held = {
      from: this.#state,
      timer: this.#timers.set(() => {
        this.#release();
      }, HOLD_MS),
    };
  }

  #release(): void {
    if (this.#held !== undefined) this.#timers.clear(this.#held.timer);
    this.#held = undefined;
    if (!this.#sending) this.#pending = undefined;
  }

  /** Stops reading an open command, for a page moving to another printer. */
  abort(): void {
    this.#watching?.abort();
    this.#watching = undefined;
    this.#release();
  }

  /**
   * Reads one command until its record says how it ended, or until the
   * reading time runs out. A record the core has not written yet answers not
   * found and is read again. Any other refusal, such as a server that keeps
   * no command record, ends the reading. Answers the last record read.
   */
  async #settle(commandId: string): Promise<CommandRecord | undefined> {
    this.#watching?.abort();
    const watching = new AbortController();
    this.#watching = watching;
    const { signal } = watching;

    let last: CommandRecord | undefined;
    for (let waited = 0; ; waited += POLL_MS) {
      try {
        last = await readCommand(this.#client, commandId, signal);
        if (last.status !== undefined || last.outcome_unknown === true) {
          return last;
        }
      } catch (thrown) {
        if (signal.aborted || faultOf(thrown).status !== NOT_FOUND) return last;
      }
      if (waited >= POLL_FOR_MS || !(await this.#wait(signal))) return last;
    }
  }

  /** Waits one polling interval. Answers false when the wait was stopped. */
  #wait(signal: AbortSignal): Promise<boolean> {
    return new Promise((resolve) => {
      if (signal.aborted) {
        resolve(false);

        return;
      }
      const stop = (): void => {
        this.#timers.clear(handle);
        resolve(false);
      };
      const handle = this.#timers.set(() => {
        signal.removeEventListener('abort', stop);
        resolve(true);
      }, POLL_MS);
      signal.addEventListener('abort', stop, { once: true });
    });
  }

  /**
   * Sends one or more G-code lines. Each line becomes one entry, so a person
   * reads which line stopped the run.
   */
  async sendGcode(text: string, force = false): Promise<void> {
    const lines = gcodeLines(text);
    if (lines.length === 0 || this.#sending) return;

    this.#sending = true;
    this.#error = undefined;
    try {
      const outcome = await sendGcode(
        this.#client,
        this.#deviceId,
        lines,
        force,
      );
      this.#appendSteps(outcome.steps ?? []);
    } catch (thrown) {
      const fault = faultOf(thrown);
      this.#error = fault;
      for (const line of lines) {
        this.#append({
          kind: 'gcode',
          sent: line,
          ok: false,
          answer: undefined,
          code: fault.code,
          reason: fault.message,
          ignored: [],
        });
      }
    } finally {
      this.#sending = false;
    }
  }

  /** Runs one stored preset and writes its steps into the log. */
  async runPreset(name: string): Promise<void> {
    if (this.#sending) return;

    this.#sending = true;
    this.#error = undefined;
    try {
      const outcome = await runGcodePreset(this.#client, this.#deviceId, name);
      this.#appendSteps(outcome.steps ?? []);
    } catch (thrown) {
      this.#error = faultOf(thrown);
    } finally {
      this.#sending = false;
    }
  }

  /** Empties the log. */
  clear(): void {
    this.#log = [];
  }

  #appendSteps(steps: readonly GcodeStep[]): void {
    for (const step of steps) {
      this.#append({
        kind: 'gcode',
        sent: step.gcode,
        ok: step.status === 'accepted',
        answer: textOf(step.answer),
        code: textOf(step.code) ?? step.status,
        reason: textOf(step.machine_reason) ?? textOf(step.detail),
        ignored: [],
      });
    }
  }

  #append(entry: Omit<CommandEntry, 'id' | 'at'>): void {
    const written: CommandEntry = {
      quiet: false,
      ...entry,
      id: this.#next,
      at: Date.now(),
    };
    this.#next += 1;
    const next = [...this.#log, written];
    this.#log = next.length > this.#limit ? next.slice(-this.#limit) : next;
  }
}

/** One store per printer, built where the application builds its `PrusaConnect`. */
export const createDeviceCommands = (
  client: DeviceCommandsClient,
  deviceId: string,
  options?: DeviceCommandsOptions,
): DeviceCommands => new DeviceCommands(client, deviceId, options);
