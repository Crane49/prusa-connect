import { flushSync } from 'svelte';
import { describe, expect, it } from 'vitest';

import {
  THEME_STORAGE_KEY,
  type ThemeMedia,
  type ThemePreference,
  ThemeStore,
  type ThemeStorage,
} from './theme.svelte';

const fakeStorage = (seed?: string) => {
  const map = new Map<string, string>();
  if (seed !== undefined) map.set(THEME_STORAGE_KEY, seed);
  const storage: ThemeStorage = {
    getItem: (key) => map.get(key) ?? null,
    setItem: (key, value) => {
      map.set(key, value);
    },
  };
  return { map, storage };
};

const fakeMedia = (dark: boolean) => {
  let matches = dark;
  const listeners: ((event: { matches: boolean }) => void)[] = [];
  const media: ThemeMedia = {
    get matches() {
      return matches;
    },
    addEventListener: (_type, listener) => {
      listeners.push(listener);
    },
  };
  return {
    media,
    flip: (next: boolean) => {
      matches = next;
      for (const listener of listeners) listener({ matches: next });
    },
  };
};

describe('ThemeStore', () => {
  it('starts on system with nothing stored, and reads the machine', () => {
    const { storage } = fakeStorage();
    const dark = new ThemeStore({ storage, media: fakeMedia(true).media });
    const light = new ThemeStore({ storage, media: fakeMedia(false).media });

    expect(dark.preference).toBe('system');
    expect(dark.dark).toBe(true);
    expect(light.dark).toBe(false);
  });

  it('reads a stored preference back', () => {
    const { storage } = fakeStorage('dark');
    const store = new ThemeStore({ storage, media: fakeMedia(false).media });

    expect(store.preference).toBe('dark');
    expect(store.dark).toBe(true);
  });

  it('ignores a stored value it does not know', () => {
    const { storage } = fakeStorage('sepia');
    const store = new ThemeStore({ storage, media: null });

    expect(store.preference).toBe('system');
  });

  it('persists a preference under the one key', () => {
    const { map, storage } = fakeStorage();
    const store = new ThemeStore({ storage, media: null });

    store.preference = 'light';

    expect(map.get(THEME_STORAGE_KEY)).toBe('light');
    expect([...map.keys()]).toEqual([THEME_STORAGE_KEY]);
  });

  it('refuses a value outside light, dark and system, and persists nothing', () => {
    const { map, storage } = fakeStorage();
    const store = new ThemeStore({ storage, media: null });

    // A JavaScript consumer can assign anything; the cast is what lets the
    // test send it.
    store.preference = 'sepia' as ThemePreference;

    expect(store.preference).toBe('system');
    expect(map.size).toBe(0);
  });

  it('follows the machine under system and stops once a side is chosen', () => {
    const { media, flip } = fakeMedia(false);
    const store = new ThemeStore({ storage: null, media });

    flip(true);
    expect(store.dark).toBe(true);

    store.preference = 'light';
    expect(store.dark).toBe(false);
    flip(false);
    flip(true);
    expect(store.dark).toBe(false);

    store.preference = 'system';
    expect(store.dark).toBe(true);
  });

  // `core.frontend.theme` is the opening state of a browser that has never
  // been told otherwise, and never an overrule of somebody's own choice.
  describe('the theme a deployment configured', () => {
    it('opens a browser that remembers nothing', () => {
      const { map, storage } = fakeStorage();
      const store = new ThemeStore({ storage, media: fakeMedia(false).media });

      store.useDefault('dark');

      expect(store.preference).toBe('dark');
      expect(store.dark).toBe(true);
      // It is the opening state and not a choice, so nothing is written.
      expect(map.size).toBe(0);
      expect(store.chosen).toBe(false);
    });

    it('gives way to what this browser already remembers', () => {
      const { storage } = fakeStorage('light');
      const store = new ThemeStore({ storage, media: fakeMedia(true).media });

      store.useDefault('dark');

      expect(store.preference).toBe('light');
    });

    it('gives way to a choice made on this page', () => {
      const { storage } = fakeStorage();
      const store = new ThemeStore({ storage, media: null });

      store.preference = 'light';
      store.useDefault('dark');

      expect(store.preference).toBe('light');
      expect(store.chosen).toBe(true);
    });

    it('refuses a word outside light, dark and system', () => {
      const store = new ThemeStore({ storage: null, media: null });

      store.useDefault('sepia' as ThemePreference);

      expect(store.preference).toBe('system');
    });
  });

  describe('the theme of the program that shows the page', () => {
    it('wins over a remembered choice and writes nothing', () => {
      const { map, storage } = fakeStorage('light');
      const store = new ThemeStore({ storage, media: null });

      store.follow('dark');

      expect(store.preference).toBe('dark');
      expect(store.dark).toBe(true);
      expect(map.get(THEME_STORAGE_KEY)).toBe('light');
    });

    it('is not undone by the deployment default that arrives later', () => {
      const store = new ThemeStore({ storage: null, media: null });

      store.follow('dark');
      store.useDefault('light');

      expect(store.preference).toBe('dark');
    });

    it('refuses a word outside light, dark and system', () => {
      const store = new ThemeStore({ storage: null, media: null });

      store.follow('sepia' as ThemePreference);

      expect(store.preference).toBe('system');
    });
  });

  it('themes without storage, and forgets', () => {
    const store = new ThemeStore({ storage: null, media: null });

    store.preference = 'dark';

    expect(store.dark).toBe(true);
  });

  // The write-count test svelte-testing.md asks for. A no-op write must not
  // touch the graph, and one write is one recomputation.
  it('recomputes dark once for a write and not at all for a no-op', () => {
    const { storage } = fakeStorage();
    const store = new ThemeStore({ storage, media: fakeMedia(false).media });
    const seen: boolean[] = [];

    const dispose = $effect.root(() => {
      $effect(() => {
        seen.push(store.dark);
      });
    });

    try {
      flushSync();
      expect(seen).toEqual([false]);

      store.preference = 'system';
      flushSync();
      expect(seen).toEqual([false]);

      store.preference = 'dark';
      flushSync();
      expect(seen).toEqual([false, true]);

      // Light and system both come to `false` on a light machine, so the
      // derived value does not change and nothing downstream runs.
      store.preference = 'light';
      flushSync();
      store.preference = 'system';
      flushSync();
      expect(seen).toEqual([false, true, false]);
    } finally {
      dispose();
    }
  });
});
