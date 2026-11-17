import { afterEach, describe, expect, it, vi } from 'vitest';
import { page, userEvent } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import { expectNoViolations } from '../../test/axe';
import CopyField from './copy-field.svelte';

const VALUE = 'a1b2c3d4e5f6';

const clipboard = () => vi.spyOn(navigator.clipboard, 'writeText');

const field = () =>
  render(CopyField, { props: { label: 'API token', value: VALUE } });

const status = (): HTMLElement => {
  const found = document.querySelector<HTMLElement>('[role="status"]');
  if (found === null) throw new Error('no status region rendered');
  return found;
};

const button = () => page.getByRole('button', { name: 'Copy' });

afterEach(() => {
  vi.restoreAllMocks();
});

describe('CopyField', () => {
  it('says the value cannot be typed into, and names it', async () => {
    await field();

    const input = page.getByRole('textbox', { name: 'API token' });
    await expect.element(input).toHaveValue(VALUE);
    expect(input.element().hasAttribute('readonly')).toBe(true);
    expect(status().textContent.trim()).toBe('Read-only');
  });

  // The way out when the clipboard refuses: one keystroke copies a value that
  // is already selected.
  it('selects the whole value when the input takes focus', async () => {
    await field();

    await userEvent.tab();

    const input = page.getByRole('textbox').element() as HTMLInputElement;
    expect(document.activeElement).toBe(input);
    expect(input.selectionStart).toBe(0);
    expect(input.selectionEnd).toBe(VALUE.length);
  });

  it('reaches the input and then the copy button by keyboard', async () => {
    await field();

    await userEvent.tab();
    await expect.element(page.getByRole('textbox')).toHaveFocus();
    await userEvent.tab();
    await expect.element(button()).toHaveFocus();
  });

  it('describes the button with the outcome of the copy', async () => {
    const writeText = clipboard().mockResolvedValue(undefined);
    await field();

    await button().click();

    expect(writeText).toHaveBeenCalledWith(VALUE);
    await expect
      .element(button())
      .toHaveAccessibleDescription('Copied to clipboard');
    // One region, so the copy is announced once.
    expect(document.querySelectorAll('[role="status"]')).toHaveLength(1);
  });

  it('draws a refused copy in the colour of a fault', async () => {
    clipboard().mockRejectedValue(
      new DOMException('Write permission denied.', 'NotAllowedError'),
    );
    const { container } = await field();

    const resting = getComputedStyle(status()).color;

    await button().click();
    await expect
      .element(button())
      .toHaveAccessibleDescription(
        'Copy failed, select the value and copy it manually.',
      );

    const reference = document.createElement('span');
    reference.className = 'text-destructive-text';
    container.append(reference);
    const faulted = getComputedStyle(status()).color;
    expect(faulted).toBe(getComputedStyle(reference).color);
    expect(faulted).not.toBe(resting);
    reference.remove();
  });

  it('takes no second press while the first write is in flight', async () => {
    let refuse: ((reason: unknown) => void) | undefined;
    const writeText = clipboard().mockImplementation(
      () =>
        new Promise<void>((_resolve, reject) => {
          refuse = reject;
        }),
    );
    await field();

    await button().click();
    const element = button().element() as HTMLButtonElement;
    await expect.poll(() => element.disabled).toBe(true);

    // A disabled button fires no click, so the raced second write never
    // starts and cannot resolve out of order over the first.
    element.click();
    expect(writeText).toHaveBeenCalledTimes(1);

    refuse?.(new DOMException('Write permission denied.', 'NotAllowedError'));
    await expect.poll(() => element.disabled).toBe(false);
    expect(writeText).toHaveBeenCalledTimes(1);
    await expect
      .element(button())
      .toHaveAccessibleDescription(
        'Copy failed, select the value and copy it manually.',
      );
  });

  it('has nothing axe objects to', async () => {
    const { container } = await field();

    await expectNoViolations(container);
  });
});
