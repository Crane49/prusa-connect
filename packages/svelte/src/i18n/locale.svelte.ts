// Which language the page is in, and every catalogue it can read it from.
//
// One store for the whole page. A component that calls `t()` in its markup
// reads `locale.current` through it, so setting the language redraws every
// screen and every module's page with no reload and no second request.
import { CORE_CATALOGUES } from './catalogues';
import type { Messages } from './messages';

/**
 * The languages this library ships a catalogue file for. English is the
 * source and the rest are community files, so a tag is on this list whether
 * or not anybody has translated it yet.
 */
export const LOCALES = ['en', 'cs', 'de', 'es', 'fr', 'it', 'pl'] as const;

export type LocaleTag = (typeof LOCALES)[number];

/** The language every message is written in, and the fallback for the rest. */
export const SOURCE_LOCALE: LocaleTag = 'en';

const KNOWN: ReadonlySet<string> = new Set(LOCALES);

const isLocale = (tag: string): tag is LocaleTag => KNOWN.has(tag);

/**
 * The tag a language name resolves to. A browser says `cs-CZ` and a person
 * may type `DE`, and both are the language before the region.
 */
export const localeOf = (tag: string): LocaleTag | undefined => {
  const base = tag.trim().toLowerCase().split('-')[0] ?? '';

  return isLocale(base) ? base : undefined;
};

/**
 * The first language on the list this library has a catalogue for. The list
 * is in the order the person prefers, which is what `navigator.languages`
 * holds, and English is the answer when none of them matches.
 */
export const pickLocale = (preferred: readonly string[]): LocaleTag => {
  for (const tag of preferred) {
    const found = localeOf(tag);
    if (found) return found;
  }

  return SOURCE_LOCALE;
};

/** Every catalogue this page holds: namespace, then language, then messages. */
type Held = Record<string, Partial<Record<LocaleTag, Messages>>>;

/** The core's own catalogue is the empty namespace. A module has its own. */
export const CORE_NAMESPACE = '';

class LocaleStore {
  #current = $state<LocaleTag>(SOURCE_LOCALE);
  #catalogues = $state<Held>({ [CORE_NAMESPACE]: CORE_CATALOGUES });
  // Not state: a key is recorded while a template or a $derived reads it,
  // and Svelte refuses a change to state there.
  #missing: string[] = [];

  /** The language on screen now. */
  get current(): LocaleTag {
    return this.#current;
  }

  /** Every language a person may choose, the source first. */
  get available(): readonly LocaleTag[] {
    return LOCALES;
  }

  /**
   * The keys asked for that no catalogue holds, in the order they were first
   * asked for. A test reads it, and so does a person building a screen: a key
   * in this list is drawn as itself and a translator will never see it.
   */
  get missing(): readonly string[] {
    return [...this.#missing];
  }

  /**
   * Sets the language. A tag this library has no catalogue for leaves the
   * language as it was, so a stored setting from a later release cannot blank
   * the page.
   */
  set(tag: string): LocaleTag {
    const found = localeOf(tag);
    if (found) this.#current = found;

    return this.#current;
  }

  /**
   * Adds one language of one namespace. The host calls it for every module it
   * loads (ADR-20260911-4p6 rule 18), and an app calls it for its own catalogue.
   * Calling it again for the same pair replaces that catalogue, which is what
   * a reload of the module listing needs.
   */
  register(namespace: string, language: string, messages: Messages): void {
    const tag = localeOf(language);
    if (!tag) return;

    const own = this.#catalogues[namespace] ?? {};
    this.#catalogues = {
      ...this.#catalogues,
      [namespace]: { ...own, [tag]: messages },
    };
  }

  /**
   * Adds lines to one language of one namespace, keeping the lines it holds.
   * A screen whose texts travel in its own chunk calls it when the chunk
   * loads, so a page that never draws the screen never carries them.
   */
  extend(namespace: string, language: string, messages: Messages): void {
    const tag = localeOf(language);
    if (!tag) return;

    const own = this.#catalogues[namespace] ?? {};
    this.#catalogues = {
      ...this.#catalogues,
      [namespace]: { ...own, [tag]: { ...own[tag], ...messages } },
    };
  }

  /** Drops every catalogue of one namespace, for a module the host forgot. */
  forget(namespace: string): void {
    if (namespace === CORE_NAMESPACE) return;

    const rest: Held = {};
    for (const [name, held] of Object.entries(this.#catalogues)) {
      if (name !== namespace) rest[name] = held;
    }

    this.#catalogues = rest;
  }

  /**
   * One message, or undefined when no catalogue holds the key. The order is
   * the module's own language, the module's English, then the core's two:
   * a module may name a key the core already names, and its own wins.
   */
  message(namespace: string, key: string): string | undefined {
    const own = this.#catalogues[namespace];
    const core = this.#catalogues[CORE_NAMESPACE];

    return (
      own?.[this.#current]?.[key] ??
      own?.[SOURCE_LOCALE]?.[key] ??
      core?.[this.#current]?.[key] ??
      core?.[SOURCE_LOCALE]?.[key]
    );
  }

  /** Records a key nothing answers. The list is bounded by the keys in use. */
  record(key: string): void {
    if (this.#missing.includes(key)) return;

    this.#missing.push(key);
  }
}

/** One store per page. Every `t()` on the page reads this one. */
export const locale = new LocaleStore();
