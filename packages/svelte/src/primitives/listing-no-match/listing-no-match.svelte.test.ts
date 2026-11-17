import { describe, expect, it, vi } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import ListingNoMatch from './listing-no-match.svelte';

const base = { heading: 'No printers match', headingLevel: 'h2' as const };

describe('ListingNoMatch', () => {
  it('offers the way back to every row', async () => {
    const onClear = vi.fn();
    await render(ListingNoMatch, { props: { ...base, onClear } });

    const button = page.getByRole('button', {
      name: 'Clear search and filters',
    });
    await expect.element(button).toBeVisible();
    await button.click();
    expect(onClear).toHaveBeenCalledOnce();
  });

  it('takes the narrower label of a listing that only searches', async () => {
    await render(ListingNoMatch, {
      props: { ...base, onClear: () => undefined, clearLabel: 'Clear search' },
    });

    await expect
      .element(page.getByRole('button', { name: 'Clear search', exact: true }))
      .toBeVisible();
    await expect
      .element(page.getByRole('button', { name: 'Clear search and filters' }))
      .not.toBeInTheDocument();
  });
});
