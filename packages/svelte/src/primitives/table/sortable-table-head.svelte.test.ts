import { describe, expect, it, vi } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import SortableTableHead from './sortable-table-head.svelte';

const draw = (direction: 'ascending' | 'descending' | undefined) => {
  const onSort = vi.fn();
  // A `th` outside a row is dropped by the HTML parser, so it mounts in one.
  const row = document.body
    .appendChild(document.createElement('table'))
    .appendChild(document.createElement('tr'));
  const screen = render(SortableTableHead, {
    props: { label: 'Name', direction, onSort },
    target: row,
  });

  return { onSort, screen };
};

describe('SortableTableHead', () => {
  it('says the column is not sorted', async () => {
    draw(undefined);

    await expect
      .element(page.getByRole('columnheader', { name: 'Name' }))
      .toHaveAttribute('aria-sort', 'none');
  });

  it('says which way the column is sorted', async () => {
    draw('ascending');

    await expect
      .element(page.getByRole('columnheader', { name: 'Name' }))
      .toHaveAttribute('aria-sort', 'ascending');
  });

  it('asks to sort once per press', async () => {
    const { onSort } = draw(undefined);

    await page.getByRole('button', { name: 'Name' }).click();

    expect(onSort).toHaveBeenCalledTimes(1);
  });
});
