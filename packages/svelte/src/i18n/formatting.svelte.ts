// How an instant and a temperature are written.

export const CLOCKS = ['24-hour', '12-hour'] as const;

export type Clock = (typeof CLOCKS)[number];

export const TEMPERATURE_UNITS = ['celsius', 'fahrenheit'] as const;

export type TemperatureUnit = (typeof TEMPERATURE_UNITS)[number];

/** The two `localStorage` keys these preferences live under. */
export const CLOCK_STORAGE_KEY = 'preferences.clock';
export const TEMPERATURE_STORAGE_KEY = 'preferences.temperature';

/** What the store needs of `localStorage`. */
export interface FormattingStorage {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
}

export interface FormattingStoreOptions {
  storage?: FormattingStorage | null;
}

const isClock = (value: unknown): value is Clock =>
  typeof value === 'string' && (CLOCKS as readonly string[]).includes(value);

const isUnit = (value: unknown): value is TemperatureUnit =>
  typeof value === 'string' &&
  (TEMPERATURE_UNITS as readonly string[]).includes(value);

// The DOM lib declares `localStorage` as always present.
const browser: { localStorage?: FormattingStorage } = globalThis;

const browserStorage = (): FormattingStorage | null => {
  try {
    return browser.localStorage ?? null;
  } catch {
    return null;
  }
};

const held = (
  storage: FormattingStorage | null,
  key: string,
): string | null => {
  try {
    return storage?.getItem(key) ?? null;
  } catch {
    return null;
  }
};

/** A shared store, so a class (svelte-5.md). */
export class FormattingStore {
  // 24-hour is unambiguous in every language this ships a catalogue for.
  #clock = $state<Clock>('24-hour');
  #temperature = $state<TemperatureUnit>('celsius');
  readonly #storage: FormattingStorage | null;

  constructor({ storage = browserStorage() }: FormattingStoreOptions = {}) {
    this.#storage = storage;

    const clock = held(storage, CLOCK_STORAGE_KEY);
    if (isClock(clock)) this.#clock = clock;

    const temperature = held(storage, TEMPERATURE_STORAGE_KEY);
    if (isUnit(temperature)) this.#temperature = temperature;
  }

  /** How a time of day is written. */
  get clock(): Clock {
    return this.#clock;
  }

  /** Rejects a word it does not know: the page stays as it is, nothing is stored. */
  set clock(next: Clock) {
    if (!isClock(next)) return;
    this.#clock = next;
    this.#remember(CLOCK_STORAGE_KEY, next);
  }

  /** What a reading is written in. Never what a command carries. */
  get temperature(): TemperatureUnit {
    return this.#temperature;
  }

  set temperature(next: TemperatureUnit) {
    if (!isUnit(next)) return;
    this.#temperature = next;
    this.#remember(TEMPERATURE_STORAGE_KEY, next);
  }

  #remember(key: string, value: string): void {
    try {
      this.#storage?.setItem(key, value);
    } catch {
      // The choice holds for this visit.
    }
  }
}

/** One store per page. Every `formatDate` and `formatUnit` reads this one. */
export const formatting = new FormattingStore();
