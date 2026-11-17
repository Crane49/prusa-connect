import { createRawSnippet } from 'svelte';
import { describe, expect, it, vi } from 'vitest';
import { render } from 'vitest-browser-svelte';

import { expectNoViolations } from '../../test/axe';
import BulkBar from './bulk-bar.svelte';

const snippet = (html: string) =>
  createRawSnippet(() => ({ render: () => html }));

const props = {
  clearLabel: 'Clear selection',
  onClear: () => undefined,
  actions: snippet('<button type="button">Delete</button>'),
  idle: snippet('<button type="button">New folder</button>'),
};

describe('BulkBar', () => {
  // The two states are different jobs, so the row shows one or the other.
  it('swaps the controls of the listing for those of the selection', async () => {
    const { container, rerender } = await render(BulkBar, {
      props: { ...props, count: 0, label: '' },
    });

    expect(container.textContent).toContain('New folder');
    expect(container.textContent).not.toContain('Delete');

    await rerender({ ...props, count: 3, label: '3 selected' });

    expect(container.textContent).toContain('3 selected');
    expect(container.textContent).toContain('Delete');
    expect(container.textContent).not.toContain('New folder');
  });

  // A row that grows as the first box is ticked pushes the listing down under
  // the pointer that ticked it.
  it('keeps one height across the swap', async () => {
    const { container, rerender } = await render(BulkBar, {
      props: { ...props, count: 0, label: '' },
    });

    const row = container.firstElementChild;
    if (row === null) throw new Error('no row rendered');
    const idle = row.getBoundingClientRect().height;

    await rerender({ ...props, count: 3, label: '3 selected' });

    expect(row.getBoundingClientRect().height).toBeCloseTo(idle, 1);
  });

  it('clears the selection on request', async () => {
    const onClear = vi.fn();
    const { getByText } = await render(BulkBar, {
      props: { ...props, count: 3, label: '3 selected', onClear },
    });

    await getByText('Clear selection').click();
    expect(onClear).toHaveBeenCalledTimes(1);
  });

  // Rows on other pages cannot be ticked one by one, so the bar offers them.
  it('offers every row only when asked to', async () => {
    const onSelectAll = vi.fn();
    const { getByText, container, rerender } = await render(BulkBar, {
      props: { ...props, count: 3, label: '3 selected' },
    });
    expect(container.textContent).not.toContain('Select all 40');

    await rerender({
      ...props,
      count: 3,
      label: '3 selected',
      selectAllLabel: 'Select all 40',
      onSelectAll,
    });
    await getByText('Select all 40').click();
    expect(onSelectAll).toHaveBeenCalledTimes(1);
  });

  it('has nothing axe objects to', async () => {
    const { container } = await render(BulkBar, {
      props: { ...props, count: 3, label: '3 selected' },
    });

    await expectNoViolations(container);
  });
});
