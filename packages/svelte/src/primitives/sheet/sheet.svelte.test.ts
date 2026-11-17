import { describe, expect, it } from 'vitest';
import { page, userEvent } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import { expectNoViolations } from '../../test/axe';
import SheetProbe from '../../test/sheet-probe.svelte';
import type { SheetSide } from './types';

const trigger = () => page.getByRole('button', { name: 'Printer settings' });

const panel = (): HTMLElement | null =>
  document.querySelector<HTMLElement>('[data-slot="sheet-content"]');

const scrim = (): HTMLElement | null =>
  document.querySelector<HTMLElement>('[data-slot="sheet-overlay"]');

const cross = (): HTMLElement | null =>
  document.querySelector<HTMLElement>('[data-slot="sheet-content"] button');

describe('Sheet', () => {
  it('opens on Enter, moves focus inside and returns it on Escape', async () => {
    await render(SheetProbe);

    await expect.element(trigger()).toHaveAttribute('aria-expanded', 'false');

    trigger().element().focus();
    await userEvent.keyboard('{Enter}');

    await expect.element(trigger()).toHaveAttribute('aria-expanded', 'true');
    // bits-ui traps focus in the panel and lands on the first tabbable
    // descendant, which is the Cancel button in the footer.
    await expect
      .element(page.getByRole('button', { name: 'Cancel' }))
      .toHaveFocus();

    await userEvent.keyboard('{Escape}');

    await expect.poll(panel).toBe(null);
    await expect.element(trigger()).toHaveFocus();
  });

  // bits-ui reports the title's and the description's `id` to the content,
  // which is what its `aria-labelledby` and `aria-describedby` point at. So a
  // panel with a title is named without the caller doing anything.
  it('names the panel with its title and describes it with the sentence', async () => {
    await render(SheetProbe, { props: { open: true } });

    const open = panel();
    expect(open?.getAttribute('role')).toBe('dialog');
    expect(open?.getAttribute('aria-modal')).toBe('true');

    const named = document.getElementById(
      open?.getAttribute('aria-labelledby') ?? '',
    );
    expect(named?.textContent).toBe('Printer settings');
    expect(named?.dataset.slot).toBe('sheet-title');

    const described = document.getElementById(
      open?.getAttribute('aria-describedby') ?? '',
    );
    expect(described?.dataset.slot).toBe('sheet-description');
  });

  // The edge is the whole difference between a sheet and a dialog, and it is
  // four class strings in `variants.ts` with nothing to stop one being pasted
  // from its neighbour. Each side is checked against the edge it lands on.
  const sides: SheetSide[] = ['top', 'right', 'bottom', 'left'];

  for (const side of sides) {
    it(`anchors the panel to the ${side} edge and slides it in from there`, async () => {
      const drawn = await render(SheetProbe, { props: { open: true, side } });

      const open = panel();
      if (open === null) throw new Error('no panel rendered');
      expect(open.className).toContain(`animate-slide-in-from-${side}`);
      expect(getComputedStyle(open)[side]).toBe('0px');

      await drawn.unmount();
    });
  }

  // The exit animation is what keeps the leaving panel on the screen long
  // enough to be seen going. bits-ui waits for an animation and for nothing
  // else, so a panel with no animation name vanishes in one frame, and so
  // does the scrim behind it.
  it('slides the panel out and fades the scrim while it closes', async () => {
    await render(SheetProbe, { props: { open: true } });

    const leaving = panel();
    const behind = scrim();
    if (leaving === null || behind === null) {
      throw new Error('no panel rendered');
    }

    await userEvent.keyboard('{Escape}');

    expect(leaving.getAttribute('data-state')).toBe('closed');
    expect(getComputedStyle(leaving).animationName).toBe('slide-out-to-right');
    expect(behind.getAttribute('data-state')).toBe('closed');
    expect(getComputedStyle(behind).animationName).toBe('fade-out');
  });

  // The scrim is a sibling of the panel inside the portal, never an ancestor
  // of it. Nested, a press meant for the scrim would count as a press inside
  // the panel and the sheet would stay open.
  it('draws the scrim beside the panel and passes the caller classes to it', async () => {
    await render(SheetProbe, {
      props: { open: true, overlayClass: 'bottom-16' },
    });

    const behind = scrim();
    expect(behind?.className).toContain('bottom-16');
    expect(behind?.contains(panel())).toBe(false);
    expect(behind?.nextElementSibling).toBe(panel());
  });

  it('shuts on the cross in the corner, which carries the catalogue word', async () => {
    await render(SheetProbe, { props: { open: true } });

    const shut = page.getByRole('button', { name: 'Close' });
    await expect.element(shut).toBeVisible();
    expect(cross()?.dataset.slot).toBe('sheet-close');

    await shut.click();

    await expect.poll(panel).toBe(null);
  });

  it('draws no cross when the caller asks for none', async () => {
    await render(SheetProbe, {
      props: { open: true, showCloseButton: false },
    });

    expect(panel()).not.toBe(null);
    await expect
      .element(page.getByRole('button', { name: 'Close' }))
      .not.toBeInTheDocument();
  });

  it('has nothing axe objects to', async () => {
    const { container } = await render(SheetProbe, { props: { open: true } });

    await expectNoViolations(container);
    await expectNoViolations(panel()!);
  });
});
