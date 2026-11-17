import { describe, expect, it, vi } from 'vitest';
import { page, userEvent } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import { expectNoViolations } from '../../test/axe';
import SliderFieldHarness from '../../test/slider-field-harness.svelte';
import SliderField from './slider-field.svelte';

const field = () => page.getByRole('spinbutton', { name: 'Speed override, %' });
const track = () => page.getByRole('slider', { name: 'Speed override' });
const committed = () => page.getByTestId('committed');

describe('SliderField', () => {
  it('names the track and the field, and prints the unit beside them', async () => {
    const { container } = await render(SliderFieldHarness);

    await expect.element(field()).toHaveValue(100);
    await expect.element(track()).toHaveAttribute('aria-valuenow', '100');
    expect(container.textContent).toContain('%');
    await expectNoViolations(container);
  });

  it('sends nothing while a number is being typed, and sends it on Enter', async () => {
    await render(SliderFieldHarness);

    await field().fill('115');
    // A half typed `1` on the way to `115` must never reach a machine.
    await expect.element(committed()).toHaveTextContent('100');

    await userEvent.keyboard('{Enter}');
    await expect.element(committed()).toHaveTextContent('115');
  });

  it('clamps a number outside the range to the end it overshot', async () => {
    await render(SliderFieldHarness);

    await field().fill('900');
    await userEvent.tab();

    await expect.element(committed()).toHaveTextContent('200');
    await expect.element(field()).toHaveValue(200);
  });

  it('keeps the value the caller gave when the field is emptied', async () => {
    await render(SliderFieldHarness, { props: { initial: 120 } });

    await field().fill('');
    await userEvent.tab();

    await expect.element(committed()).toHaveTextContent('120');
    await expect.element(field()).toHaveValue(120);
  });

  it('commits a settled value from the track, and the field follows', async () => {
    await render(SliderFieldHarness);

    // Tab reaches the thumb. A click on the track would move the value on the
    // way, and this is about what one settled step commits.
    await userEvent.tab();
    await userEvent.keyboard('{ArrowRight}');

    await expect.element(committed()).toHaveTextContent('101');
    await expect.element(field()).toHaveValue(101);
  });

  it('drops the draft when the machine reports a new value', async () => {
    const onCommit = vi.fn();
    const { rerender } = await render(SliderField, {
      props: {
        value: 100,
        onCommit,
        min: 50,
        max: 200,
        unit: '%',
        label: 'Speed override',
      },
    });

    await field().fill('115');
    await rerender({ value: 140 });

    await expect.element(field()).toHaveValue(140);
    expect(onCommit).not.toHaveBeenCalled();
  });

  it('blocks both controls when the value cannot be set', async () => {
    const onCommit = vi.fn();
    await render(SliderField, {
      props: {
        value: 95,
        onCommit,
        min: 50,
        max: 200,
        unit: '%',
        label: 'Speed override',
        disabled: true,
      },
    });

    await expect.element(field()).toBeDisabled();
    await expect.element(track()).toHaveAttribute('data-disabled');

    await userEvent.keyboard('{ArrowRight}');
    expect(onCommit).not.toHaveBeenCalled();
  });
});
