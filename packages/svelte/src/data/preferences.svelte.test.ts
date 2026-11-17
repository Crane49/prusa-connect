import { describe, expect, it } from 'vitest';

import {
  HOME_STORAGE_KEY,
  Preferences,
  type PreferenceStore,
} from './preferences.svelte';

const fakeStorage = (seed?: string) => {
  const map = new Map<string, string>();
  if (seed !== undefined) map.set(HOME_STORAGE_KEY, seed);
  const storage: PreferenceStore = {
    getItem: (key) => map.get(key) ?? null,
    setItem: (key, value) => {
      map.set(key, value);
    },
  };
  return { map, storage };
};

const refusingStorage = (): PreferenceStore => ({
  getItem: () => {
    throw new Error('refused');
  },
  setItem: () => {
    throw new Error('refused');
  },
});

describe('Preferences', () => {
  it('starts on the dashboard, whatever the fleet size', () => {
    expect(new Preferences(fakeStorage().storage).home).toBe('dashboard');
  });

  it('remembers what the reader chose', () => {
    const { map, storage } = fakeStorage();
    const preferences = new Preferences(storage);

    preferences.setHome('auto');

    expect(preferences.home).toBe('auto');
    expect(map.get(HOME_STORAGE_KEY)).toBe('auto');
  });

  it('reads back what it wrote', () => {
    expect(new Preferences(fakeStorage('auto').storage).home).toBe('auto');
  });

  it('ignores a stored word it has no meaning for', () => {
    expect(new Preferences(fakeStorage('sideways').storage).home).toBe(
      'dashboard',
    );
  });

  it('remembers nothing when it is handed no storage', () => {
    const preferences = new Preferences(null);

    preferences.setHome('my-printer');

    expect(preferences.home).toBe('my-printer');
  });

  it('gives back the readings a reader turned on', () => {
    const { storage } = fakeStorage();
    const preferences = new Preferences(storage);

    expect(preferences.chartMetrics).toBeUndefined();

    preferences.setChartMetrics(['/tools/0/nozzle_temp', '/axis/z']);

    expect(new Preferences(storage).chartMetrics).toEqual([
      '/tools/0/nozzle_temp',
      '/axis/z',
    ]);
  });

  // An empty chart with every reading struck through reads as a broken tab.
  it('reads every line turned off as no choice on the next visit', () => {
    const { storage } = fakeStorage();
    const preferences = new Preferences(storage);

    preferences.setChartMetrics([]);

    expect(preferences.chartMetrics).toEqual([]);
    expect(new Preferences(storage).chartMetrics).toBeUndefined();
  });

  it('works when the browser refuses storage', () => {
    const preferences = new Preferences(refusingStorage());

    expect(preferences.home).toBe('dashboard');
    expect(() => {
      preferences.setHome('auto');
    }).not.toThrow();
    expect(preferences.home).toBe('auto');
  });
});
