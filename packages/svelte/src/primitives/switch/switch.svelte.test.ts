import { describe, expect, it, vi } from 'vitest';
import { page, userEvent } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import { expectNoViolations } from '../../test/axe';
import Switch from './switch.svelte';

describe('Switch', () => {
  it('toggles on Space and reports the new state', async () => {
    const onCheckedChange = vi.fn();
    await render(Switch, {
      props: { onCheckedChange, 'aria-label': 'Auto-connect' },
    });

    const control = page.getByRole('switch', { name: 'Auto-connect' });
    await expect.element(control).toHaveAttribute('aria-checked', 'false');

    await userEvent.tab();
    await expect.element(control).toHaveFocus();

    await userEvent.keyboard(' ');
    await expect.element(control).toHaveAttribute('aria-checked', 'true');
    await expect.element(control).toHaveAttribute('data-state', 'checked');
    expect(onCheckedChange).toHaveBeenLastCalledWith(true);
  });

  it('toggles on Enter as well', async () => {
    await render(Switch, { props: { 'aria-label': 'Auto-connect' } });

    const control = page.getByRole('switch');
    await userEvent.tab();
    await userEvent.keyboard('{Enter}');
    await expect.element(control).toHaveAttribute('aria-checked', 'true');

    await userEvent.keyboard('{Enter}');
    await expect.element(control).toHaveAttribute('aria-checked', 'false');
    await expect.element(control).toHaveAttribute('data-state', 'unchecked');
  });

  it('blocks the callback and the keyboard while disabled', async () => {
    const onCheckedChange = vi.fn();
    await render(Switch, {
      props: { onCheckedChange, disabled: true, 'aria-label': 'Auto-connect' },
    });

    const control = page.getByRole('switch');
    await userEvent.tab();
    await expect.element(control).not.toHaveFocus();

    await control.click({ force: true });
    expect(onCheckedChange).not.toHaveBeenCalled();
    await expect.element(control).toHaveAttribute('aria-checked', 'false');
  });

  // The thumb takes its size from the root through the `group/switch` name, so
  // the one attribute has to reach both.
  it('renders each size with its own track and thumb', async () => {
    const sizes = ['sm', 'md'] as const;
    const boxes = new Map<string, number>();

    for (const size of sizes) {
      const { container, unmount } = await render(Switch, {
        props: { size, 'aria-label': 'Auto-connect' },
      });
      const track = page.getByRole('switch').element();
      const thumb = container.querySelector('[data-slot="switch-thumb"]');

      expect(track.getAttribute('data-size')).toBe(size);
      boxes.set(size, thumb?.getBoundingClientRect().width ?? 0);
      await unmount();
    }

    expect(boxes.get('sm')).toBeGreaterThan(0);
    expect(boxes.get('md')).toBeGreaterThan(boxes.get('sm') ?? 0);
  });

  // A disabled switch that keeps the live colour reads as a control somebody
  // can still press.
  it('paints a disabled switch away from the live colour', async () => {
    const live = await render(Switch, {
      props: { checked: true, 'aria-label': 'Auto-connect' },
    });
    const painted = getComputedStyle(
      page.getByRole('switch').element(),
    ).backgroundColor;
    await live.unmount();

    await render(Switch, {
      props: { checked: true, disabled: true, 'aria-label': 'Auto-connect' },
    });
    expect(
      getComputedStyle(page.getByRole('switch').element()).backgroundColor,
    ).not.toBe(painted);
  });

  it('has nothing axe objects to', async () => {
    const { container } = await render(Switch, {
      props: { checked: true, 'aria-label': 'Auto-connect' },
    });

    await expectNoViolations(container);
  });
});
