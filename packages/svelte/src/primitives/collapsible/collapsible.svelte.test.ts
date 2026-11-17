import { describe, expect, it } from 'vitest';
import { page, userEvent } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import { expectNoViolations } from '../../test/axe';
import CollapsibleProbe from '../../test/collapsible-probe.svelte';

const label = 'Advanced settings';
const body = 'Configure bed mesh calibration.';

const section = (disabled = false) =>
  render(CollapsibleProbe, { props: { label, body, disabled } });

const content = (): HTMLElement | null =>
  document.querySelector<HTMLElement>('[data-slot="collapsible-content"]');

describe('Collapsible', () => {
  it('opens and shuts on Enter, and aria-expanded follows', async () => {
    await section();

    const trigger = page.getByRole('button', { name: label });
    await expect.element(trigger).toHaveAttribute('aria-expanded', 'false');

    trigger.element().focus();
    await userEvent.keyboard('{Enter}');
    await expect.element(trigger).toHaveAttribute('aria-expanded', 'true');
    await expect.element(page.getByText(body)).toBeVisible();

    await userEvent.keyboard('{Enter}');
    await expect.element(trigger).toHaveAttribute('aria-expanded', 'false');
    await expect.poll(() => content()?.hasAttribute('hidden')).toBe(true);
  });

  // The shut section is still in the document. bits-ui sets `hidden` on it, so
  // it takes no focus, a screen reader skips it and nothing of it is on screen.
  it('shows nothing of the content while it is shut', async () => {
    await section();

    expect(content()?.hasAttribute('hidden')).toBe(true);
    await expect.element(page.getByText(body)).not.toBeVisible();
  });

  it('refuses to open while the trigger is disabled', async () => {
    await section(true);

    const trigger = page.getByRole('button', { name: label });
    trigger.element().focus();
    await userEvent.keyboard('{Enter}');

    await expect.element(trigger).toHaveAttribute('aria-expanded', 'false');
    expect(content()?.hasAttribute('hidden')).toBe(true);
  });

  // A disabled row that keeps the live colour reads as a row somebody can
  // still press.
  it('paints a disabled trigger away from the live colour', async () => {
    const live = await section();
    const painted = getComputedStyle(
      page.getByRole('button', { name: label }).element(),
    ).color;
    await live.unmount();

    await section(true);
    expect(
      getComputedStyle(page.getByRole('button', { name: label }).element())
        .color,
    ).not.toBe(painted);
  });

  it('has nothing axe objects to', async () => {
    const { container } = await section();

    await page.getByRole('button', { name: label }).click();
    await expectNoViolations(container);
  });
});
