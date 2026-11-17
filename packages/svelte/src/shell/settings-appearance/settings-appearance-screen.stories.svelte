<script module lang="ts">
  import { SvelteMap } from 'svelte/reactivity';
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import type { HomePreference } from '../../domain';
  import { HOME_STORAGE_KEY, Preferences } from '../../data/preferences.svelte';
  import SettingsAppearanceScreen from './settings-appearance-screen.svelte';

  const { Story } = defineMeta({
    title: 'Parts/Screen components/Settings Appearance',
    component: SettingsAppearanceScreen,
  });

  /** The real store over a map instead of `localStorage`. */
  const holding = (start: HomePreference): Preferences => {
    const map = new SvelteMap<string, string>([[HOME_STORAGE_KEY, start]]);

    return new Preferences({
      getItem: (key) => map.get(key) ?? null,
      setItem: (key, value) => {
        map.set(key, value);
      },
    });
  };
</script>

<!-- Every control but the home page is the library's own singleton. -->
<Story name="Default" args={{ home: holding('all-printers') }} />

<Story name="Opens On The Printer" args={{ home: holding('my-printer') }} />

<Story name="Opens By The Count" args={{ home: holding('auto') }} />
