// What this browser's reader chose, for the choices no server holds.
import type { HomePreference } from '../domain/home-shape';

import {
  browserStorage,
  readRemembered,
  type RememberedStorage,
  writeRemembered,
} from '../remembered';

export type PreferenceStore = RememberedStorage;

/** The `localStorage` keys these preferences live under. */
export const HOME_STORAGE_KEY = 'preferences.home';

/** Which printer `my-printer` means. */
export const PRINTER_STORAGE_KEY = 'preferences.printer';

/** The metrics a reader turned on for the history chart. */
export const CHART_METRICS_STORAGE_KEY = 'preferences.chartMetrics';

const HOME_PREFERENCES: readonly HomePreference[] = [
  'dashboard',
  'auto',
  'my-printer',
  'all-printers',
];

const isHome = (value: unknown): value is HomePreference =>
  typeof value === 'string' &&
  (HOME_PREFERENCES as readonly string[]).includes(value);

/** A shared store, so a class (svelte-5.md). */
export class Preferences {
  readonly #store: PreferenceStore | null;

  // The home page is a dashboard unless the reader chose a page of
  // printers (ADR-20261008-sww rule 13).
  #home = $state<HomePreference>('dashboard');

  #printer = $state<string | undefined>(undefined);

  #chartMetrics = $state.raw<readonly string[] | undefined>(undefined);

  constructor(store: PreferenceStore | null = browserStorage()) {
    this.#store = store;

    const held = readRemembered(store, HOME_STORAGE_KEY);
    if (isHome(held)) this.#home = held;

    this.#printer = readRemembered(store, PRINTER_STORAGE_KEY) ?? undefined;

    // A reader who turned every line off comes back to the usual readings,
    // not to an empty chart whose legend is all struck through.
    const metrics = readRemembered(store, CHART_METRICS_STORAGE_KEY);
    if (metrics !== null && metrics !== '') {
      this.#chartMetrics = metrics.split(',');
    }
  }

  /** How much room a small fleet gets. */
  get home(): HomePreference {
    return this.#home;
  }

  setHome(next: HomePreference): void {
    this.#home = next;
    this.#remember(HOME_STORAGE_KEY, next);
  }

  /** The printer this reader was last on. */
  get printer(): string | undefined {
    return this.#printer;
  }

  setPrinter(id: string): void {
    this.#printer = id;
    this.#remember(PRINTER_STORAGE_KEY, id);
  }

  /** The metrics the history chart draws, or undefined where none was chosen. */
  get chartMetrics(): readonly string[] | undefined {
    return this.#chartMetrics;
  }

  setChartMetrics(next: readonly string[]): void {
    this.#chartMetrics = [...next];
    this.#remember(CHART_METRICS_STORAGE_KEY, next.join(','));
  }

  #remember(key: string, value: string): void {
    writeRemembered(this.#store, key, value);
  }
}
