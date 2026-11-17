import { describe, expect, it, vi } from 'vitest';
import { page, userEvent } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import { expectNoViolations } from '../../test/axe';
import AlertDialogProbe from '../../test/alert-dialog-probe.svelte';

const trigger = () => page.getByRole('button', { name: 'Remove printer' });

const panel = (): HTMLElement | null =>
  document.querySelector<HTMLElement>('[data-slot="alert-dialog-content"]');

const scrim = (): HTMLElement | null =>
  document.querySelector<HTMLElement>('[data-slot="alert-dialog-overlay"]');

describe('AlertDialog', () => {
  // Radix moves the focus to Cancel when the panel opens. bits-ui moves it to
  // the panel, which carries `tabindex="-1"` for that reason, and the first
  // Tab then reaches Cancel.
  it('opens on Enter, takes the focus and returns it on Escape', async () => {
    await render(AlertDialogProbe);

    await expect.element(trigger()).toHaveAttribute('aria-expanded', 'false');

    trigger().element().focus();
    await userEvent.keyboard('{Enter}');

    await expect.poll(panel).not.toBe(null);
    await expect.poll(() => document.activeElement).toBe(panel());

    await userEvent.keyboard('{Escape}');

    await expect.poll(panel).toBe(null);
    await expect.element(trigger()).toHaveFocus();
  });

  it('names the panel with its title and calls it an alert dialog', async () => {
    await render(AlertDialogProbe, { props: { open: true } });

    const open = panel();
    expect(open?.getAttribute('role')).toBe('alertdialog');
    expect(open?.getAttribute('aria-modal')).toBe('true');

    const named = document.getElementById(
      open?.getAttribute('aria-labelledby') ?? '',
    );
    expect(named?.textContent).toBe('Remove printer');
    expect(named?.dataset.slot).toBe('alert-dialog-title');

    const described = document.getElementById(
      open?.getAttribute('aria-describedby') ?? '',
    );
    expect(described?.dataset.slot).toBe('alert-dialog-description');
  });

  it('walks from the panel to Cancel and on to the action', async () => {
    await render(AlertDialogProbe, { props: { open: true } });

    await expect.poll(() => document.activeElement).toBe(panel());

    await userEvent.tab();
    await expect
      .element(page.getByRole('button', { name: 'Cancel' }))
      .toHaveFocus();

    await userEvent.tab();
    await expect
      .element(page.getByRole('button', { name: 'Remove', exact: true }))
      .toHaveFocus();
  });

  // A dialog shuts on a press outside it. An alert dialog is answered, so the
  // scrim is inert and only the two buttons and Escape reach the question.
  it('stays open when the scrim is pressed', async () => {
    await render(AlertDialogProbe, { props: { open: true } });

    // The panel sits over the middle of the scrim, so the press lands in a
    // corner the panel does not cover.
    await userEvent.click(scrim()!, { position: { x: 4, y: 4 } });

    expect(panel()).not.toBe(null);
  });

  it('shuts on Cancel', async () => {
    await render(AlertDialogProbe, { props: { open: true } });

    await page.getByRole('button', { name: 'Cancel' }).click();

    await expect.poll(panel).toBe(null);
  });

  // bits-ui puts no click handler on the action, so the caller's handler is
  // the only thing a press runs and the caller decides when the panel goes.
  it('runs the caller on the action and leaves the panel to it', async () => {
    const onremove = vi.fn();
    await render(AlertDialogProbe, { props: { open: true, onremove } });

    await page.getByRole('button', { name: 'Remove', exact: true }).click();

    expect(onremove).toHaveBeenCalledTimes(1);
  });

  it('holds the small panel to a narrower width than the default one', async () => {
    const wide = await render(AlertDialogProbe, { props: { open: true } });

    expect(panel()?.dataset.size).toBe('default');
    const widest = getComputedStyle(panel()!).maxWidth;
    await wide.unmount();

    await render(AlertDialogProbe, { props: { open: true, size: 'sm' } });

    expect(panel()?.dataset.size).toBe('sm');
    expect(getComputedStyle(panel()!).maxWidth).not.toBe(widest);
  });

  it('has nothing axe objects to', async () => {
    const { container } = await render(AlertDialogProbe, {
      props: { open: true },
    });

    await expectNoViolations(container);
    await expectNoViolations(panel()!);
  });
});
