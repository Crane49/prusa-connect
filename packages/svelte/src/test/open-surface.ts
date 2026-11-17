import { expect, vi } from 'vitest';
import { type Locator, page } from 'vitest/browser';

/**
 * The popover that is open now, as a locator to look inside. A popover that
 * is closing stays in the page while it fades, and one left by an earlier
 * test may still be there, so a search over the whole page can find two.
 */
export const openSurface = async (): Promise<Locator> => {
  const open = await vi.waitFor(() => {
    const found = document.querySelectorAll<HTMLElement>(
      '[data-slot="popover-content"][data-state="open"]',
    );
    expect(found).toHaveLength(1);

    return found[0];
  });
  if (open === undefined) throw new Error('no popover is open');

  return page.elementLocator(open);
};
