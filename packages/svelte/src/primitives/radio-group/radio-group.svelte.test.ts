import { describe, expect, it, vi } from 'vitest';
import { page, userEvent } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import { expectNoViolations } from '../../test/axe';
import RadioGroupProbe from '../../test/radio-group-probe.svelte';

const choices = [
  { value: 'pla', label: 'PLA' },
  { value: 'petg', label: 'PETG', disabled: true },
  { value: 'abs', label: 'ABS' },
];

const radio = (name: string) => page.getByRole('radio', { name });

describe('RadioGroup', () => {
  it('moves the selection with an arrow key and steps over a disabled item', async () => {
    await render(RadioGroupProbe, {
      props: { choices, value: 'pla', label: 'Filament type' },
    });

    // The roving tabindex puts the tab stop on the checked item, so one Tab
    // reaches the group and lands there.
    await userEvent.tab();
    await expect.element(radio('PLA')).toHaveFocus();

    await userEvent.keyboard('{ArrowDown}');

    await expect.element(radio('ABS')).toHaveFocus();
    await expect.element(radio('ABS')).toHaveAttribute('aria-checked', 'true');
    await expect.element(radio('PLA')).toHaveAttribute('aria-checked', 'false');
    await expect
      .element(radio('PETG'))
      .toHaveAttribute('aria-checked', 'false');
  });

  it('reports the value it settles on', async () => {
    const onValueChange = vi.fn();
    await render(RadioGroupProbe, {
      props: { choices, value: 'pla', label: 'Filament type', onValueChange },
    });

    await radio('ABS').click();

    expect(onValueChange).toHaveBeenCalledWith('abs');
    await expect.element(radio('ABS')).toHaveAttribute('aria-checked', 'true');
  });

  it('answers nothing for a disabled item', async () => {
    const onValueChange = vi.fn();
    await render(RadioGroupProbe, {
      props: { choices, value: 'pla', label: 'Filament type', onValueChange },
    });

    // Not the locator's click: a disabled control takes no pointer events, so
    // the actionability wait would time out rather than report the block.
    radio('PETG').element().dispatchEvent(new MouseEvent('click'));

    expect(onValueChange).not.toHaveBeenCalled();
    await expect
      .element(radio('PETG'))
      .toHaveAttribute('aria-checked', 'false');
    await expect.element(radio('PLA')).toHaveAttribute('aria-checked', 'true');
  });

  it('draws the dot on the checked item alone', async () => {
    await render(RadioGroupProbe, {
      props: { choices, value: 'pla', label: 'Filament type' },
    });

    const dots = document.querySelectorAll(
      '[data-slot="radio-group-indicator"]',
    );
    expect(dots).toHaveLength(1);
    expect(
      radio('PLA')
        .element()
        .contains(dots[0] ?? null),
    ).toBe(true);

    await radio('ABS').click();
    await expect
      .poll(
        () =>
          radio('ABS')
            .element()
            .querySelector('[data-slot="radio-group-indicator"]') !== null,
      )
      .toBe(true);
    expect(
      document.querySelectorAll('[data-slot="radio-group-indicator"]'),
    ).toHaveLength(1);
  });

  it('has nothing axe refuses', async () => {
    const { container } = await render(RadioGroupProbe, {
      props: { choices, value: 'pla', label: 'Filament type' },
    });

    await expectNoViolations(container);
  });
});
