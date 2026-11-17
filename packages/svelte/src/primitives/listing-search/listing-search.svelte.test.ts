import { describe, expect, it } from 'vitest';
import { page, userEvent } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import ListingSearch from './listing-search.svelte';
import type { ListingSearchProps } from './types';

const draw = async (props: Partial<ListingSearchProps> = {}) => {
  const reported: string[] = [];
  await render(ListingSearch, {
    props: {
      value: '',
      onValue: (next: string) => reported.push(next),
      label: 'Search printers',
      placeholder: 'Search printers',
      ...props,
    },
  });
  return reported;
};

const box = () => page.getByRole('searchbox', { name: 'Search printers' });
const clearButton = () => page.getByRole('button', { name: 'Clear search' });

describe('ListingSearch', () => {
  it('draws a clear button only while the box holds text', async () => {
    await draw();
    await expect.element(clearButton()).not.toBeInTheDocument();
  });

  it('draws the clear button for a box with text', async () => {
    await draw({ value: 'prusa' });
    await expect.element(clearButton()).toBeVisible();
  });

  it('empties the box and keeps focus there when the clear button is pressed', async () => {
    const reported = await draw({ value: 'prusa' });

    await clearButton().click();

    expect(reported).toStrictEqual(['']);
    expect(document.activeElement).toBe(box().element());
  });

  it('reports each key with commit input', async () => {
    const reported = await draw();

    await userEvent.type(box(), 'ab');

    expect(reported).toStrictEqual(['a', 'ab']);
  });

  it('reports only the finished text with commit change', async () => {
    const reported = await draw({ commit: 'change' });

    await box().fill('abc');
    expect(reported).toStrictEqual([]);

    await userEvent.keyboard('{Enter}');
    expect(reported).toStrictEqual(['abc']);
  });

  it('reports an empty box when the clear button is pressed with commit change', async () => {
    const reported = await draw({ commit: 'change', value: 'abc' });

    await clearButton().click();

    expect(reported).toStrictEqual(['']);
  });

  it('empties text a change commit has not reported when Escape is pressed', async () => {
    const reported = await draw({ commit: 'change' });

    await box().fill('abc');
    await userEvent.keyboard('{Escape}');

    expect(box().element()).toHaveProperty('value', '');
    expect(reported).toStrictEqual(['']);
  });

  it('draws the clear button for text a change commit has not reported', async () => {
    const reported = await draw({ commit: 'change' });

    await userEvent.type(box(), 'abc');

    await expect.element(clearButton()).toBeVisible();
    expect(reported).toStrictEqual([]);
  });

  // A caller that reads the server on every report would read it for nothing.
  it('reports nothing when Escape is pressed in an empty box', async () => {
    const reported = await draw({ commit: 'change' });

    await box().click();
    await userEvent.keyboard('{Escape}');

    expect(reported).toStrictEqual([]);
  });
});
