import { describe, expect, it, vi } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import type { PrintTarget } from '../../print-picker';
import DropChoice from './drop-choice.svelte';
import type { DropChoiceProps } from './types';

const free: PrintTarget = {
  id: 'p1',
  model: 'MK4',
  name: 'MK4',
  ready: true,
  reasonKey: undefined,
  reasonStates: undefined,
  state: 'IDLE',
};

const props = (over: Partial<DropChoiceProps> = {}): DropChoiceProps => ({
  names: ['bracket.bgcode'],
  printer: 'MK4',
  printTarget: free,
  onConfirm: vi.fn(),
  onCancel: vi.fn(),
  ...over,
});

describe('DropChoice', () => {
  it('starts on Print now for one file on a printer that can start it', async () => {
    const onConfirm = vi.fn();
    await render(DropChoice, { props: props({ onConfirm }) });

    await expect.element(page.getByText('Send to MK4')).toBeVisible();
    await expect
      .element(page.getByRole('radio', { name: /Print now/u }))
      .toBeChecked();
    await page.getByRole('button', { name: 'Confirm' }).click();

    expect(onConfirm).toHaveBeenCalledExactlyOnceWith(true, undefined);
  });

  it('offers the action a module runs on a landed file as a choice', async () => {
    const onConfirm = vi.fn();
    const action = {
      namespace: 'simple-queue',
      id: 'queue',
      title: 'Add to queue',
    };
    await render(DropChoice, {
      props: props({ onConfirm, actions: [action] }),
    });

    await page.getByRole('radio', { name: /Add to queue/u }).click();
    await page.getByRole('button', { name: 'Confirm' }).click();

    expect(onConfirm).toHaveBeenCalledExactlyOnceWith(false, action);
  });

  it('copies only when the person picks it', async () => {
    const onConfirm = vi.fn();
    await render(DropChoice, { props: props({ onConfirm }) });

    await page.getByRole('radio', { name: /Only copy to printer/u }).click();
    await page.getByRole('button', { name: 'Confirm' }).click();

    expect(onConfirm).toHaveBeenCalledExactlyOnceWith(false, undefined);
  });

  it('does not copy a file picked to print when the printer stops being able to', async () => {
    const onConfirm = vi.fn();
    const screen = await render(DropChoice, {
      props: props({ onConfirm }),
    });

    await page.getByRole('radio', { name: /Print now/u }).click();
    await screen.rerender({ printTarget: { ...free, ready: false } });
    await expect
      .element(page.getByRole('button', { name: 'Confirm' }))
      .toBeDisabled();
    expect(onConfirm).not.toHaveBeenCalled();
  });

  it('only copies to a printer that is printing, and says why', async () => {
    const onConfirm = vi.fn();
    await render(DropChoice, {
      props: props({
        onConfirm,
        printTarget: {
          ...free,
          ready: false,
          reasonKey: 'printPicker.reason.state.PRINTING',
          state: 'PRINTING',
        },
      }),
    });

    await expect
      .element(page.getByRole('radio', { name: /Print now/u }))
      .toBeDisabled();
    await expect
      .element(page.getByRole('radio', { name: /Only copy to printer/u }))
      .toBeChecked();
    await page.getByRole('button', { name: 'Confirm' }).click();

    expect(onConfirm).toHaveBeenCalledExactlyOnceWith(false, undefined);
  });

  it('only copies several files, because a printer prints one at a time', async () => {
    await render(DropChoice, {
      props: props({ names: ['a.bgcode', 'b.bgcode'] }),
    });

    await expect
      .element(page.getByRole('radio', { name: /Print now/u }))
      .toBeDisabled();
    await expect
      .element(
        page.getByText(
          'A printer prints one file at a time, so several files are only copied.',
        ),
      )
      .toBeVisible();
  });

  it('sends nothing when it is cancelled', async () => {
    const onCancel = vi.fn();
    const onConfirm = vi.fn();
    await render(DropChoice, { props: props({ onCancel, onConfirm }) });

    await page.getByRole('button', { name: 'Cancel' }).click();

    expect(onCancel).toHaveBeenCalled();
    expect(onConfirm).not.toHaveBeenCalled();
  });
});
