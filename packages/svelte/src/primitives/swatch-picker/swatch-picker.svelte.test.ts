import { describe, expect, it } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import SwatchPicker from './swatch-picker.svelte';
import type { SwatchPickerProps } from './types';

const swatches = [
  { key: 'red', hex: '#FF0000', label: 'Red' },
  { key: 'blue', hex: '#0000FF', label: 'Blue' },
];

const draw = async (props: Partial<SwatchPickerProps> = {}) => {
  const picked: (string | undefined)[] = [];
  await render(SwatchPicker, {
    props: {
      swatches,
      value: 'red',
      label: 'Colour',
      onPick: (key: string | undefined) => picked.push(key),
      ...props,
    },
  });
  return picked;
};

describe('SwatchPicker', () => {
  it('marks the picked swatch pressed', async () => {
    await draw();
    await expect
      .element(page.getByRole('button', { name: 'Red' }))
      .toHaveAttribute('aria-pressed', 'true');
    await expect
      .element(page.getByRole('button', { name: 'Blue' }))
      .toHaveAttribute('aria-pressed', 'false');
  });

  it('reports undefined for the picked swatch when deselectable', async () => {
    const picked = await draw({ deselectable: true });
    await page.getByRole('button', { name: 'Red' }).click();
    expect(picked).toStrictEqual([undefined]);
  });

  it('reports the key of the picked swatch when not deselectable', async () => {
    const picked = await draw();
    await page.getByRole('button', { name: 'Red' }).click();
    await page.getByRole('button', { name: 'Blue' }).click();
    expect(picked).toStrictEqual(['red', 'blue']);
  });
});
