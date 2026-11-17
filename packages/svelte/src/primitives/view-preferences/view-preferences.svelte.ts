// What a listing draws before this reader has touched it, on this browser.
import {
  browserStorage,
  readRemembered,
  type RememberedStorage,
  writeRemembered,
} from '../../remembered';
import { DEFAULT_PAGE_SIZE, PAGE_SIZES, type PageSize } from '../pager/sizes';
import type { ViewModeName } from '../view-mode/types';

/** The `localStorage` keys these preferences live under. */
export const ROWS_STORAGE_KEY = 'preferences.rowsPerPage';
export const DEFAULT_VIEWS_STORAGE_KEY = 'preferences.defaultViews';

export interface DefaultViewField {
  /** A listing's `viewSetting`, which is its `id` unless it names another. */
  id: string;
  labelKey: string;
  /** The shapes this listing offers, as its own toolbar offers them. */
  modes: readonly ViewModeName[];
  /** What it opens in until a reader says otherwise. */
  initial: ViewModeName;
}

/**
 * Every listing whose opening shape is a reader's to choose.
 *
 * `storage` and a printer's own file browser share the `files` setting: both
 * are the same listing of the same things, and a reader deciding how files
 * open decides it once.
 *
 * `modes` is what each screen's toolbar offers and nothing more — a
 * preference for a shape the screen cannot draw is a preference it could
 * never honour.
 */
export const DEFAULT_VIEW_FIELDS: readonly DefaultViewField[] = [
  {
    id: 'printers',
    labelKey: 'settingsAppearance.defaultViews.printers',
    modes: ['table', 'cards', 'frames'],
    initial: 'cards',
  },
  {
    id: 'jobs',
    labelKey: 'settingsAppearance.defaultViews.jobs',
    modes: ['table', 'rows'],
    initial: 'table',
  },
  {
    id: 'files',
    labelKey: 'settingsAppearance.defaultViews.files',
    modes: ['table', 'cards'],
    initial: 'table',
  },
  {
    id: 'tags',
    labelKey: 'settingsAppearance.defaultViews.tags',
    modes: ['table', 'cards'],
    initial: 'table',
  },
  {
    id: 'settings-credentials',
    labelKey: 'settingsAppearance.defaultViews.tokens',
    modes: ['table', 'rows'],
    initial: 'table',
  },
  {
    id: 'settings-people',
    labelKey: 'settingsAppearance.defaultViews.people',
    modes: ['table', 'rows'],
    initial: 'table',
  },
];

const FIELD_BY_ID = new Map(
  DEFAULT_VIEW_FIELDS.map((field) => [field.id, field]),
);

/** Reads back only what a listing could act on: a known field, a shape it offers. */
const readDefaultViews = (raw: string | null): Record<string, string> => {
  if (raw === null) return {};

  try {
    const parsed: unknown = JSON.parse(raw);
    if (typeof parsed !== 'object' || parsed === null) return {};

    return Object.fromEntries(
      Object.entries(parsed as Record<string, unknown>).filter(
        ([id, view]) =>
          FIELD_BY_ID.get(id)?.modes.some((mode) => mode === view) === true,
      ),
    ) as Record<string, string>;
  } catch {
    return {};
  }
};

export type ViewPreferenceStorage = RememberedStorage;

export interface ViewPreferencesOptions {
  storage?: ViewPreferenceStorage | null;
}

const isPageSize = (value: number): value is PageSize =>
  PAGE_SIZES.some((size) => size === value);

/** A shared store, so a class (svelte-5.md). */
export class ViewPreferences {
  #rowsPerPage = $state<PageSize>(DEFAULT_PAGE_SIZE);
  #defaultViews = $state.raw<Record<string, string>>({});
  readonly #storage: ViewPreferenceStorage | null;

  constructor({ storage = browserStorage() }: ViewPreferencesOptions = {}) {
    this.#storage = storage;

    this.#defaultViews = readDefaultViews(
      readRemembered(storage, DEFAULT_VIEWS_STORAGE_KEY),
    );

    // `Number(null)` and `Number('')` are both 0.
    const rows = Number(readRemembered(storage, ROWS_STORAGE_KEY));
    if (isPageSize(rows)) this.#rowsPerPage = rows;
  }

  /** How many rows a listing opens with. */
  get rowsPerPage(): PageSize {
    return this.#rowsPerPage;
  }

  /** Rejects a size no pager offers: the state stays, nothing is stored. */
  set rowsPerPage(next: PageSize) {
    if (!isPageSize(next)) return;
    this.#rowsPerPage = next;
    this.#remember(ROWS_STORAGE_KEY, String(next));
  }

  /**
   * What a listing opens in. The field's own `initial` where this reader has
   * not said, and undefined for a listing no field names — that one keeps
   * whatever its screen asked for.
   */
  defaultView(setting: string): string | undefined {
    return this.#defaultViews[setting] ?? FIELD_BY_ID.get(setting)?.initial;
  }

  /** Rejects a shape the listing does not offer: nothing changes, nothing is stored. */
  setDefaultView(setting: string, next: string): void {
    if (
      FIELD_BY_ID.get(setting)?.modes.some((mode) => mode === next) !== true
    ) {
      return;
    }

    this.#defaultViews = { ...this.#defaultViews, [setting]: next };
    this.#remember(
      DEFAULT_VIEWS_STORAGE_KEY,
      JSON.stringify(this.#defaultViews),
    );
  }

  #remember(key: string, value: string): void {
    writeRemembered(this.#storage, key, value);
  }
}

/** One store per page. Every listing reads this one. */
export const viewPreferences = new ViewPreferences();
