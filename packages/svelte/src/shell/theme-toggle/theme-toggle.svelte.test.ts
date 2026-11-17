// The control cycles light, dark, system and back, and each press writes the
// preference the store persists.
import { describe, expect, it } from 'vitest';
import { page, userEvent } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import { theme } from '../../styles/theme.svelte';
import ThemeToggle from './theme-toggle.svelte';

describe('the theme control', () => {
  it('cycles through the three preferences', async () => {
    theme.preference = 'light';
    await render(ThemeToggle);

    const control = page.getByRole('button', { name: /theme/i });

    await userEvent.click(control);
    expect(theme.preference).toBe('dark');

    await userEvent.click(control);
    expect(theme.preference).toBe('system');

    await userEvent.click(control);
    expect(theme.preference).toBe('light');
  });
});
