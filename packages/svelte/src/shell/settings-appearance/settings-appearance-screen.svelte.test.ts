// The home choice on the Appearance page, on a server with and without
// dashboards (ADR-20261008-sww).
import { describe, expect, it } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import { Preferences } from '../../data';
import SettingsAppearanceScreen from './settings-appearance-screen.svelte';

describe('the home choice', () => {
  it('offers the dashboard where the server keeps dashboards', async () => {
    await render(SettingsAppearanceScreen, {
      props: { home: new Preferences(null) },
    });

    await expect
      .element(page.getByRole('radio', { name: 'Dashboard' }))
      .toBeChecked();
  });

  it('leaves the dashboard out, and shows automatic, where the server has none', async () => {
    await render(SettingsAppearanceScreen, {
      props: { home: new Preferences(null), dashboards: false },
    });

    await expect
      .element(page.getByRole('radio', { name: 'Automatic' }))
      .toBeChecked();
    await expect
      .element(page.getByRole('radio', { name: 'Dashboard' }))
      .not.toBeInTheDocument();
  });
});
