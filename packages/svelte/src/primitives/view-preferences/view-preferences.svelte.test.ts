import { describe, expect, it } from 'vitest';

import {
  DEFAULT_VIEWS_STORAGE_KEY,
  ROWS_STORAGE_KEY,
  ViewPreferences,
  type ViewPreferenceStorage,
} from './view-preferences.svelte';

/** A store a test reads back, standing in for localStorage. */
const fakeStorage = (
  seed: Record<string, string> = {},
): ViewPreferenceStorage & { held: Map<string, string> } => {
  const held = new Map(Object.entries(seed));

  return {
    held,
    getItem: (key) => held.get(key) ?? null,
    setItem: (key, value) => {
      held.set(key, value);
    },
  };
};

describe('ViewPreferences', () => {
  it('opens on the default page size', () => {
    const preferences = new ViewPreferences({ storage: fakeStorage() });

    expect(preferences.rowsPerPage).toBe(25);
  });

  it('reads back what it wrote', () => {
    const storage = fakeStorage();
    const preferences = new ViewPreferences({ storage });

    preferences.rowsPerPage = 100;

    expect(storage.held.get(ROWS_STORAGE_KEY)).toBe('100');

    const reopened = new ViewPreferences({ storage });

    expect(reopened.rowsPerPage).toBe(100);
  });

  it('ignores a stored size no pager offers', () => {
    const preferences = new ViewPreferences({
      storage: fakeStorage({ [ROWS_STORAGE_KEY]: '37' }),
    });

    expect(preferences.rowsPerPage).toBe(25);
  });

  it('remembers nothing with no storage, and still answers', () => {
    const preferences = new ViewPreferences({ storage: null });

    preferences.rowsPerPage = 50;

    expect(preferences.rowsPerPage).toBe(50);
  });
});

describe('ViewPreferences default views', () => {
  it('opens a listing in the shape its field names', () => {
    const preferences = new ViewPreferences({ storage: fakeStorage() });

    expect(preferences.defaultView('printers')).toBe('cards');
    expect(preferences.defaultView('jobs')).toBe('table');
  });

  it('says nothing about a listing no field names', () => {
    const preferences = new ViewPreferences({ storage: fakeStorage() });

    expect(preferences.defaultView('printer-files')).toBeUndefined();
  });

  it('reads back what it wrote', () => {
    const storage = fakeStorage();
    const preferences = new ViewPreferences({ storage });

    preferences.setDefaultView('printers', 'table');

    expect(new ViewPreferences({ storage }).defaultView('printers')).toBe(
      'table',
    );
  });

  it('refuses a shape the listing does not offer', () => {
    const preferences = new ViewPreferences({ storage: fakeStorage() });

    preferences.setDefaultView('jobs', 'cards');

    expect(preferences.defaultView('jobs')).toBe('table');
  });

  it('ignores a held value the listing could not draw', () => {
    const preferences = new ViewPreferences({
      storage: fakeStorage({
        [DEFAULT_VIEWS_STORAGE_KEY]: JSON.stringify({
          printers: 'rows',
          jobs: 'rows',
        }),
      }),
    });

    expect(preferences.defaultView('printers')).toBe('cards');
    expect(preferences.defaultView('jobs')).toBe('rows');
  });

  it('survives a storage key holding something other than JSON', () => {
    const preferences = new ViewPreferences({
      storage: fakeStorage({ [DEFAULT_VIEWS_STORAGE_KEY]: 'not json' }),
    });

    expect(preferences.defaultView('printers')).toBe('cards');
  });
});
