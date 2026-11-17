import { describe, expect, it } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import { expectNoViolations } from '../../test/axe';
import SettingsScreen from './settings-screen.svelte';

describe('SettingsScreen', () => {
  it('names the page once and draws no strip of panes', async () => {
    const { container } = await render(SettingsScreen);

    await expect
      .element(page.getByRole('heading', { level: 1 }))
      .toHaveTextContent('Settings');
    expect(container.querySelector('[role="tablist"]')).toBeNull();
  });

  it('has no accessibility violations', async () => {
    const { container } = await render(SettingsScreen);

    await expectNoViolations(container);
  });
});
