// It runs in a browser rather than under Node because the store paints as.
import { flushSync } from 'svelte';
import { afterEach, describe, expect, it } from 'vitest';

import { DEFAULT_SKIN, type Skin } from './skin';
import {
  SKIN_STORAGE_KEY,
  type SkinStorage,
  SkinStore,
} from './skin-store.svelte';

const fakeStorage = (seed?: string) => {
  const map = new Map<string, string>();
  if (seed !== undefined) map.set(SKIN_STORAGE_KEY, seed);
  const storage: SkinStorage = {
    getItem: (key) => map.get(key) ?? null,
    setItem: (key, value) => {
      map.set(key, value);
    },
  };
  return { map, storage };
};

const painted = (): string | undefined => document.documentElement.dataset.skin;

afterEach(() => {
  delete document.documentElement.dataset.skin;
});

describe('SkinStore', () => {
  it('starts on the default with nothing stored, and paints nothing', () => {
    const { storage } = fakeStorage();
    const store = new SkinStore({ storage });

    expect(store.preference).toBe(DEFAULT_SKIN);
    expect(store.chosen).toBe(false);
    // The page keeps what it was served with until something says otherwise.
    expect(painted()).toBeUndefined();
  });

  it('reads a stored preference back and paints it', () => {
    const { storage } = fakeStorage('slicer');
    const store = new SkinStore({ storage });

    expect(store.preference).toBe('slicer');
    expect(store.chosen).toBe(true);
    expect(painted()).toBe('slicer');
  });

  it('ignores a stored value it does not know', () => {
    const { storage } = fakeStorage('candidate-chartreuse');
    const store = new SkinStore({ storage });

    expect(store.preference).toBe(DEFAULT_SKIN);
    expect(store.chosen).toBe(false);
  });

  it('persists a preference under the one key and paints it', () => {
    const { map, storage } = fakeStorage();
    const store = new SkinStore({ storage });

    store.preference = 'prusapro';

    expect(map.get(SKIN_STORAGE_KEY)).toBe('prusapro');
    expect([...map.keys()]).toEqual([SKIN_STORAGE_KEY]);
    expect(painted()).toBe('prusapro');
  });

  it('refuses a name outside the three, and persists nothing', () => {
    const { map, storage } = fakeStorage();
    const store = new SkinStore({ storage });

    // A JavaScript consumer can assign anything.
    store.preference = 'candidate-chartreuse' as Skin;

    expect(store.preference).toBe(DEFAULT_SKIN);
    expect(map.size).toBe(0);
    expect(painted()).toBeUndefined();
  });

  it('repaints without storage, and forgets', () => {
    const store = new SkinStore({ storage: null });

    store.preference = 'slicer';

    expect(store.preference).toBe('slicer');
    expect(painted()).toBe('slicer');
  });

  // `core.frontend.skin` is the opening state of a browser that has never.
  describe('the skin a deployment configured', () => {
    it('opens a browser that remembers nothing', () => {
      const { map, storage } = fakeStorage();
      const store = new SkinStore({ storage });

      store.useDefault('slicer');

      expect(store.preference).toBe('slicer');
      expect(painted()).toBe('slicer');
      // It is the opening state and not a choice, so nothing is written.
      expect(map.size).toBe(0);
      expect(store.chosen).toBe(false);
    });

    it('gives way to what this browser already remembers', () => {
      const { storage } = fakeStorage('prusapro');
      const store = new SkinStore({ storage });

      store.useDefault('slicer');

      expect(store.preference).toBe('prusapro');
      expect(painted()).toBe('prusapro');
    });

    it('gives way to a choice made on this page', () => {
      const { storage } = fakeStorage();
      const store = new SkinStore({ storage });

      store.preference = 'slicer';
      store.useDefault('prusapro');

      expect(store.preference).toBe('slicer');
      expect(store.chosen).toBe(true);
    });

    it('refuses a name it has no block for', () => {
      const store = new SkinStore({ storage: null });

      store.useDefault('candidate-chartreuse');

      expect(store.preference).toBe(DEFAULT_SKIN);
      expect(painted()).toBeUndefined();
    });
  });

  // The write-count test svelte-testing.md asks for.
  it('reports the preference once for a write and not at all for a no-op', () => {
    const { storage } = fakeStorage();
    const store = new SkinStore({ storage });
    const seen: Skin[] = [];

    const dispose = $effect.root(() => {
      $effect(() => {
        seen.push(store.preference);
      });
    });

    try {
      flushSync();
      expect(seen).toEqual([DEFAULT_SKIN]);

      store.preference = 'slicer';
      flushSync();
      expect(seen).toEqual([DEFAULT_SKIN, 'slicer']);

      store.preference = 'slicer';
      flushSync();
      expect(seen).toEqual([DEFAULT_SKIN, 'slicer']);

      store.preference = 'candidate-chartreuse' as Skin;
      flushSync();
      expect(seen).toEqual([DEFAULT_SKIN, 'slicer']);
    } finally {
      dispose();
    }
  });
});
