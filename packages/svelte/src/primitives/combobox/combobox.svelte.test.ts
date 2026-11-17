import { describe, expect, it } from 'vitest';
import { page, userEvent } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import ComboboxDialogProbe from '../../test/combobox-dialog-probe.svelte';
import Field from '../field/field.svelte';
import Combobox from './combobox.svelte';
import type { ComboboxProps } from './types';

const ITEMS = [
  { value: '1', label: 'Shelf A' },
  { value: '2', label: 'Shelf B' },
  { value: '3', label: 'Drybox' },
];

const draw = async (props: Partial<ComboboxProps> = {}) => {
  const picked: (string | undefined)[] = [];
  const created: string[] = [];
  await render(Combobox, {
    props: {
      items: ITEMS,
      value: undefined,
      onPick: (next: string | undefined) => picked.push(next),
      onCreate: (text: string) => {
        created.push(text);
      },
      createLabel: (text: string) => `Create “${text}”`,
      label: 'Location',
      ...props,
    },
  });
  return { picked, created };
};

const field = () => page.getByRole('combobox', { name: 'Location' });
const options = () =>
  [...document.querySelectorAll('[role="option"]')].map((o) =>
    o.textContent.trim(),
  );

describe('Combobox', () => {
  it('narrows the list to what is typed, whatever its case', async () => {
    await draw();

    await userEvent.type(field(), 'sHeLf');

    await expect
      .poll(options)
      .toStrictEqual(['Shelf A', 'Shelf B', 'Create “sHeLf”']);
  });

  it('picks the item named exactly on Enter', async () => {
    const { picked, created } = await draw();

    await userEvent.type(field(), 'drybox{Enter}');

    expect(picked).toStrictEqual(['3']);
    expect(created).toStrictEqual([]);
  });

  it('creates from typed text no item has on Enter', async () => {
    const { picked, created } = await draw();

    await userEvent.type(field(), 'Attic {Enter}');

    expect(created).toStrictEqual(['Attic']);
    expect(picked).toStrictEqual([]);
  });

  it('picks the row walked to by the arrows', async () => {
    const { picked } = await draw();

    await userEvent.type(field(), 'shelf');
    await expect.poll(options).toHaveLength(3);
    await userEvent.keyboard('{ArrowDown}{Enter}');

    expect(picked).toStrictEqual(['2']);
  });

  it('clears the pick from the none row', async () => {
    const { picked } = await draw({ value: '1', noneLabel: 'No location' });

    await field().click();
    await page.getByRole('option', { name: 'No location' }).click();

    expect(picked).toStrictEqual([undefined]);
  });

  it('gives the picked label back when the field loses the focus', async () => {
    await draw({ value: '1' });

    await userEvent.type(field(), 'xyz');
    await expect.element(field()).toHaveValue('Shelf Axyz');
    field().element().blur();

    await expect.element(field()).toHaveValue('Shelf A');
    await expect.poll(options).toStrictEqual([]);
  });

  it('shuts only the list on Escape inside a dialog', async () => {
    await render(ComboboxDialogProbe, { props: { title: 'Edit spool' } });

    await field().click();
    await expect.poll(options).toStrictEqual(['Shelf A']);
    await userEvent.keyboard('{Escape}');

    await expect.poll(options).toStrictEqual([]);
    await expect
      .element(page.getByRole('dialog', { name: 'Edit spool' }))
      .toBeVisible();
  });

  it('takes its id and description from a Field', async () => {
    await render(Field, {
      props: {
        label: 'Location',
        description: 'Where it is stored.',
        control: Combobox,
        controlProps: {
          items: ITEMS,
          value: undefined,
          onPick: () => undefined,
          label: 'Location',
        },
      },
    });

    const input = page.getByLabelText('Location');
    await expect.element(input).toHaveAttribute('data-slot', 'combobox-input');
    await expect
      .element(input)
      .toHaveAccessibleDescription('Where it is stored.');
  });
});
