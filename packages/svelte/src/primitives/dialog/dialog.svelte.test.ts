import { describe, expect, it, onTestFinished } from 'vitest';
import { page, userEvent } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import { expectNoViolations } from '../../test/axe';
import DialogProbe from '../../test/dialog-probe.svelte';

const trigger = () => page.getByRole('button', { name: 'Rename printer' });

const panel = (): HTMLElement | null =>
  document.querySelector<HTMLElement>('[data-slot="dialog-content"]');

describe('Dialog', () => {
  it('opens on Enter, moves focus inside and returns it on Escape', async () => {
    await render(DialogProbe);

    trigger().element().focus();
    await userEvent.keyboard('{Enter}');

    // bits-ui traps focus in the panel and lands on the first tabbable
    // descendant.
    await expect
      .element(page.getByRole('button', { name: 'Cancel' }))
      .toHaveFocus();

    await userEvent.keyboard('{Escape}');

    await expect.poll(panel).toBe(null);
    await expect.element(trigger()).toHaveFocus();
  });

  // bits-ui writes the role, `aria-modal` and the pair of references. The
  // title and the description are what name and describe the panel.
  it('is a modal dialog named by its title', async () => {
    await render(DialogProbe, { props: { open: true } });

    const dialog = page.getByRole('dialog', { name: 'Rename printer' });
    await expect.element(dialog).toBeVisible();
    await expect.element(dialog).toHaveAttribute('aria-modal', 'true');

    const described = panel()?.getAttribute('aria-describedby');
    expect(described).toBeTruthy();
    expect(document.getElementById(described!)?.textContent).toContain(
      'Choose a name',
    );
  });

  it('shuts the panel from the control in the corner', async () => {
    await render(DialogProbe, { props: { open: true } });

    await page.getByRole('button', { name: 'Close' }).click();

    await expect.poll(panel).toBe(null);
  });

  it('draws no control in the corner when the caller asks for none', async () => {
    await render(DialogProbe, {
      props: { open: true, showCloseButton: false },
    });

    await expect.element(page.getByRole('dialog')).toBeVisible();
    expect(page.getByRole('button', { name: 'Close' }).query()).toBe(null);
  });

  // The exit animation is what keeps the closing panel on the screen long
  // enough to be seen going. A panel with no animation name vanishes in one
  // frame.
  it('grows wider for each size on a screen with the room', async () => {
    await page.viewport(1280, 800);
    onTestFinished(() => page.viewport(414, 896));
    const widths: number[] = [];
    for (const size of ['md', 'lg', 'xl'] as const) {
      const shown = await render(DialogProbe, { props: { open: true, size } });
      await expect.poll(panel).not.toBe(null);
      expect(panel()?.dataset.size).toBe(size);
      widths.push(panel()?.getBoundingClientRect().width ?? 0);
      await shown.unmount();
    }

    expect(widths[0]).toBeLessThan(widths[1] ?? 0);
    expect(widths[1]).toBeLessThan(widths[2] ?? 0);
  });

  it('scales the panel out while it closes', async () => {
    await render(DialogProbe, { props: { open: true } });

    const closing = panel();
    if (closing === null) throw new Error('no panel rendered');

    await userEvent.keyboard('{Escape}');

    expect(closing.getAttribute('data-state')).toBe('closed');
    expect(getComputedStyle(closing).animationName).toBe('scale-out');
  });

  it('has nothing axe objects to', async () => {
    const { container } = await render(DialogProbe, { props: { open: true } });

    await expectNoViolations(container);
    await expectNoViolations(panel()!);
  });
});
