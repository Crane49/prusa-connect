import { createRawSnippet } from 'svelte';
import { afterEach, describe, expect, it } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import { expectNoViolations } from '../../test/axe';
import Label from './label.svelte';

const snippet = (html: string) =>
  createRawSnippet(() => ({ render: () => html }));

const hosts: HTMLElement[] = [];

/**
 * A control and the label that names it, in the order the `peer-disabled:`
 * classes need: the control first, the label after it.
 */
const field = async (id: string, disabled = false): Promise<HTMLElement> => {
  const host = document.createElement('div');
  host.innerHTML = `<input id="${id}" class="peer" ${disabled ? 'disabled' : ''} />`;
  document.body.append(host);
  hosts.push(host);

  await render(Label, {
    target: host,
    props: { for: id, children: snippet('Printer name') },
  });
  return host;
};

const labelOf = (host: HTMLElement): HTMLLabelElement => {
  const label = host.querySelector('label');
  if (label === null) throw new Error('no label rendered');
  return label;
};

afterEach(() => {
  for (const host of hosts.splice(0)) host.remove();
});

describe('Label', () => {
  it('moves focus to the control it names', async () => {
    const host = await field('printer-name');

    await page.elementLocator(labelOf(host)).click();

    expect(document.activeElement?.id).toBe('printer-name');
  });

  it('greys its words when the control before it is disabled', async () => {
    const live = await field('live-name');
    const dead = await field('dead-name', true);

    const colour = (host: HTMLElement) => getComputedStyle(labelOf(host)).color;

    expect(colour(dead)).not.toBe(colour(live));
  });

  it('refuses the pointer on a disabled control, so the click lands nowhere', async () => {
    const dead = await field('blocked-name', true);

    const style = getComputedStyle(labelOf(dead));
    expect(style.cursor).toBe('not-allowed');
  });

  it('names its control for a screen reader', async () => {
    const host = await field('named');

    await expect
      .element(page.getByRole('textbox', { name: 'Printer name' }))
      .toBeInTheDocument();
    await expectNoViolations(host);
  });
});

describe('Label variants', () => {
  const styleOf = async (variant: 'option' | 'hint') => {
    await render(Label, {
      props: { variant, children: snippet('Keep the file') },
    });
    return getComputedStyle(
      document.querySelector<HTMLElement>('[data-variant="' + variant + '"]')!,
    );
  };

  it('sets an option label in normal weight, start-aligned', async () => {
    const style = await styleOf('option');
    expect(style.fontWeight).toBe('400');
    expect(style.alignItems).toBe('flex-start');
  });

  it('sets a hint label small', async () => {
    expect(parseFloat((await styleOf('hint')).fontSize)).toBeCloseTo(12, 0);
  });
});
