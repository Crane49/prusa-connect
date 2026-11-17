import { describe, expect, it } from 'vitest';
import { page, userEvent } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import { expectNoViolations } from '../../test/axe';
import Input from './input.svelte';

const control = (): HTMLInputElement => {
  const found = document.querySelector<HTMLInputElement>('[data-slot="input"]');
  if (found === null) throw new Error('no input rendered');
  return found;
};

describe('Input', () => {
  it('takes what is typed into it', async () => {
    await render(Input, { props: { 'aria-label': 'Printer name' } });

    await userEvent.type(page.getByRole('textbox'), 'MK4S');

    expect(control().value).toBe('MK4S');
  });

  it('is named by the label that points at it', async () => {
    const host = document.createElement('div');
    host.innerHTML = '<label for="printer-name">Printer name</label>';
    document.body.append(host);

    await render(Input, {
      target: host,
      props: { id: 'printer-name', value: 'MK4S' },
    });

    await expect
      .element(page.getByRole('textbox', { name: 'Printer name' }))
      .toHaveValue('MK4S');
    await expectNoViolations(host);

    host.remove();
  });

  it('takes nothing while it is disabled', async () => {
    await render(Input, {
      props: { 'aria-label': 'Printer name', disabled: true },
    });

    await userEvent.tab();

    expect(document.activeElement).not.toBe(control());
    expect(control().value).toBe('');
  });

  it('stands at its own height on each step of the control scale', async () => {
    const heightOf = async (size: 'sm' | 'md' | 'lg'): Promise<number> => {
      const host = document.createElement('div');
      document.body.append(host);
      await render(Input, {
        target: host,
        props: { size, 'aria-label': size },
      });
      const found = host.querySelector('input');
      if (found === null) throw new Error('no input rendered');
      const height = found.getBoundingClientRect().height;
      host.remove();
      return height;
    };

    const [small, medium, large] = await Promise.all([
      heightOf('sm'),
      heightOf('md'),
      heightOf('lg'),
    ]);

    expect(small).toBeLessThan(medium);
    expect(medium).toBeLessThan(large);
    // 44px is the floor a control reached with a gloved hand takes.
    expect(large).toBe(44);
  });
});
