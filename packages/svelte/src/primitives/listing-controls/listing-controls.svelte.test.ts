import { describe, expect, it } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import { expectNoViolations } from '../../test/axe';
import ListingControlsProbe from '../../test/listing-controls-probe.svelte';

/** The cluster itself. */
const cluster = (): HTMLElement => {
  const found = document.querySelector<HTMLElement>(
    '[data-slot="listing-controls"]',
  );
  if (found === null) throw new Error('no listing controls rendered');

  return found;
};

describe('ListingControls', () => {
  it('draws the picker before the view toggle, whatever the screen adds', async () => {
    await render(ListingControlsProbe, { props: { withChildren: true } });

    const order = [...cluster().children].map(
      (child) =>
        child.getAttribute('data-testid') ?? child.getAttribute('aria-label'),
    );

    // What the screen keeps beside the pair comes first, then the fields, then
    // how the list is drawn. One order, so a reader who has learnt one
    // listing's toolbar has learnt the next one's.
    expect(order).toEqual(['beside', 'Fields', 'Display mode']);
  });

  it('renders no field picker for a listing with nothing to put away', async () => {
    await render(ListingControlsProbe, { props: { withFields: false } });

    expect(page.getByRole('button', { name: 'Fields' }).query()).toBe(null);
    await expect
      .element(page.getByRole('group', { name: 'Display mode' }))
      .toBeInTheDocument();
  });

  it('moves the view the toggle reports', async () => {
    await render(ListingControlsProbe, {});

    await page.getByRole('radio', { name: 'Rows' }).click();

    await expect.element(page.getByTestId('drawn')).toHaveTextContent('rows');
  });

  it('gives the view group the name the screen chose', async () => {
    await render(ListingControlsProbe, {
      props: { label: 'Job listing view' },
    });

    await expect
      .element(page.getByRole('group', { name: 'Job listing view' }))
      .toBeInTheDocument();
  });

  it('has nothing axe refuses', async () => {
    const { container } = await render(ListingControlsProbe, {
      props: { withChildren: true },
    });

    await expectNoViolations(container);
  });
});
