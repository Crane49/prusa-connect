import { describe, expect, it } from 'vitest';
import { page, userEvent } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import { expectNoViolations } from '../../test/axe';
import PopoverProbe from '../../test/popover-probe.svelte';

const trigger = () => page.getByRole('button', { name: 'Printer options' });

const surface = (): HTMLElement | null =>
  document.querySelector<HTMLElement>('[data-slot="popover-content"]');

describe('Popover', () => {
  it('opens on Enter, moves focus inside and returns it on Escape', async () => {
    await render(PopoverProbe);

    await expect.element(trigger()).toHaveAttribute('aria-expanded', 'false');

    trigger().element().focus();
    await userEvent.keyboard('{Enter}');

    await expect.element(trigger()).toHaveAttribute('aria-expanded', 'true');
    // bits-ui traps focus in the surface and lands on the first tabbable
    // descendant.
    await expect
      .element(page.getByRole('button', { name: 'Open settings' }))
      .toHaveFocus();

    await userEvent.keyboard('{Escape}');

    await expect.poll(surface).toBe(null);
    await expect.element(trigger()).toHaveFocus();
  });

  // Radix gives the surface `role="dialog"`, and the React design system names
  // it with `aria-labelledby`. bits-ui puts no role on it, so a name there
  // would reach nobody. The trigger says what it opens instead.
  //
  // bits-ui also leaves the surface with no `id`, so the trigger's
  // `aria-controls` never resolves and the attribute stays off.
  it('says on the trigger what it opens, and puts no role on the surface', async () => {
    await render(PopoverProbe, { props: { open: true } });

    const open = trigger().element();
    expect(open.getAttribute('aria-haspopup')).toBe('dialog');
    expect(open.getAttribute('aria-expanded')).toBe('true');
    expect(surface()?.getAttribute('role')).toBe(null);
  });

  it('draws the surface on the popover colour', async () => {
    await render(PopoverProbe, { props: { open: true } });

    expect(surface()?.className).toContain('bg-popover');
    expect(getComputedStyle(surface()!).backgroundColor).not.toBe(
      'rgba(0, 0, 0, 0)',
    );
  });

  it('scales the surface from the corner the anchor put it on', async () => {
    await render(PopoverProbe, { props: { open: true } });

    // The React class reads a Radix custom property. bits-ui publishes the
    // same measurement under its own name, and the element aliases one to the
    // other, so the origin has to resolve to a real pair of lengths.
    await expect
      .poll(() => getComputedStyle(surface()!).transformOrigin)
      .toMatch(/^[\d.]+px [\d.]+px$/);
  });

  it('has nothing axe objects to', async () => {
    const { container } = await render(PopoverProbe, {
      props: { open: true },
    });

    await expectNoViolations(container);
    await expectNoViolations(surface()!);
  });
});
