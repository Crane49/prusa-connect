import { describe, expect, it, vi } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import ReplaceQuestion from './replace-question.svelte';

describe('ReplaceQuestion', () => {
  it('asks a copy only whether to replace the file', async () => {
    const onReplace = vi.fn();
    await render(ReplaceQuestion, {
      props: { name: 'a.bgcode', onReplace, onKeep: vi.fn() },
    });

    await expect
      .element(page.getByRole('button', { name: /Print the printer.s file/u }))
      .not.toBeInTheDocument();
    await page.getByRole('button', { name: 'Replace' }).click();

    expect(onReplace).toHaveBeenCalledOnce();
  });

  it('offers printing the file on the printer, replacing it, or neither', async () => {
    const onReplace = vi.fn();
    const onPrintHeld = vi.fn();
    const onKeep = vi.fn();
    await render(ReplaceQuestion, {
      props: { name: 'a.bgcode', print: true, onReplace, onPrintHeld, onKeep },
    });

    await page
      .getByRole('button', { name: /Print the printer.s file/u })
      .click();
    await page.getByRole('button', { name: 'Replace and print' }).click();
    await page.getByRole('button', { name: 'Cancel print' }).click();

    expect(onPrintHeld).toHaveBeenCalledOnce();
    expect(onReplace).toHaveBeenCalledOnce();
    expect(onKeep).toHaveBeenCalledOnce();
  });
});
