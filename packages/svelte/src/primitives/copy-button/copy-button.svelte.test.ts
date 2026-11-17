import { afterEach, describe, expect, it, vi } from 'vitest';
import { page, userEvent } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import { expectNoViolations } from '../../test/axe';
import CopyButton from './copy-button.svelte';
import type { CopyStatus } from './types';

const STATUS_RESET_MS = 3000;

/** The clipboard the component writes to. The tests run in a secure context. */
const clipboard = () => vi.spyOn(navigator.clipboard, 'writeText');

const announced = (): string =>
  (document.querySelector('[role="status"]')?.textContent ?? '').trim();

const element = (): HTMLButtonElement => {
  const found = document.querySelector('button');
  if (found === null) throw new Error('no copy button rendered');
  return found;
};

afterEach(() => {
  vi.restoreAllMocks();
});

describe('CopyButton', () => {
  it('puts the value on the clipboard and says so without renaming itself', async () => {
    const writeText = clipboard().mockResolvedValue(undefined);
    await render(CopyButton, {
      props: { value: 'SN-MK4S-0007-3312', label: 'Copy serial number' },
    });

    const button = page.getByRole('button', { name: 'Copy serial number' });
    await button.click();

    expect(writeText).toHaveBeenCalledWith('SN-MK4S-0007-3312');
    await expect.poll(announced).toBe('Copied to clipboard');
    // The name never changes: some assistive technology does not re-announce
    // a rename on an element that already has focus.
    await expect.element(button).toHaveAccessibleName('Copy serial number');
  });

  it('says the copy failed rather than looking as if it worked', async () => {
    clipboard().mockRejectedValue(
      new DOMException('Write permission denied.', 'NotAllowedError'),
    );
    await render(CopyButton, { props: { value: 'SN-MK4S-0007-3312' } });

    await page.getByRole('button', { name: 'Copy' }).click();

    await expect
      .poll(announced)
      .toBe('Copy failed, select the value and copy it manually.');
  });

  it('falls back to a selection copy where the page has no clipboard API', async () => {
    // Plain http is not a secure context, so `navigator.clipboard` is undefined.
    vi.spyOn(navigator, 'clipboard', 'get').mockReturnValue(
      undefined as unknown as Clipboard,
    );
    let selected = '';
    const execCommand = vi
      .spyOn(document, 'execCommand')
      .mockImplementation(() => {
        selected = document.querySelector('textarea')?.value ?? '';
        return true;
      });
    await render(CopyButton, { props: { value: 'SN-MK4S-0007-3312' } });

    await page.getByRole('button', { name: 'Copy' }).click();

    expect(execCommand).toHaveBeenCalledWith('copy');
    expect(selected).toBe('SN-MK4S-0007-3312');
    expect(document.querySelector('textarea')).toBeNull();
    await expect.poll(announced).toBe('Copied to clipboard');
  });

  it('says the copy failed when the fallback is refused too', async () => {
    vi.spyOn(navigator, 'clipboard', 'get').mockReturnValue(
      undefined as unknown as Clipboard,
    );
    vi.spyOn(document, 'execCommand').mockReturnValue(false);
    await render(CopyButton, { props: { value: 'SN-MK4S-0007-3312' } });

    await page.getByRole('button', { name: 'Copy' }).click();

    await expect
      .poll(announced)
      .toBe('Copy failed, select the value and copy it manually.');
  });

  it('copies from the keyboard', async () => {
    const writeText = clipboard().mockResolvedValue(undefined);
    await render(CopyButton, { props: { value: 'SN-MK4S-0007-3312' } });

    await userEvent.tab();
    await expect
      .element(page.getByRole('button', { name: 'Copy' }))
      .toHaveFocus();
    await userEvent.keyboard('{Enter}');

    expect(writeText).toHaveBeenCalledWith('SN-MK4S-0007-3312');
  });

  it('takes no second press while the write is in flight', async () => {
    let settle: (() => void) | undefined;
    const writeText = clipboard().mockImplementation(
      () =>
        new Promise<void>((resolve) => {
          settle = resolve;
        }),
    );
    await render(CopyButton, { props: { value: 'SN-MK4S-0007-3312' } });

    await page.getByRole('button', { name: 'Copy' }).click();
    await expect.poll(() => element().disabled).toBe(true);
    await expect.poll(announced).toBe('Copying…');

    // A disabled button fires no click, so the raced second write the
    // disabled state guards against never starts.
    element().click();
    expect(writeText).toHaveBeenCalledTimes(1);

    settle?.();
    await expect.poll(() => element().disabled).toBe(false);
    await expect.poll(announced).toBe('Copied to clipboard');
  });

  it('goes quiet again once the moment has passed', async () => {
    clipboard().mockResolvedValue(undefined);
    await render(CopyButton, { props: { value: 'SN-MK4S-0007-3312' } });

    await page.getByRole('button', { name: 'Copy' }).click();
    await expect.poll(announced).toBe('Copied to clipboard');

    await expect.poll(announced, { timeout: STATUS_RESET_MS + 1000 }).toBe('');
  });

  it('leaves no timer behind when it is taken off the page', async () => {
    clipboard().mockResolvedValue(undefined);
    const timers = new Set<number>();
    const realSetTimeout = window.setTimeout.bind(window);
    const realClearTimeout = window.clearTimeout.bind(window);

    // Only the component's own timer is counted. The test runner sets its own,
    // and a total over every timer on the page would measure the runner.
    window.setTimeout = ((
      handler: TimerHandler,
      delay?: number,
      ...args: unknown[]
    ) => {
      const id = realSetTimeout(handler, delay, ...args);
      if (delay === STATUS_RESET_MS) timers.add(id);
      return id;
    }) as typeof window.setTimeout;
    window.clearTimeout = ((id?: number) => {
      if (id !== undefined) timers.delete(id);
      realClearTimeout(id);
    }) as typeof window.clearTimeout;

    try {
      const rendered = await render(CopyButton, {
        props: { value: 'SN-MK4S-0007-3312' },
      });
      await page.getByRole('button', { name: 'Copy' }).click();
      await expect.poll(() => timers.size).toBeGreaterThan(0);

      await rendered.unmount();
      expect(timers.size).toBe(0);
    } finally {
      window.setTimeout = realSetTimeout;
      window.clearTimeout = realClearTimeout;
    }
  });

  it('hands the outcome to a caller that draws it instead', async () => {
    clipboard().mockResolvedValue(undefined);
    const seen: CopyStatus[] = [];
    const host = document.createElement('div');
    host.innerHTML = '<p id="outcome">Read-only</p>';
    document.body.append(host);

    await render(CopyButton, {
      target: host,
      props: {
        value: 'SN-MK4S-0007-3312',
        announce: false,
        describedBy: 'outcome',
        onStatusChange: (status: CopyStatus) => {
          seen.push(status);
        },
      },
    });

    // The caller's own region is the only one, so one copy is announced once.
    expect(host.querySelectorAll('[role="status"]')).toHaveLength(0);
    await expect
      .element(page.getByRole('button', { name: 'Copy' }))
      .toHaveAccessibleDescription('Read-only');

    await page.getByRole('button', { name: 'Copy' }).click();
    await expect.poll(() => seen).toEqual(['pending', 'copied']);

    host.remove();
  });

  it('has nothing axe objects to', async () => {
    const { container } = await render(CopyButton, {
      props: { value: 'SN-MK4S-0007-3312', label: 'Copy serial number' },
    });

    await expectNoViolations(container);
  });
});
