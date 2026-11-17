import { afterEach, describe, expect, it, vi } from 'vitest';
import { page, userEvent } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import { expectNoViolations } from '../../test/axe';
import CodeBlock from './code-block.svelte';

const hosts: HTMLElement[] = [];

/** A host of its own per block, so two blocks on the page stay tellable apart. */
const inside = (): HTMLElement => {
  const host = document.createElement('div');
  document.body.append(host);
  hosts.push(host);
  return host;
};

const sentence = (host: HTMLElement): HTMLElement => {
  const found = host.querySelector<HTMLElement>('[role="status"]');
  if (found === null) throw new Error('no status rendered');
  return found;
};

const pressCopy = (host: HTMLElement): void => {
  const found = host.querySelector('button');
  if (found === null) throw new Error('no copy button rendered');
  found.click();
};

afterEach(() => {
  vi.restoreAllMocks();
  for (const host of hosts.splice(0)) host.remove();
});

describe('CodeBlock', () => {
  it('saves the text under the file name it was given', async () => {
    const value = '[service::connect]\nhostname = shop.example.net\n';

    await render(CodeBlock, {
      target: inside(),
      props: { value, download: 'prusa_printer_settings.ini' },
    });

    const link = page.getByRole('link', { name: 'Download' });
    await expect
      .element(link)
      .toHaveAttribute('download', 'prusa_printer_settings.ini');
    const href = link.element().getAttribute('href') ?? '';
    expect(await (await fetch(href)).text()).toBe(value);
  });

  it('offers no download without a file name', async () => {
    await render(CodeBlock, { target: inside(), props: { value: 'x' } });

    await expect
      .element(page.getByRole('link', { name: 'Download' }))
      .not.toBeInTheDocument();
  });

  it('draws the label, the text and the sentence that says it cannot be typed into', async () => {
    const host = inside();

    await render(CodeBlock, {
      target: host,
      props: { label: 'Printer object', value: '{"id":"p-01"}' },
    });

    await expect.element(page.getByText('Printer object')).toBeInTheDocument();
    await expect.element(page.getByText('{"id":"p-01"}')).toBeInTheDocument();
    expect(sentence(host).textContent.trim()).toBe('Read-only');
    await expectNoViolations(host);
  });

  it('says there is nothing to show rather than drawing an empty box', async () => {
    const host = inside();

    await render(CodeBlock, {
      target: host,
      props: { label: 'Printer object', value: '' },
    });

    await expect
      .element(page.getByText('Nothing to show.'))
      .toBeInTheDocument();
  });

  it('puts the copy button at the end of the row when there is no label', async () => {
    const withLabel = inside();
    const without = inside();

    await render(CodeBlock, {
      target: withLabel,
      props: { label: 'Printer object', value: 'x' },
    });
    await render(CodeBlock, { target: without, props: { value: 'x' } });

    const rowOf = (host: HTMLElement): string => {
      const row = host.querySelector('[data-slot="code-block"] > div');
      if (row === null) throw new Error('no row rendered');
      return getComputedStyle(row).justifyContent;
    };

    expect(rowOf(withLabel)).toBe('space-between');
    expect(rowOf(without)).toBe('flex-end');
  });

  it('puts the text on the clipboard and describes the button with the outcome', async () => {
    const host = inside();
    const writeText = vi
      .spyOn(navigator.clipboard, 'writeText')
      .mockResolvedValue(undefined);

    await render(CodeBlock, {
      target: host,
      props: { label: 'Printer object', value: '{"id":"p-01"}' },
    });

    await userEvent.click(page.getByRole('button', { name: 'Copy' }));

    expect(writeText).toHaveBeenCalledWith('{"id":"p-01"}');
    await expect
      .element(page.getByRole('button', { name: 'Copy' }))
      .toHaveAccessibleDescription('Copied to clipboard');
  });

  it('says so, in the destructive colour, when the clipboard refuses', async () => {
    const host = inside();
    vi.spyOn(navigator.clipboard, 'writeText').mockRejectedValue(
      new DOMException('Write permission denied.', 'NotAllowedError'),
    );

    await render(CodeBlock, {
      target: host,
      props: { label: 'Printer object', value: '{"id":"p-01"}' },
    });

    await userEvent.click(page.getByRole('button', { name: 'Copy' }));

    await expect
      .element(page.getByRole('button', { name: 'Copy' }))
      .toHaveAccessibleDescription(
        'Copy failed, select the text below and copy it manually.',
      );

    const reference = document.createElement('span');
    reference.className = 'text-destructive-text';
    host.append(reference);
    expect(getComputedStyle(sentence(host)).color).toBe(
      getComputedStyle(reference).color,
    );
  });

  it('takes no second press while the write is still in flight', async () => {
    const host = inside();
    let release = (): void => undefined;
    const inFlight = new Promise<void>((resolve) => {
      release = (): void => {
        resolve();
      };
    });
    const writeText = vi
      .spyOn(navigator.clipboard, 'writeText')
      .mockReturnValue(inFlight);

    await render(CodeBlock, {
      target: host,
      props: { label: 'Printer object', value: '{"id":"p-01"}' },
    });

    pressCopy(host);
    await expect
      .element(page.getByRole('button', { name: 'Copy' }))
      .toBeDisabled();
    expect(sentence(host).textContent.trim()).toBe('Copying…');

    pressCopy(host);
    expect(writeText).toHaveBeenCalledTimes(1);

    release();
    await expect
      .element(page.getByRole('button', { name: 'Copy' }))
      .toHaveAccessibleDescription('Copied to clipboard');
  });

  it('is reached and pressed by the keyboard alone', async () => {
    const host = inside();
    const writeText = vi
      .spyOn(navigator.clipboard, 'writeText')
      .mockResolvedValue(undefined);

    await render(CodeBlock, {
      target: host,
      props: { label: 'Printer object', value: 'ready' },
    });

    const button = host.querySelector('button');
    button?.focus();
    await userEvent.keyboard('{Enter}');

    expect(writeText).toHaveBeenCalledWith('ready');
  });

  it('goes back to saying it is read-only once the outcome has been read', async () => {
    const host = inside();
    vi.spyOn(navigator.clipboard, 'writeText').mockResolvedValue(undefined);

    await render(CodeBlock, {
      target: host,
      props: { label: 'Printer object', value: 'ready' },
    });

    pressCopy(host);
    await expect
      .poll(() => sentence(host).textContent.trim())
      .toBe('Copied to clipboard');

    // The outcome stands for three seconds and then the block says what it is
    // again, so a reader arriving later is not told about a copy they never
    // made.
    await expect
      .poll(() => sentence(host).textContent.trim(), { timeout: 5000 })
      .toBe('Read-only');
  }, 8000);
});
