import { describe, expect, it } from 'vitest';
import { page, userEvent } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import { expectNoViolations } from '../../test/axe';
import BreadcrumbProbe from '../../test/breadcrumb-probe.svelte';

const links = [
  { href: '/printers', label: 'Printers' },
  { href: '/printers/mk4s', label: 'MK4S' },
];
const current = 'USB files';

// The words of the trail come from the test and not from the catalogue, so an
// assertion below names what it renders.
const trail = () =>
  render(BreadcrumbProbe, {
    props: { label: 'Breadcrumb', more: 'More', links, current },
  });

const buttonTrail = () =>
  render(BreadcrumbProbe, {
    props: {
      label: 'Breadcrumb',
      more: 'More',
      links,
      current,
      asButton: true,
    },
  });

describe('Breadcrumb', () => {
  // The `child` snippet merges nothing. A caller that forgets to spread the
  // attributes loses the slot and the classes with no error.
  it('hands its element over to the child snippet', async () => {
    await buttonTrail();

    const link = document.querySelector('[data-slot="breadcrumb-link"]');
    expect(link?.tagName).toBe('BUTTON');
    expect(link?.className).toContain('hover:text-foreground');
    expect(
      document.querySelectorAll('a[data-slot="breadcrumb-link"]'),
    ).toHaveLength(0);
  });

  // A screen reader that reports the current page as a link offers a person a
  // press that goes nowhere.
  it('names the current page and does not link it', async () => {
    await trail();

    const marker = document.querySelector('[data-slot="breadcrumb-page"]');
    expect(marker?.tagName).toBe('SPAN');
    expect(marker?.getAttribute('aria-current')).toBe('page');
    expect(marker?.textContent).toBe(current);
    expect(page.getByRole('link', { name: current }).elements()).toHaveLength(
      0,
    );
  });

  it('gives the nav a name to tell it from the other landmarks', async () => {
    await trail();

    await expect
      .element(page.getByRole('navigation', { name: 'Breadcrumb' }))
      .toBeInTheDocument();
  });

  it('reaches every link by keyboard, in the order of the trail', async () => {
    await trail();

    await userEvent.tab();
    await expect
      .element(page.getByRole('link', { name: 'Printers' }))
      .toHaveFocus();

    await userEvent.tab();
    await expect
      .element(page.getByRole('link', { name: 'MK4S' }))
      .toHaveFocus();

    // Past the last link. The current page takes no focus of its own.
    await userEvent.tab();
    expect(document.activeElement?.getAttribute('data-slot')).not.toBe(
      'breadcrumb-page',
    );
  });

  it('has nothing axe objects to', async () => {
    const { container } = await trail();

    await expectNoViolations(container);
  });
});
