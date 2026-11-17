import { createRawSnippet } from 'svelte';
import { describe, expect, it } from 'vitest';
import { page, userEvent } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import { formatNumber } from '../../i18n';
import { expectNoViolations } from '../../test/axe';
import FacetRow from './facet-row.svelte';
import type { FacetRowProps } from './types';

const label = (text: string) =>
  createRawSnippet(() => ({ render: () => `<span>${text}</span>` }));

interface Drawn {
  container: HTMLElement;
  row: HTMLButtonElement;
  /** The box on the left, which follows the row's state. */
  box: HTMLElement;
  /** How many times the row has called back. */
  toggles: () => number;
}

const draw = async (props: Partial<FacetRowProps> = {}): Promise<Drawn> => {
  let toggles = 0;
  const { container } = await render(FacetRow, {
    props: {
      label: label('Ready'),
      checked: false,
      onToggle: () => {
        toggles += 1;
      },
      ...props,
    },
  });

  const row = container.querySelector<HTMLButtonElement>(
    '[data-slot="facet-row"]',
  );
  if (row === null) throw new Error('no facet row rendered');
  const box = row.firstElementChild;
  if (!(box instanceof HTMLElement)) throw new Error('no box rendered');

  return { container, row, box, toggles: () => toggles };
};

describe('FacetRow', () => {
  it('is a pressed button and not a checkbox', async () => {
    const off = await draw();

    expect(off.row.tagName).toBe('BUTTON');
    expect(off.row.type).toBe('button');
    expect(off.row.getAttribute('aria-pressed')).toBe('false');
    expect(off.row.hasAttribute('data-on')).toBe(false);
    expect(off.box.hasAttribute('data-on')).toBe(false);
    expect(off.box.getAttribute('aria-hidden')).toBe('true');
    const resting = getComputedStyle(off.box).backgroundColor;

    const on = await draw({ checked: true });

    expect(on.row.getAttribute('aria-pressed')).toBe('true');
    expect(on.row.hasAttribute('data-on')).toBe(true);
    expect(on.box.hasAttribute('data-on')).toBe(true);
    // `data-on:bg-selected` fills the box, and nothing else tells a person the
    // row is picked once the tick is in place.
    expect(getComputedStyle(on.box).backgroundColor).not.toBe(resting);
  });

  it('calls back on a click and on the keyboard', async () => {
    const { row, toggles } = await draw();

    await userEvent.tab();
    expect(document.activeElement).toBe(row);
    await userEvent.keyboard(' ');
    expect(toggles()).toBe(1);

    await userEvent.keyboard('{Enter}');
    expect(toggles()).toBe(2);

    await page.getByRole('button', { name: 'Ready' }).click();
    expect(toggles()).toBe(3);
  });

  it('refuses a row the caller disabled and says why', async () => {
    const { row, toggles } = await draw({
      count: 6,
      disabled: true,
      hint: 'read only',
    });

    expect(row.disabled).toBe(true);
    // The hint follows the label in the same line, parted by a hyphen.
    expect(row.textContent.replace(/\s+/g, ' ')).toContain('Ready - read only');

    row.click();
    expect(toggles()).toBe(0);

    await userEvent.tab();
    expect(document.activeElement).not.toBe(row);
  });

  it('refuses a facet that would return nothing', async () => {
    const { row, toggles } = await draw({ count: 0 });

    expect(row.disabled).toBe(true);
    row.click();
    expect(toggles()).toBe(0);
    // The row stays in the list, dimmed, so the choices do not reshuffle as a
    // person ticks them.
    expect(Number(getComputedStyle(row).opacity)).toBeLessThan(1);
  });

  it('keeps a picked facet clickable at a count of zero', async () => {
    const { row, toggles } = await draw({ count: 0, checked: true });

    // Disabling it would leave Clear all as the only way out of an empty
    // result.
    expect(row.disabled).toBe(false);
    row.click();
    expect(toggles()).toBe(1);
  });

  it('writes the count in the reader language, and none without one', async () => {
    const counted = await draw({ count: 1284 });
    const number = counted.row.lastElementChild;

    expect(number?.textContent.trim()).toBe(formatNumber(1284));
    expect(number?.textContent.trim()).not.toBe('1284');

    const plain = await draw();

    // Without a count the row is a plain choice, so the label is the last
    // thing in it.
    expect(plain.row.lastElementChild?.textContent).toContain('Ready');
  });

  it('has nothing axe objects to', async () => {
    const { container } = await draw({ count: 12, checked: true });

    await expectNoViolations(container);
  });
});
