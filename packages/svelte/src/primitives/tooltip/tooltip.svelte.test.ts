import { describe, expect, it } from 'vitest';
import { page, userEvent } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import { expectNoViolations } from '../../test/axe';
import TooltipProbe from '../../test/tooltip-probe.svelte';

const trigger = () => page.getByRole('button', { name: 'Pause' });

const surface = (): HTMLElement | null =>
  document.querySelector<HTMLElement>('[data-slot="tooltip-content"]');

describe('Tooltip', () => {
  it('opens on focus and closes on Escape without moving focus', async () => {
    await render(TooltipProbe);

    await userEvent.tab();
    await expect.element(trigger()).toHaveFocus();
    await expect
      .poll(() => surface()?.textContent)
      .toContain('Pause the current job');

    await userEvent.keyboard('{Escape}');

    await expect.poll(surface).toBe(null);
    await expect.element(trigger()).toHaveFocus();
  });

  // bits-ui points the trigger's `aria-describedby` at whatever `id` the
  // surface element carries, and it writes no `id` of its own. The surface is
  // drawn by the component, which is what gives the reference something to
  // resolve to.
  //
  // bits-ui only names the trigger it saw the person reach, so the reference
  // appears after a real focus and not under a forced `open`.
  it('describes the trigger with the surface while it is open', async () => {
    await render(TooltipProbe);

    await userEvent.tab();
    await expect.poll(surface).not.toBe(null);

    const named = surface()!;
    expect(named.id).not.toBe('');
    expect(named.getAttribute('role')).toBe('tooltip');
    await expect
      .poll(() => trigger().element().getAttribute('aria-describedby'))
      .toBe(named.id);
    expect(document.getElementById(named.id)).toBe(named);
  });

  it('draws the text on the inverted colour', async () => {
    await render(TooltipProbe, { props: { open: true } });

    expect(surface()?.className).toContain('bg-foreground');
    expect(getComputedStyle(surface()!).color).toBe(
      getComputedStyle(document.body).backgroundColor,
    );
  });

  it('scales the surface from the corner the anchor put it on', async () => {
    await render(TooltipProbe, { props: { open: true } });

    // The React class reads a Radix custom property. bits-ui publishes the
    // same measurement under its own name, and the element aliases one to the
    // other, so the origin has to resolve to a real pair of lengths.
    await expect
      .poll(() => getComputedStyle(surface()!).transformOrigin)
      .toMatch(/^[\d.]+px [\d.]+px$/);
  });

  it('has nothing axe objects to', async () => {
    const { container } = await render(TooltipProbe, { props: { open: true } });

    await expectNoViolations(container);
    await expectNoViolations(surface()!);
  });
});
