// Which skin this browser is drawn in, remembered between visits.
import {
  browserStorage,
  readRemembered,
  type RememberedStorage,
  writeRemembered,
} from '../remembered';
import { applySkin, DEFAULT_SKIN, type Skin, SKIN_NAMES } from './skin';

/** The one `localStorage` key the preference lives under. */
export const SKIN_STORAGE_KEY = 'skin';

export type SkinStorage = RememberedStorage;

export interface SkinStoreOptions {
  storage?: SkinStorage | null;
}

const isSkin = (value: unknown): value is Skin =>
  typeof value === 'string' &&
  (SKIN_NAMES as readonly string[]).includes(value);

/** What this browser remembers, or `undefined` when it remembers nothing. */
const storedPreference = (storage: SkinStorage | null): Skin | undefined => {
  const stored = readRemembered(storage, SKIN_STORAGE_KEY);
  return isSkin(stored) ? stored : undefined;
};

/** A shared store, so a class (svelte-5.md). */
export class SkinStore {
  #preference = $state<Skin>(DEFAULT_SKIN);
  readonly #storage: SkinStorage | null;
  // Whether a person on this browser has chosen a palette.
  #chosen = $state(false);

  constructor({ storage = browserStorage() }: SkinStoreOptions = {}) {
    this.#storage = storage;
    const held = storedPreference(storage);
    this.#chosen = held !== undefined;

    // Only a remembered choice paints here.
    if (held !== undefined) {
      this.#preference = held;
      applySkin(held);
    }
  }

  get preference(): Skin {
    return this.#preference;
  }

  /** Rejects any name outside the three: the state stays, nothing is persisted. */
  set preference(next: Skin) {
    if (!isSkin(next)) return;
    // A write is a choice even when it names the palette the page is already.
    this.#chosen = true;
    this.#preference = next;
    applySkin(next);
    writeRemembered(this.#storage, SKIN_STORAGE_KEY, next);
  }

  /** True once somebody on this browser has picked a palette. Read-only by design. */
  get chosen(): boolean {
    return this.#chosen;
  }

  /** Takes the skin a deployment configured. */
  useDefault(next: string): void {
    if (this.#chosen || !isSkin(next)) return;
    this.#preference = next;
    applySkin(next);
  }
}

export const skin = new SkinStore();
