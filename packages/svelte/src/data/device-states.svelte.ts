import type { StateTransition } from '../domain';
import {
  type DeviceStateKind,
  type DeviceStatesClient,
  readDeviceStates,
} from './services/device-states';

/**
 * The states each device kind's adapter declared, and the moves between them.
 *
 * A screen asks this for the moves a kind publishes for a verb, so it can say
 * which states a printer takes that verb from instead of guessing. The
 * vocabulary belongs to the server and not to the build, so it is read once
 * per session and kept.
 *
 * A shared store, so a class (svelte-5.md). The client comes in through the
 * constructor, which is what lets a test hand it a fake and what lets the host
 * hand it its own.
 *
 * A server that cannot answer leaves the list empty. Every reader treats an
 * empty list as a kind that declared no moves, which is the same answer a
 * server with no adapter of that kind gives, and the screens fall back to
 * their own rule.
 */
export class DeviceStates {
  readonly #client: DeviceStatesClient;
  #kinds = $state.raw<readonly DeviceStateKind[]>([]);
  #asked = $state(false);
  #error = $state<Error | undefined>(undefined);
  #flight: Promise<void> | undefined;

  constructor(client: DeviceStatesClient) {
    this.#client = client;
  }

  /** Every kind the server declared, in the order it published them. */
  get kinds(): readonly DeviceStateKind[] {
    return this.#kinds;
  }

  /** True once a read has finished, whether it answered or failed. */
  get asked(): boolean {
    return this.#asked;
  }

  /** Why the read failed, when it did. */
  get error(): Error | undefined {
    return this.#error;
  }

  /**
   * The moves the named kinds declared, with a move named twice kept once.
   *
   * It takes a list because a fleet holds machines of more than one kind and
   * the picker is handed one table for all of them. A kind the server said
   * nothing about contributes nothing.
   */
  transitionsOf(kinds: readonly string[]): readonly StateTransition[] {
    const moves: StateTransition[] = [];
    // A server declares a handful of kinds and a kind a dozen moves, so the
    // list of what has been taken is scanned rather than indexed.
    const taken: string[] = [];

    for (const kind of this.#kinds) {
      if (!kinds.includes(kind.kind)) continue;

      for (const move of kind.transitions ?? []) {
        const spelled = `${move.from}\u0000${move.to}\u0000${move.verb ?? ''}`;
        if (taken.includes(spelled)) continue;

        taken.push(spelled);
        moves.push(move);
      }
    }

    return moves;
  }

  /**
   * Reads the vocabulary. A second call while the first is in flight joins it
   * rather than making a second request (svelte-data.md), and a call after one
   * has landed asks again, because a module installed since would add a kind.
   */
  read(): Promise<void> {
    this.#flight ??= this.#read().finally(() => {
      this.#flight = undefined;
    });

    return this.#flight;
  }

  async #read(): Promise<void> {
    try {
      const answer = await readDeviceStates(this.#client);
      this.#kinds = answer.items ?? [];
      this.#error = undefined;
    } catch (thrown) {
      // A server that cannot answer this still draws every screen, so the
      // list stays empty and the fault is kept for a reader that wants it.
      this.#kinds = [];
      this.#error =
        thrown instanceof Error ? thrown : new Error(String(thrown));
    } finally {
      this.#asked = true;
    }
  }
}
