// The time of day, for every tile that shows one.
//
// One timer for the whole page, whatever number of clocks are on it. A tile
// asks for the clock when it mounts and lets it go when it unmounts, and the
// timer runs only while somebody holds it. A class, because every clock tile
// shares it (svelte-5.md).
import { untrack } from 'svelte';

/** What the clock needs of the browser's timers. A test hands in its own. */
export interface Ticker {
  now(): number;
  every(run: () => void, ms: number): () => void;
}

export const browserTicker: Ticker = {
  now: () => Date.now(),
  every: (run, ms) => {
    const handle = setInterval(run, ms);

    return () => {
      clearInterval(handle);
    };
  },
};

/** How often the clock moves. A second, so a clock with seconds is right. */
const TICK_MS = 1000;

/** The first instant of the minute an instant falls in. */
const minuteOf = (at: number): number => at - (at % 60_000);

export class Clock {
  readonly #ticker: Ticker;
  #now = $state(0);
  #minute = $state(0);
  #holders = 0;
  #stop: (() => void) | undefined;

  constructor(ticker: Ticker) {
    this.#ticker = ticker;
    this.#set(ticker.now());
  }

  /** The time as it was at the last tick, in milliseconds. */
  get now(): number {
    return this.#now;
  }

  /**
   * The time at the start of the current minute. A reader that writes a time
   * of day to the minute reads this, and is redrawn once a minute rather than
   * once a second.
   */
  get minute(): number {
    return this.#minute;
  }

  /** Starts the clock for one more holder, and answers the call that lets it go. */
  hold(): () => void {
    // Taken outside any reader's reactive graph, so an effect that holds the
    // clock never reruns because the clock moved.
    return untrack(() => this.#hold());
  }

  #hold(): () => void {
    this.#holders += 1;
    if (this.#stop === undefined) {
      this.#set(this.#ticker.now());
      this.#stop = this.#ticker.every(() => {
        this.#set(this.#ticker.now());
      }, TICK_MS);
    }

    let held = true;

    return () => {
      if (!held) return;
      held = false;
      this.#holders -= 1;
      if (this.#holders === 0) {
        this.#stop?.();
        this.#stop = undefined;
      }
    };
  }

  #set(at: number): void {
    this.#now = at;
    const minute = minuteOf(at);
    if (minute !== this.#minute) this.#minute = minute;
  }
}
