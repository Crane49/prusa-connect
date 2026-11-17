import { createRawSnippet } from 'svelte';
import { describe, expect, it } from 'vitest';
import { page, userEvent } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import Chip from './chip.svelte';

const children = createRawSnippet(() => ({ render: () => '<span>PLA</span>' }));

describe('Chip', () => {
  it('toggles aria-pressed from selected', async () => {
    const { rerender } = await render(Chip, { props: { children } });
    const chip = page.getByRole('button', { name: 'PLA' });
    await expect.element(chip).toHaveAttribute('aria-pressed', 'false');

    await rerender({ children, selected: true });
    await expect.element(chip).toHaveAttribute('aria-pressed', 'true');
  });

  it('calls onclick on Enter', async () => {
    let calls = 0;
    await render(Chip, { props: { children, onclick: () => (calls += 1) } });
    page.getByRole('button', { name: 'PLA' }).element().focus();
    await userEvent.keyboard('{Enter}');
    expect(calls).toBe(1);
  });

  it('marks the selected link as the current page', async () => {
    await render(Chip, { props: { children, href: '#pla', selected: true } });
    await expect
      .element(page.getByRole('link', { name: 'PLA' }))
      .toHaveAttribute('aria-current', 'page');
  });
});
